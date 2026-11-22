package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/youngle316/kintsugi/internal/llm"
	"github.com/youngle316/kintsugi/internal/settings"
	"github.com/youngle316/kintsugi/internal/store"
	"github.com/youngle316/kintsugi/modules/capture"
)

// apiKeySecret is the Keychain account of the SiliconFlow API key.
const apiKeySecret = "siliconflow-api-key"

// Kintsugi is the app's backend, which the pages call.
type Kintsugi struct {
	settings *settings.File
	db       *store.Store
	capture  capture.Capture // nil where the native module is missing

	mu       sync.Mutex
	shortcut string
	panel    *mygo.Window
	main     *mygo.Window
	tray     *mygo.Tray
	busy     bool
	current  *Check // what the panel shows
	lastLog  []string
}

// Check is a correction the panel shows.
type Check struct {
	// State is "loading", "done" or "error".
	State        string      `json:"state"`
	ID           int64       `json:"id"`
	Original     string      `json:"original"`
	Result       *llm.Result `json:"result"`
	Error        string      `json:"error,omitempty"`
	SourceApp    string      `json:"sourceApp"`
	SourceName   string      `json:"sourceName"`
	Method       string      `json:"method"`
	WholeField   bool        `json:"wholeField"`
	Language     string      `json:"language"`
	CanReplace   bool        `json:"canReplace"`
	ManualMode   bool        `json:"manualMode"`
	ActionResult string      `json:"actionResult,omitempty"`
}

// CheckChanged is sent to the panel as a check goes on.
var CheckChanged = mygo.NewEvent[Check]("check:changed")

// DataChanged is sent to the main window when a check is saved.
var DataChanged = mygo.NewEvent[int64]("data:changed")

// Navigate tells the main window which tab to show.
var Navigate = mygo.NewEvent[string]("navigate")

// ---- The check ----

// checkSelection reads the selection of the app in front, shows the panel
// and corrects the text. It runs on a goroutine of its own.
func (k *Kintsugi) checkSelection() {
	k.mu.Lock()
	if k.busy {
		k.mu.Unlock()
		return
	}
	k.busy = true
	k.mu.Unlock()
	defer func() { k.mu.Lock(); k.busy = false; k.mu.Unlock() }()

	s := k.settings.Get()
	c := &Check{State: "loading", Language: string(s.Language)}
	text, caret, err := k.read(s, c)
	if err != nil {
		c.State, c.Error = "error", err.Error()
		k.showPanel(c, caret)
		return
	}
	c.Original = text
	k.showPanel(c, caret)

	key, _ := k.apiKey()
	client := &llm.Client{BaseURL: s.BaseURL, APIKey: key}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := client.Check(ctx, text, s.CheckModel)
	if err != nil {
		c.State, c.Error = "error", err.Error()
		k.update(c)
		return
	}
	// Saved now, as ignored, so that closing the panel any way still
	// records the check; the action updates it.
	id, err := k.db.Save(ctx, store.Submission{
		CreatedAt: time.Now(), SourceApp: c.SourceApp, SourceAppName: c.SourceName,
		CaptureMethod: c.Method, OriginalText: text, Action: store.ActionIgnored, Result: res,
	})
	if err != nil {
		log.Println("saving the check:", err)
	}
	c.ID, c.Result, c.State = id, &res, "done"
	k.update(c)
	_ = DataChanged.Broadcast(id)
}

// read gets the text to check: from the native module as the app's mode
// allows, else what the user copied.
func (k *Kintsugi) read(s settings.Settings, c *Check) (string, *capture.Rect, error) {
	mode := settings.ModeManual
	if k.capture != nil {
		if app, err := k.capture.FrontmostApp(); err == nil {
			c.SourceApp, c.SourceName = app.BundleID, app.Name
			mode = s.ModeFor(app.BundleID)
		}
		if mode != settings.ModeManual && !k.capture.IsTrusted() {
			return "", nil, errors.New("Kintsugi needs the Accessibility permission to read the selection. Open Kintsugi ▸ Settings to grant it, or copy the text (⌘C) and press the shortcut again with this app set to manual mode")
		}
	}
	if mode == settings.ModeManual {
		c.Method, c.ManualMode = "manual_clipboard", true
		text := mygo.Clipboard.ReadText()
		if strings.TrimSpace(text) == "" {
			return "", nil, errors.New("the clipboard has no text: copy what you wrote (⌘C), then press the shortcut")
		}
		return text, nil, nil
	}
	sel, err := k.capture.GetSelectedText(capture.ReadOptions{
		AX: mode == settings.ModeAuto, Clipboard: true, WholeField: true,
	})
	k.mu.Lock()
	k.lastLog = sel.Log
	k.mu.Unlock()
	if err != nil {
		if errors.Is(err, capture.ErrNoText) {
			return "", nil, errors.New("nothing to check: select some text first")
		}
		return "", nil, err
	}
	c.Method, c.WholeField, c.CanReplace = string(sel.Method), sel.WholeField, true
	return sel.Text, sel.Caret, nil
}

// update sends the check to the panel.
func (k *Kintsugi) update(c *Check) {
	k.mu.Lock()
	cp := *c
	k.current = &cp
	p := k.panel
	k.mu.Unlock()
	if p != nil {
		_ = CheckChanged.Emit(p, cp)
	}
}

