package main

// The capture lab is the tool of step 0: in each app to test, the user
// selects text and presses the lab's shortcut, which reads the selection
// with each method on its own, then tries each way of writing back. The
// results, kept in a file, say which mode each app needs.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/youngle316/kintsugi/modules/capture"
)

// LabShortcut runs the lab on the app in front.
const LabShortcut = "Ctrl+Alt+Cmd+L"

// LabAttempt is one method tried.
type LabAttempt struct {
	Method string   `json:"method"` // ax, clipboard, ax_write, paste_write
	OK     bool     `json:"ok"`
	Detail string   `json:"detail"`
	Log    []string `json:"log"`
}

// LabRun is a test in one app.
type LabRun struct {
	ID       int64        `json:"id"`
	Time     int64        `json:"time"` // Unix milliseconds
	App      string       `json:"app"`
	BundleID string       `json:"bundleId"`
	Role     string       `json:"role"`
	Note     string       `json:"note"` // what the user tested, e.g. "Amp web, Safari"
	Text     string       `json:"text"` // what was read, cut to 200 characters
	Reads    []LabAttempt `json:"reads"`
	Writes   []LabAttempt `json:"writes"`
	// Confirmed are the user's answers to whether each write showed in
	// the app: by method, "yes" or "no".
	Confirmed map[string]string `json:"confirmed"`
	Focus     capture.Focus     `json:"focus"`
}

// LabChanged is sent when a lab run is added or changes.
var LabChanged = mygo.NewEvent[[]LabRun]("lab:changed")

func (k *Kintsugi) labPath() string {
	dir, _ := mygo.App.Path(mygo.PathUserData)
	return filepath.Join(dir, "capture-lab.json")
}

func (k *Kintsugi) labRuns() []LabRun {
	var runs []LabRun
	if data, err := os.ReadFile(k.labPath()); err == nil {
		_ = json.Unmarshal(data, &runs)
	}
	return runs
}

func (k *Kintsugi) saveLab(runs []LabRun) {
	data, _ := json.MarshalIndent(runs, "", "  ")
	if err := os.WriteFile(k.labPath(), data, 0o600); err != nil {
		log.Println("lab:", err)
	}
	_ = LabChanged.Broadcast(runs)
}

// runLab reads the selection of the app in front with each method alone.
// It runs on a goroutine of its own, from the lab's shortcut.
func (k *Kintsugi) runLab() {
	if k.capture == nil {
		return
	}
	k.mu.Lock()
	if k.busy {
		k.mu.Unlock()
		return
	}
	k.busy = true
	k.mu.Unlock()
	defer func() { k.mu.Lock(); k.busy = false; k.mu.Unlock() }()

	run := LabRun{ID: time.Now().UnixMilli(), Time: time.Now().UnixMilli(), Confirmed: map[string]string{}}
	if app, err := k.capture.FrontmostApp(); err == nil {
		run.App, run.BundleID = app.Name, app.BundleID
	}
	if !k.capture.IsTrusted() {
		run.Reads = append(run.Reads, LabAttempt{Method: "ax", Detail: "no Accessibility permission"})
	} else {
		if f, err := k.capture.InspectFocus(); err == nil {
			run.Focus, run.Role = f, f.Role
		}
		// The clipboard first: the read with the accessibility API comes
		// last, so that it is the one the write tests write back to.
		for _, m := range []struct {
			name string
			opts capture.ReadOptions
		}{
			{"clipboard", capture.ReadOptions{Clipboard: true}},
			{"ax", capture.ReadOptions{AX: true}},
		} {
			sel, err := k.capture.GetSelectedText(m.opts)
			a := LabAttempt{Method: m.name, Log: sel.Log}
			if err != nil {
				a.Detail = err.Error()
			} else {
				a.OK = true
				a.Detail = fmt.Sprintf("%d characters via %s", len([]rune(sel.Text)), sel.Method)
				if run.Text == "" {
					run.Text = cut(sel.Text, 200)
				}
			}
			run.Reads = append(run.Reads, a)
			time.Sleep(150 * time.Millisecond)
		}
	}
	runs := append(k.labRuns(), run)
	k.saveLab(runs)
	k.OpenWindow("lab")
}

// LabRuns returns the lab's results.
func (k *Kintsugi) LabRuns() []LabRun { return k.labRuns() }

// LabShortcut is the shortcut that runs the lab.
func (k *Kintsugi) LabShortcut() string { return LabShortcut }

// LabWrite writes back, in the app of the newest run, its text with a
// marker, by one method alone ("ax" or "paste"). It needs the run to be
// the last read, as it writes where that read was.
func (k *Kintsugi) LabWrite(id int64, method string) (LabRun, error) {
	runs := k.labRuns()
	i := len(runs) - 1
	if i < 0 || runs[i].ID != id {
		return LabRun{}, errors.New("only the newest run can be written back: run the lab again in that app")
	}
	if k.capture == nil {
		return LabRun{}, errors.New("the capture module is missing")
	}
	run := &runs[i]
	text := strings.TrimRight(run.Text, " \n") + " [kintsugi " + method + " test]"
	opts := capture.WriteOptions{AX: method == "ax", Paste: method == "paste"}
	res, err := k.capture.ReplaceSelection(text, opts)
	a := LabAttempt{Method: method + "_write", Log: res.Log}
	switch {
	case err != nil:
		a.Detail = err.Error()
	case res.Verified:
		a.OK, a.Detail = true, "written and read back"
	default:
		a.OK, a.Detail = true, "posted; check that it shows in the app"
	}
	run.Writes = append(run.Writes, a)
	k.saveLab(runs)
	return *run, nil
}

// LabUpdate saves the user's note and answers for a run.
func (k *Kintsugi) LabUpdate(id int64, note string, confirmed map[string]string) error {
	runs := k.labRuns()
	for i := range runs {
		if runs[i].ID == id {
			runs[i].Note, runs[i].Confirmed = note, confirmed
			k.saveLab(runs)
			return nil
		}
	}
	return errors.New("no such run")
}

// LabDelete removes a run; id 0 removes them all.
func (k *Kintsugi) LabDelete(id int64) {
	runs := k.labRuns()
	kept := runs[:0]
	for _, r := range runs {
		if id != 0 && r.ID != id {
			kept = append(kept, r)
		}
	}
	k.saveLab(kept)
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
