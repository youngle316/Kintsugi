package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOldFileGetsThePasteShortcut(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	// As 0.6.0 wrote it, with settings that are gone.
	if err := os.WriteFile(p, []byte(`{"shortcut":"Alt+Cmd+J","clipboardShortcut":"Ctrl+Cmd+B","recordClipboard":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if s := f.Get(); s.Shortcut != "Alt+Cmd+J" || s.PasteShortcut != "Ctrl+Cmd+V" {
		t.Fatalf("%+v", s)
	}
}

func TestShortcutsMustDiffer(t *testing.T) {
	for _, set := range []func(*Settings){
		func(s *Settings) { s.PasteShortcut = "" },
		func(s *Settings) { s.PasteShortcut = s.Shortcut },
		func(s *Settings) { s.PasteShortcut = "alt+cmd+e" },
		func(s *Settings) { s.ExplainShortcut = s.Shortcut },
	} {
		s := Defaults()
		set(&s)
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
}
