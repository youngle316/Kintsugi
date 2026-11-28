package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
	"github.com/youngle316/kintsugi/internal/llm"
	"github.com/youngle316/kintsugi/internal/settings"
	"github.com/youngle316/kintsugi/internal/store"
	"github.com/youngle316/kintsugi/modules/capture"
)

// apiKeySecret is the Keychain account of the SiliconFlow API key.
const apiKeySecret = "siliconflow-api-key"

// Kintsugi is the app: the check, the windows that show it, and what they
// call.
type Kintsugi struct {
	settings *settings.File
	db       *store.Store
	capture  capture.Capture // nil where the native module is missing

	mu       sync.Mutex
	shortcut string // the shortcut that checks
	explain  string // the shortcut that explains
	paste    string // the shortcut that checks a pasted text
	panel    *mygo.Window
	main     *mygo.Window
	tray     *mygo.Tray
	busy     bool
	current  *Check // what the panel shows

	// The state of the windows' views, read and changed on the main
	// thread only.
	view  mainState
	board panelState
}

// Check is what the panel shows: a correction of the English the user
// wrote, or an explanation of English they read.
type Check struct {
	// Mode is modeCheck or modeExplain.
	Mode string
	// State is stateCompose, "loading", "done" or "error".
	State       string
	ID          int64
	Original    string
	Result      *llm.Result      // done, of a check
	Explanation *llm.Explanation // done, of an explanation
	Error       string
	Method      string
	WholeField  bool
	Language    string
	CanReplace  bool
	ManualMode  bool
}

// The modes of the panel.
const (
	modeCheck   = "check"
	modeExplain = "explain"
)

// ---- The check ----

// checkSelection reads the selection of the app in front, shows the panel
// and corrects the text. It runs on a goroutine of its own.
func (k *Kintsugi) checkSelection() { k.run(modeCheck) }

// explainSelection reads the selection of the app in front, shows the
// panel and explains the text in plain English. It runs on a goroutine of
// its own. Explanations are saved apart from checks, for a vocabulary book
// to come: they are not the user's mistakes.
func (k *Kintsugi) explainSelection() { k.run(modeExplain) }

func (k *Kintsugi) run(mode string) {
	if !k.begin() {
		return
	}
	defer k.end()
	s := k.settings.Get()
	c := &Check{Mode: mode, State: "loading", Language: string(s.Language)}
	text, caret, err := k.read(c)
	if err != nil {
		c.State, c.Error = "error", err.Error()
		k.showPanel(c, caret)
		return
	}
	c.Original = text
	k.showPanel(c, caret)
	k.process(c, s)
}

// begin starts a check, unless one is under way; end ends it.
func (k *Kintsugi) begin() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.busy {
		return false
	}
	k.busy = true
	return true
}

func (k *Kintsugi) end() {
	k.mu.Lock()
	k.busy = false
	k.mu.Unlock()
}

// process corrects or explains the text of c, saves it and shows it.
func (k *Kintsugi) process(c *Check, s settings.Settings) {
	text, mode := c.Original, c.Mode
	key, _ := k.apiKey()
	client := &llm.Client{BaseURL: s.BaseURL, APIKey: key}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if mode == modeExplain {
		ex, err := client.Explain(ctx, text, s.CheckModel)
		if err != nil {
			c.State, c.Error = "error", err.Error()
			k.update(c)
			return
		}
		c.Explanation, c.State = &ex, "done"
		k.update(c)
		err = k.db.SaveLookup(ctx, store.Lookup{CreatedAt: time.Now(), CaptureMethod: c.Method, Text: text, Explanation: ex})
		if err != nil {
			log.Println("saving the lookup:", err)
		}
		return
	}
	res, err := client.Check(ctx, text, s.CheckModel)
	if err != nil {
		c.State, c.Error = "error", err.Error()
		k.update(c)
		return
	}
	// Saved now, as ignored, so that closing the panel any way still
	// records the check; the action updates it.
	id, err := k.db.Save(ctx, store.Submission{
		CreatedAt: time.Now(), CaptureMethod: c.Method, OriginalText: text, Action: store.ActionIgnored, Result: res,
	})
	if err != nil {
		log.Println("saving the check:", err)
	}
	c.ID, c.Result, c.State = id, &res, "done"
	k.update(c)
	k.reload()
}

