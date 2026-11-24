// Package settings keeps Kintsugi's preferences in a JSON file. The API key
// is not among them: it lives in the Keychain (mygo.Secrets).
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/youngle316/kintsugi/internal/llm"
)

// Language is the language of explanations.
type Language string

const (
	LanguageZh Language = "zh"
	LanguageEn Language = "en"
)

// Settings are the user's preferences.
type Settings struct {
	BaseURL      string   `json:"baseURL"`
	CheckModel   string   `json:"checkModel"`
	QualityModel string   `json:"qualityModel"`
	Language     Language `json:"language"`
	Shortcut     string   `json:"shortcut"`
	// ExplainShortcut explains the selection in plain English.
	ExplainShortcut string `json:"explainShortcut"`
	// ClipboardShortcut opens the clipboard panel.
	ClipboardShortcut string `json:"clipboardShortcut"`
	// RecordClipboard keeps what the user copies.
	RecordClipboard bool `json:"recordClipboard"`
	// Onboarded is set once the first-run guide was finished or skipped.
	Onboarded bool `json:"onboarded"`
}

// Defaults are the settings of a new install.
func Defaults() Settings {
	return Settings{
		BaseURL:           llm.DefaultBaseURL,
		CheckModel:        "deepseek-ai/DeepSeek-V4.1-Flash",
		QualityModel:      "deepseek-ai/DeepSeek-V4-Pro",
		Language:          LanguageZh,
		Shortcut:          "Alt+Cmd+G",
		ExplainShortcut:   "Alt+Cmd+E",
		ClipboardShortcut: "Ctrl+Cmd+V",
		RecordClipboard:   true,
	}
}

// Validate checks and tidies the settings.
func (s *Settings) Validate() error {
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.CheckModel = strings.TrimSpace(s.CheckModel)
	s.QualityModel = strings.TrimSpace(s.QualityModel)
	s.Shortcut = strings.TrimSpace(s.Shortcut)
	s.ExplainShortcut = strings.TrimSpace(s.ExplainShortcut)
	s.ClipboardShortcut = strings.TrimSpace(s.ClipboardShortcut)
	switch {
	case !strings.HasPrefix(s.BaseURL, "https://") && !strings.HasPrefix(s.BaseURL, "http://"):
		return errors.New("the API address must start with https://")
	case s.CheckModel == "":
		return errors.New("choose a model for checking")
	case s.Shortcut == "":
		return errors.New("choose a shortcut")
	case s.ExplainShortcut == "":
		return errors.New("choose a shortcut to explain")
	case strings.EqualFold(s.ExplainShortcut, s.Shortcut):
		return errors.New("checking and explaining need different shortcuts")
	case s.ClipboardShortcut == "":
		return errors.New("choose a shortcut for the clipboard history")
	case strings.EqualFold(s.ClipboardShortcut, s.Shortcut), strings.EqualFold(s.ClipboardShortcut, s.ExplainShortcut):
		return errors.New("the clipboard history needs a shortcut of its own")
	}
	if s.QualityModel == "" {
		s.QualityModel = s.CheckModel
	}
	if s.Language != LanguageEn {
		s.Language = LanguageZh
	}
	return nil
}

// File keeps settings in a file.
type File struct {
	path string
	mu   sync.Mutex
	cur  Settings
}

// Load reads the settings at path, or the defaults when there is none.
func Load(path string) (*File, error) {
	f := &File{path: path, cur: Defaults()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &f.cur); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return f, nil
}

// Get returns the settings.
func (f *File) Get() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

// Set validates and saves the settings.
func (f *File) Set(s Settings) (Settings, error) {
	if err := s.Validate(); err != nil {
		return s, err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return s, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return s, err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return s, err
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return s, err
	}
	f.cur = s
	return s, nil
}