// ---- What the panel calls ----

// Current returns what the panel shows, for a page that just loaded.
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
// panel. In manual mode it puts the correction on the clipboard instead.
func (k *Kintsugi) Replace(id int64, text string) (string, error) {
	k.hidePanel()
	c := k.Current()
	if c == nil || c.ManualMode || k.capture == nil {
		mygo.Clipboard.WriteText(text)
		k.setAction(id, store.ActionCopied)
		return "copied", nil
	}
	mode := k.settings.Get().ModeFor(c.SourceApp)
	res, err := k.capture.ReplaceSelection(text, capture.WriteOptions{AX: mode == settings.ModeAuto, Paste: true})
	k.mu.Lock()
	k.lastLog = append(k.lastLog, res.Log...)
	k.mu.Unlock()
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
	_ = DataChanged.Broadcast(id)
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

// Issues lists the mistakes f selects.
func (k *Kintsugi) Issues(ctx context.Context, f store.Filter) ([]store.IssueRow, error) {
	return k.db.Issues(ctx, f)
}

// Categories are the kinds of mistakes.
func (k *Kintsugi) Categories() []string { return llm.Categories }

// Settings returns the settings.
func (k *Kintsugi) Settings() settings.Settings { return k.settings.Get() }

// SaveSettings saves the settings and registers the shortcut they name.
func (k *Kintsugi) SaveSettings(s settings.Settings) (settings.Settings, error) {
	old := k.settings.Get()
	s.Onboarded = s.Onboarded || old.Onboarded
	if err := s.Validate(); err != nil {
		return old, err
	}
	if s.Shortcut != old.Shortcut {
		var err error
		mygo.RunOnMain(func() { err = k.registerShortcut(s.Shortcut) })
		if err != nil {
			return old, fmt.Errorf("the shortcut %s: %w", s.Shortcut, err)
		}
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
	Available bool `json:"available"`
	Trusted   bool `json:"trusted"`
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

// OpenAtLogin reports whether Kintsugi starts at login.
func (k *Kintsugi) OpenAtLogin() bool { return mygo.App.OpenAtLogin() }

// SetOpenAtLogin starts Kintsugi at login, or stops.
func (k *Kintsugi) SetOpenAtLogin(open bool) error { return mygo.App.SetOpenAtLogin(open) }

// FinishOnboarding records that the first-run guide is done.
func (k *Kintsugi) FinishOnboarding() error {
	s := k.settings.Get()
	s.Onboarded = true
	_, err := k.settings.Set(s)
	return err
}

// CaptureLog returns what the last read and write found, step by step.
func (k *Kintsugi) CaptureLog() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.lastLog...)
}

// ---- Windows and the menu bar ----

// OpenWindow shows the main window at a tab ("" for the default).
func (k *Kintsugi) OpenWindow(tab string) {
	mygo.RunOnMain(func() {
		if k.main == nil || k.main.IsDestroyed() {
			k.main = mygo.NewWindow(mygo.WindowOptions{
				Title: "Kintsugi", Width: 1040, Height: 720, MinWidth: 760, MinHeight: 520,
				BackgroundColor: "light-dark(#faf8f3, #16140f)",
				StateKey:        "main",
				URL:             "/",
			})
		}
		k.main.Show()
		mygo.App.Focus()
	})
	if tab != "" {
		_ = Navigate.Emit(k.main, tab)
	}
}

func (k *Kintsugi) makePanel() {
	p := mygo.NewWindow(mygo.WindowOptions{
		Panel: true, Hidden: true, Frameless: true, Transparent: true,
		Vibrancy: mygo.VibrancyHUD, CornerRadius: 14,
		Width: 520, Height: 440, MinHeight: 160, MaxHeight: 640,
		URL: "/panel",
	})
	// Hide rather than close, so that it shows at once the next time.
	p.OnClose(func(e *mygo.CloseEvent) {
		e.PreventDefault()
		p.Hide()
	})
	// Gone as the user goes back to writing; the check stays saved.
	p.OnBlur(p.Hide)
	k.panel = p
}

// showPanel shows the check near the caret, or the mouse.
func (k *Kintsugi) showPanel(c *Check, caret *capture.Rect) {
	k.update(c)
	mygo.RunOnMain(func() {
		p := k.panel
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
		p.Show()
	})
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
			mygo.Separator(),
			{Label: "Open Kintsugi", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("overview") }},
			{Label: "Mistakes", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("mistakes") }},
			{Label: "Capture Lab (step 0)", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("lab") }},
			{Label: "Settings…", Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) { go k.OpenWindow("settings") }},
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

// trayIcon draws a 32×32 template image: a bowl with a crack through it.
func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			dx, dy := float64(x)-15.5, float64(y)-10
			inBowl := y >= 12 && y <= 26 && dx*dx/(13.5*13.5)+dy*dy/(16*16) <= 1
			rim := y >= 10 && y < 12 && x >= 2 && x <= 29
			if !(inBowl || rim) {
				continue
			}
			// The crack: a zigzag, left transparent.
			cx := 14 + map[bool]int{true: 3, false: 0}[(y/4)%2 == 0]
			if x == cx || x == cx+1 {
				continue
			}
			img.Set(x, y, color.Black)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