// read gets the text to check or explain: the selection of the app in
// front, read the same way in every app, else, without the native module,
// what the user copied.
func (k *Kintsugi) read(c *Check) (string, *capture.Rect, error) {
	if k.capture != nil && !k.capture.IsTrusted() {
		return "", nil, errors.New("Kintsugi needs the Accessibility permission to read the selection. Open Kintsugi ▸ Settings to grant it")
	}
	if k.capture == nil {
		c.Method, c.ManualMode = "manual_clipboard", true
		text := mygo.Clipboard.ReadText()
		if strings.TrimSpace(text) == "" {
			return "", nil, errors.New("the clipboard has no text: copy the text (⌘C), then press the shortcut")
		}
		return text, nil, nil
	}
	sel, err := k.capture.GetSelectedText(capture.ReadOptions{
		AX: true, Clipboard: true, WholeField: true,
	})
	if err != nil {
		if errors.Is(err, capture.ErrNoText) {
			return "", nil, fmt.Errorf("nothing to %s: select some text first", c.Mode)
		}
		return "", nil, err
	}
	c.Method, c.WholeField, c.CanReplace = string(sel.Method), sel.WholeField, true
	return sel.Text, sel.Caret, nil
}

// update shows the check in the panel.
func (k *Kintsugi) update(c *Check) {
	k.mu.Lock()
	cp := *c
	k.current = &cp
	p := k.panel
	k.mu.Unlock()
	if p != nil {
		p.Update(func() { k.board.hot = -1 })
	}
}

// ---- What the panel calls ----

// Current returns what the panel shows.
func (k *Kintsugi) Current() *Check {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.current == nil {
		return nil
	}
	cp := *k.current
	return &cp
}

// Replace writes the correction back where it was read, then hides the
// panel. Without the native module it puts the correction on the
// clipboard instead. It waits for the app it writes to, so it runs off the
// main thread.
func (k *Kintsugi) Replace(id int64, text string) (string, error) {
	k.hidePanel()
	c := k.Current()
	if c == nil || c.ManualMode || k.capture == nil {
		mygo.Clipboard.WriteText(text)
		k.setAction(id, store.ActionCopied)
		return "copied", nil
	}
	res, err := k.capture.ReplaceSelection(text, capture.WriteOptions{AX: true, Paste: true})
	if err != nil {
		// Nothing written: the correction stays on the clipboard to paste.
		mygo.Clipboard.WriteText(text)
		k.setAction(id, store.ActionCopied)
		return "", fmt.Errorf("could not write back (%v); the correction is on the clipboard", err)
	}
	k.setAction(id, store.ActionReplaced)
	return string(res.Method), nil
}

// Copy puts the correction on the clipboard and hides the panel.
func (k *Kintsugi) Copy(id int64, text string) {
	mygo.Clipboard.WriteText(text)
	k.setAction(id, store.ActionCopied)
	k.hidePanel()
}

// Dismiss hides the panel; the check stays saved as ignored.
func (k *Kintsugi) Dismiss() { k.hidePanel() }

func (k *Kintsugi) setAction(id int64, action string) {
	if id == 0 {
		return
	}
	if err := k.db.SetAction(context.Background(), id, action); err != nil {
		log.Println("saving the action:", err)
	}
	k.reload()
}

// ---- What the main window calls ----

// Overview sums up the checks of the last days (all of them when days is 0).
func (k *Kintsugi) Overview(ctx context.Context, days int) (store.Overview, error) {
	var since time.Time
	if days > 0 {
		since = time.Now().AddDate(0, 0, -days)
	}
	return k.db.Overview(ctx, since)
}

// SaveSettings saves the settings and registers the shortcuts they name.
func (k *Kintsugi) SaveSettings(s settings.Settings) (settings.Settings, error) {
	old := k.settings.Get()
	s.Onboarded = s.Onboarded || old.Onboarded
	if err := s.Validate(); err != nil {
		return old, err
	}
	var err error
	mygo.RunOnMain(func() { err = k.registerShortcuts(s) })
	if err != nil {
		return old, err
	}
	return k.settings.Set(s)
}

