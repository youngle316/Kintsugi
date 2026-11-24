package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOldFileGetsClipboardDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(p, []byte(`{"shortcut":"Alt+Cmd+J"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if s := f.Get(); s.Shortcut != "Alt+Cmd+J" || s.ClipboardShortcut != "Ctrl+Cmd+V" || !s.RecordClipboard {
		t.Fatalf("%+v", s)
	}
}

func TestRecordingOffSurvivesReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	f, _ := Load(p)
	s := f.Get()
	s.RecordClipboard = false
	if _, err := f.Set(s); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(p)
	if f.Get().RecordClipboard {
		t.Fatal("recording came back on")
	}
}

func TestShortcutsMustDiffer(t *testing.T) {
	for _, set := range []func(*Settings){
		func(s *Settings) { s.ClipboardShortcut = "" },
		func(s *Settings) { s.ClipboardShortcut = s.Shortcut },
		func(s *Settings) { s.ClipboardShortcut = "alt+cmd+e" },
		func(s *Settings) { s.ExplainShortcut = s.Shortcut },
	} {
		s := Defaults()
		set(&s)
		if s.Validate() == nil {
			t.Errorf("accepted %q %q %q", s.Shortcut, s.ExplainShortcut, s.ClipboardShortcut)
		}
	}
}
