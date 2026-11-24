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
	k.compose()
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

func TestComposingAgainKeepsAnUncheckedDraftOnly(t *testing.T) {
	k := &Kintsugi{}
	k.compose()
	k.board.draft = "half"
	k.compose()
	if k.board.draft != "half" {
		t.Errorf("an unchecked draft became %q", k.board.draft)
	}
	k.current.State = "done"
	k.compose()
	if k.board.draft != "" || k.Current().State != stateCompose {
		t.Errorf("draft %q, state %q", k.board.draft, k.Current().State)
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
