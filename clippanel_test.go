package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/youngle316/kintsugi/internal/store"
)

type clipCalls struct {
	pasted, deleted []string
	closed, paused  int
}

func clipTester(t *testing.T, texts ...string) (*ui.Tester, *clipBoard, *clipCalls) {
	t.Helper()
	calls := &clipCalls{}
	b := &clipBoard{
		recording: true,
		paste:     func(c store.Clip) { calls.pasted = append(calls.pasted, c.Text) },
		remove:    func(c store.Clip) { calls.deleted = append(calls.deleted, c.Text) },
		dismiss:   func() { calls.closed++ },
		record:    func(bool) { calls.paused++ },
	}
	for i, s := range texts {
		b.clips = append(b.clips, store.Clip{ID: int64(i + 1), Text: s, App: "Mail", CopiedAt: time.Now()})
	}
	b.open()
	return ui.NewTester(b.view, 520, 440), b, calls
}

func wantOne(t *testing.T, got []string, want string) {
	t.Helper()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnterPastesTheNewest(t *testing.T) {
	tt, _, calls := clipTester(t, "one", "two")
	tt.Key(0, ui.KeyEnter)
	wantOne(t, calls.pasted, "one")
}

func TestArrowsChooseWhileSearchHasFocus(t *testing.T) {
	tt, _, calls := clipTester(t, "one", "two", "three")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter)
	wantOne(t, calls.pasted, "two")
}

func TestCommandDigitPastesThatClip(t *testing.T) {
	tt, _, calls := clipTester(t, "one", "two", "three")
	tt.Key(ui.Cmd, ui.Key3)
	tt.Key(ui.Cmd, ui.Key9) // none there
	wantOne(t, calls.pasted, "three")
}

func TestTypingSearchesAtOnce(t *testing.T) {
	tt, b, calls := clipTester(t, "apple", "banana")
	tt.Type("ban")
	if b.query != "ban" || tt.HasText("apple") || !tt.HasText("banana") {
		t.Fatalf("query %q", b.query)
	}
	tt.Key(0, ui.KeyEnter)
	wantOne(t, calls.pasted, "banana")
}

func TestCommandBackspaceDeletesButBackspaceEditsTheSearch(t *testing.T) {
	tt, b, calls := clipTester(t, "one", "two")
	tt.Type("onx")
	tt.Key(0, ui.KeyBackspace)
	if b.query != "on" || len(calls.deleted) != 0 {
		t.Fatalf("query %q, deleted %q", b.query, calls.deleted)
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if b.query != "on" {
		t.Errorf("⌘⌫ changed the search to %q", b.query)
	}
	wantOne(t, calls.deleted, "one")
}

func TestClickPastes(t *testing.T) {
	tt, _, calls := clipTester(t, "one", "two")
	tt.Click("two")
	wantOne(t, calls.pasted, "two")
}

func TestEscapeCloses(t *testing.T) {
	tt, _, calls := clipTester(t, "one")
	tt.Key(0, ui.KeyEscape)
	if calls.closed != 1 {
		t.Fatal(calls.closed)
	}
}

func TestEmptyStates(t *testing.T) {
	tt, _, calls := clipTester(t)
	if !tt.HasText("Nothing copied yet") {
		t.Error("empty")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if len(calls.pasted)+len(calls.deleted) != 0 {
		t.Error("acted on nothing")
	}
	tt, b, _ := clipTester(t, "one")
	tt.Type("zzz")
	if !tt.HasText("No matches") {
		t.Error("no matches")
	}
	b.recording = false
	tt.Frame()
	if !tt.HasText("Recording is paused") {
		t.Error("paused")
	}
}

func TestOpeningStartsOver(t *testing.T) {
	_, b, _ := clipTester(t, "one", "two")
	b.query, b.sel, b.notice = "x", 1, "old"
	b.open()
	if b.query != "" || b.sel != 0 || b.notice != "" {
		t.Fatalf("%+v", b)
	}
}

func TestNewClipKeepsTheSelectedOne(t *testing.T) {
	_, b, _ := clipTester(t, "one", "two")
	b.sel = 1
	b.show(append([]store.Clip{{ID: 9, Text: "new"}}, b.clips...))
	if b.clips[b.sel].Text != "two" {
		t.Fatalf("selected %q", b.clips[b.sel].Text)
	}
}

func TestRowsStartAtTheLeftAndEndAtTheRight(t *testing.T) {
	tt, _, _ := clipTester(t, "a", "a much longer text than that")
	one, ok1 := tt.Find("⌘1")
	two, ok2 := tt.Find("⌘2")
	text, ok3 := tt.Find("a much longer text than that")
	if !ok1 || !ok2 || !ok3 || one.X < 440 || one.X-two.X > 4 || two.X-one.X > 4 || text.X > 40 {
		t.Fatalf("⌘1 at %v, ⌘2 at %v, the text at %v", one, two, text)
	}
}