// HasAPIKey reports whether an API key is in the Keychain.
func (k *Kintsugi) HasAPIKey() bool {
	key, err := k.apiKey()
	return err == nil && key != ""
}

// SetAPIKey keeps the key in the Keychain; an empty key removes it.
func (k *Kintsugi) SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return mygo.Secrets.Delete(apiKeySecret)
	}
	return mygo.Secrets.Set(apiKeySecret, []byte(key))
}

func (k *Kintsugi) apiKey() (string, error) {
	b, err := mygo.Secrets.Get(apiKeySecret)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		// For development and tests: a key in the environment, used
		// only while the Keychain has none.
		return os.Getenv("SILICONFLOW_API_KEY"), nil
	}
	return string(b), err
}

// TestModel checks a short sentence with the model, to try the key.
func (k *Kintsugi) TestModel(ctx context.Context, model string) (string, error) {
	s := k.settings.Get()
	key, err := k.apiKey()
	if err != nil {
		return "", err
	}
	if model == "" {
		model = s.CheckModel
	}
	start := time.Now()
	res, err := (&llm.Client{BaseURL: s.BaseURL, APIKey: key}).Check(ctx, "This are a test.", model)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s answered in %.1fs: %q", model, time.Since(start).Seconds(), res.CorrectedText), nil
}

// PermissionState is whether Kintsugi may read other apps.
type PermissionState struct {
	// Available is false where the capture module is missing.
	Available bool
	Trusted   bool
}

// Permission reports the Accessibility permission.
func (k *Kintsugi) Permission() PermissionState {
	if k.capture == nil {
		return PermissionState{}
	}
	return PermissionState{Available: true, Trusted: k.capture.IsTrusted()}
}

// GuidePermission opens System Settings at Accessibility, with Kintsugi to
// drag into the list, and waits until it is granted or the guide closes.
func (k *Kintsugi) GuidePermission(ctx context.Context) (PermissionState, error) {
	st, err := permissionflow.Check(permissionflow.Accessibility)
	if err == nil && st != permissionflow.Granted {
		_, err = permissionflow.Guide(ctx, permissionflow.Accessibility, permissionflow.GuideOptions{})
	}
	return k.Permission(), err
}

// SetOpenAtLogin starts Kintsugi at login, or stops.
func (k *Kintsugi) SetOpenAtLogin(open bool) error { return mygo.App.SetOpenAtLogin(open) }

// VersionInfo is the running version and whether it can update itself.
type VersionInfo struct {
	Version   string
	CanUpdate bool
	// Available is a newer version a check found, "" when none.
	Available string
}

// Version returns the running version.
func (k *Kintsugi) Version() VersionInfo {
	return VersionInfo{Version: mygo.App.Version(), CanUpdate: mygo.Updater.Enabled(), Available: updater.AvailableVersion()}
}

// CheckForUpdates shows the update window, which checks GitHub Releases.
func (k *Kintsugi) CheckForUpdates() { updater.CheckForUpdates() }

// FinishOnboarding records that the first-run guide is done.
func (k *Kintsugi) FinishOnboarding() error {
	s := k.settings.Get()
	s.Onboarded = true
	_, err := k.settings.Set(s)
	return err
}

// ---- Windows and the menu bar ----

// OpenWindow shows the main window at a page ("" for the default: the
// first-run guide until it is done, else the overview).
func (k *Kintsugi) OpenWindow(page string) {
	if page == "" {
		page = pageOverview
		if !k.settings.Get().Onboarded || !k.HasAPIKey() {
			page = pageWelcome
		}
	}
	mygo.RunOnMain(func() {
		if k.main == nil || k.main.IsDestroyed() {
			k.main = mygo.NewWindow(mygo.WindowOptions{
				Title: "Kintsugi", Width: 1080, Height: 740, MinWidth: 820, MinHeight: 540,
				TitleBarStyle:        mygo.TitleBarHidden,
				TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
				Vibrancy:             mygo.VibrancySidebar,
				BackgroundColor:      "light-dark(#f8f8f8, #000000)",
				StateKey:             "main",
				Content:              ui.View(k.mainView),
			})
		}
		k.navigate(page)
		k.main.Show()
		mygo.App.Focus()
	})
}

