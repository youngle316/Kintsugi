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

// CaptureMode says how Kintsugi reads text from an app.
type CaptureMode string

const (
	// ModeAuto reads with the accessibility API, then a simulated ⌘C, and
	// writes back with the accessibility API, then a simulated ⌘V.
	ModeAuto CaptureMode = "auto"
	// ModeClipboard skips the accessibility API: simulated ⌘C and ⌘V only.
	ModeClipboard CaptureMode = "clipboard"
	// ModeManual reads what the user copied themselves, and puts the
	// correction on the clipboard instead of writing it back.
	ModeManual CaptureMode = "manual"
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
	// AppModes overrides the capture mode by bundle ID, for apps where
	// step 0 found that automatic capture fails.
	AppModes map[string]CaptureMode `json:"appModes"`
	// Onboarded is set once the first-run guide was finished or skipped.
	Onboarded bool `json:"onboarded"`
}

// Defaults are the settings of a new install.
func Defaults() Settings {
	return Settings{
		BaseURL:      llm.DefaultBaseURL,
		CheckModel:   "deepseek-ai/DeepSeek-V4.1-Flash",
		QualityModel: "deepseek-ai/DeepSeek-V4-Pro",
		Language:     LanguageZh,
		Shortcut:     "Alt+Cmd+G",
		AppModes:     map[string]CaptureMode{},
	}
}

// ModeFor returns the capture mode of an app.
func (s Settings) ModeFor(bundleID string) CaptureMode {
	if m, ok := s.AppModes[bundleID]; ok {
		return m
	}
	return ModeAuto
}

// Validate checks and tidies the settings.
func (s *Settings) Validate() error {
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.CheckModel = strings.TrimSpace(s.CheckModel)
	s.QualityModel = strings.TrimSpace(s.QualityModel)
	s.Shortcut = strings.TrimSpace(s.Shortcut)
	switch {
	case !strings.HasPrefix(s.BaseURL, "https://") && !strings.HasPrefix(s.BaseURL, "http://"):
		return errors.New("the API address must start with https://")
	case s.CheckModel == "":
		return errors.New("choose a model for checking")
	case s.Shortcut == "":
		return errors.New("choose a shortcut")
	}
	if s.QualityModel == "" {
		s.QualityModel = s.CheckModel
	}
	if s.Language != LanguageEn {
		s.Language = LanguageZh
	}
	for id, m := range s.AppModes {
		if m != ModeClipboard && m != ModeManual {
			delete(s.AppModes, id) // auto is the default: not kept
		}
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
	if f.cur.AppModes == nil {
		f.cur.AppModes = map[string]CaptureMode{}
	}
	return f, nil
}

// Get returns the settings.
func (f *File) Get() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.cur
	s.AppModes = make(map[string]CaptureMode, len(f.cur.AppModes))
	for k, v := range f.cur.AppModes {
		s.AppModes[k] = v
	}
	return s
}

// Set validates and saves the settings.
func (f *File) Set(s Settings) (Settings, error) {
	if s.AppModes == nil {
		s.AppModes = map[string]CaptureMode{}
	}
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
