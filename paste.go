package main

// Checking text the user pastes: for where the selection cannot be read,
// as in a terminal app. The panel takes the text, then shows its check.

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/field"
)

// stateCompose is the state of a Check whose text the user is pasting.
const stateCompose = "compose"

// methodPasted is the capture method of a text pasted into the panel.
const methodPasted = "pasted"

// compose has the panel take a text, beginning with clip, what the user
// copied. A draft stays when nothing new was copied since it was filled.
// It runs on the main thread.
func (k *Kintsugi) compose(clip string) {
	b := &k.board
	if ch := k.Current(); ch == nil || ch.State != stateCompose {
		b.draft = ""
	}
	blank := strings.TrimSpace(clip) == ""
	switch {
	case b.draft != "" && (blank || clip == b.filled):
	case blank:
		b.draft, b.filled = "", ""
	default:
		b.draft, b.filled = clip, clip
	}
	k.editDraft()
}

// editDraft has the panel take a text, beginning with the draft.
func (k *Kintsugi) editDraft() {
	k.board.focusDraft, k.board.hot = true, -1
	k.mu.Lock()
	k.current = &Check{Mode: modeCheck, State: stateCompose, Method: methodPasted, ManualMode: true}
	k.mu.Unlock()
}

// openPaste shows the panel with the text the user copied, to check, or the check under way.
func (k *Kintsugi) openPaste() {
	mygo.RunOnMain(func() {
		k.mu.Lock()
		busy := k.busy
		k.mu.Unlock()
		visible := k.panel.IsVisible()
		if !busy {
			k.compose(mygo.Clipboard.ReadText())
		}
		k.panel.Update(func() {})
		if !visible && !k.board.pinned {
			place(k.panel, nil)
		}
		k.panel.Show()
	})
}

// checkPasted corrects a text pasted into the panel. It runs on a
// goroutine of its own.
func (k *Kintsugi) checkPasted(text string) {
	if !k.begin() {
		return
	}
	defer k.end()
	s := k.settings.Get()
	c := &Check{Mode: modeCheck, State: "loading", Language: string(s.Language), Original: text, Method: methodPasted, ManualMode: true}
	k.update(c)
	k.process(c, s)
}

// composer is the field to paste into.
func (k *Kintsugi) composer(c *ui.Context) {
	in := field.TextArea(c.Key("draft"), &k.board.draft, field.TextAreaProps{
		Placeholder: "Paste English to check (⌘V)", MinRows: 20, MaxRows: 20,
	}).Label("Text to check")
	if k.board.focusDraft {
		in.Focus()
		end := utf8.RuneCountInString(k.board.draft)
		in.SetTextSelection(end, end)
		k.board.focusDraft = false
	}
}

// submitDraft checks the draft, unless it is blank.
func (k *Kintsugi) submitDraft() {
	if strings.TrimSpace(k.board.draft) != "" {
		k.board.check(k.board.draft)
	}
}