func (k *Kintsugi) makePanel() {
	k.board.check = func(text string) { go k.checkPasted(text) }
	p := mygo.NewWindow(mygo.WindowOptions{
		// Opaque: a see-through material let the desktop show through and
		// washed out the text over a bright background.
		Panel: true, Hidden: true, Frameless: true, CornerRadius: 14,
		Width: 520, Height: 440, MinHeight: 160, MaxHeight: 640,
		BackgroundColor: "light-dark(#ffffff, #1a1a1a)",
		Content:         ui.View(k.panelView),
	})
	// Hide rather than close, so that it shows at once the next time; but
	// a quit closes every window, and one kept open cancels it.
	quitting := false
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.OnQuitCanceled(func() { quitting = false })
	p.OnClose(func(e *mygo.CloseEvent) {
		if quitting {
			return
		}
		e.PreventDefault()
		p.Hide()
	})
	// Gone as the user goes back to writing, unless pinned; the check
	// stays saved.
	p.OnBlur(func() {
		// A text being pasted stays: the user may go and copy more.
		if ch := k.Current(); !k.board.pinned && (ch == nil || ch.State != stateCompose) {
			p.Hide()
		}
	})
	k.panel = p
}

// showPanel shows the check near the caret, or the mouse; pinned, where
// the user put it.
func (k *Kintsugi) showPanel(c *Check, caret *capture.Rect) {
	k.update(c)
	mygo.RunOnMain(func() {
		p := k.panel
		if k.board.pinned {
			p.Show()
			return
		}
		place(p, caret)
		p.Show()
	})
}

// place moves the panel near the caret, or the mouse, on the screen.
func place(p *mygo.Window, caret *capture.Rect) {
	w, h := p.Size()
	pt := mygo.Screen.CursorScreenPoint()
	x, y := pt.X+12, pt.Y+18
	if caret != nil && caret.Height > 0 {
		x, y = int(caret.X), int(caret.Y+caret.Height)+8
	}
	d := mygo.Screen.DisplayNearestPoint(mygo.Point{X: x, Y: y})
	wa := d.WorkArea
	if x+w > wa.X+wa.Width {
		x = wa.X + wa.Width - w - 8
	}
	if y+h > wa.Y+wa.Height {
		// Above the caret when there is no room under it.
		if caret != nil {
			y = int(caret.Y) - h - 8
		} else {
			y = wa.Y + wa.Height - h - 8
		}
	}
	x, y = max(x, wa.X+8), max(y, wa.Y+8)
	p.SetPosition(x, y)
}

func (k *Kintsugi) hidePanel() {
	mygo.RunOnMain(func() { k.panel.Hide() })
}

func (k *Kintsugi) makeTray() {
	t, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "kintsugi",
		Icon:           trayIcon(),
		IconIsTemplate: true,
		ToolTip:        "Kintsugi",
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Check Selection", Click: func(*mygo.MenuItem, *mygo.Window) { go k.checkSelection() }},
			{Label: "Explain Selection", Click: func(*mygo.MenuItem, *mygo.Window) { go k.explainSelection() }},
			{Label: "Check Pasted Text…", Click: func(*mygo.MenuItem, *mygo.Window) { go k.openPaste() }},
			mygo.Separator(),
			{Label: "Open Kintsugi", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("overview") }},
			{Label: "Mistakes", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("mistakes") }},
			{Label: "Settings…", Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("settings") }},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}),
	})
	if err != nil {
		log.Println("tray:", err)
		return
	}
	k.tray = t
}

// trayIcon draws the mark as a 32×32 template image: the bowl, its crack
// left empty, since macOS tints the menu bar's icons one color.
func trayIcon() []byte {
	bowl := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 1.6 24 24"><path d="` + markBowl + `"/></svg>`))
	var b bytes.Buffer
	_ = png.Encode(&b, bowl.Image(32, 32, color.Black))
	return b.Bytes()
}
