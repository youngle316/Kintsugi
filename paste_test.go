package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/youngle316/kintsugi/internal/llm"
)

func pasteTester(t *testing.T) (*ui.Tester, *Kintsugi, *[]string) {
	t.Helper()
	k := &Kintsugi{}
	var checked []string
	k.board.check = func(text string) { checked = append(checked, text) }
	k.compose("")
	return ui.NewTester(k.panelView, 520, 440), k, &checked
}

func TestPastedTextIsChecked(t *testing.T) {
	tt, _, checked := pasteTester(t)
	tt.SetClipboard("I have went home.\nShe go too.")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(*checked) != 1 || (*checked)[0] != "I have went home.\nShe go too." {
		t.Fatalf("%q", *checked)
	}
}

func TestEnterAddsALine(t *testing.T) {
	tt, k, checked := pasteTester(t)
	tt.Type("one")
	tt.Key(0, ui.KeyEnter)
	tt.Type("two")
	if k.board.draft != "one\ntwo" || len(*checked) != 0 {
		t.Fatalf("draft %q, checked %q", k.board.draft, *checked)
	}
}

func TestBlankTextIsNotChecked(t *testing.T) {
	tt, _, checked := pasteTester(t)
	tt.Type("  ")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(*checked) != 0 {
		t.Fatalf("%q", *checked)
	}
}

func TestPanelOpensWithTheClipboard(t *testing.T) {
	tt, k, checked := pasteTester(t)
	k.compose("She go home.")
	tt.Frame()
	tt.Type(" Now.")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(*checked) != 1 || (*checked)[0] != "She go home. Now." {
		t.Fatalf("%q", *checked)
	}
}

func TestComposingAgain(t *testing.T) {
	for _, c := range []struct{ name, draft, state, clip, want string }{
		{"the same clipboard keeps the edits", "old, edited", stateCompose, "old", "old, edited"},
		{"a new copy replaces the draft", "old, edited", stateCompose, "new", "new"},
		{"an empty clipboard keeps the draft", "old, edited", stateCompose, " ", "old, edited"},
		{"a closed panel fills again", "", stateCompose, "old", "old"},
		{"after a check it starts over", "old, edited", "done", "old", "old"},
		{"after a check, nothing copied", "old, edited", "done", "", ""},
	} {
		k := &Kintsugi{}
		k.compose("old")
		k.board.draft, k.current.State = c.draft, c.state
		k.compose(c.clip)
		if k.board.draft != c.want || k.Current().State != stateCompose {
			t.Errorf("%s: %q", c.name, k.board.draft)
		}
	}
}

func TestComposerHasNoFooterText(t *testing.T) {
	tt, _, _ := pasteTester(t)
	if tt.HasText("⌘↩ Check") {
		t.Error("the footer is back")
	}
}

func TestErrorGoesBackToTheText(t *testing.T) {
	tt, k, _ := pasteTester(t)
	tt.Type("keep me")
	k.current.State, k.current.Error = "error", "no network"
	tt.Frame()
	if err := tt.Click("Edit the text"); err != nil {
		t.Fatal(err)
	}
	if k.Current().State != stateCompose || k.board.draft != "keep me" {
		t.Fatalf("%q %q", k.Current().State, k.board.draft)
	}
}

func TestPastedTextIsCopiedNotReplaced(t *testing.T) {
	_, k, _ := pasteTester(t)
	c := *k.Current()
	c.State, c.Result = "done", &llm.Result{}
	if replaceAction(&c) != "copy" {
		t.Fatal(replaceAction(&c))
	}
}
