// Kintsugi is a menu bar app that corrects the English the user writes in
// other apps, keeps every mistake, and shows which ones they make most.
//
// A global shortcut reads the selected text of the app in front (package
// capture, a Swift native module), a chat model of SiliconFlow corrects it
// (internal/llm), a panel that leaves that app in front shows the result,
// and every check is saved in SQLite (internal/store). The main window, a
// window of native UI drawn with Lobe MyGo's components, shows the
// statistics, the mistakes and the settings. A second shortcut explains, in
// the same panel, English the user reads in plain English.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/youngle316/kintsugi/internal/settings"
	"github.com/youngle316/kintsugi/internal/store"
	"github.com/youngle316/kintsugi/modules/capture"
)

func main() {
	// One instance: a second one would register the shortcut twice.
	// mygo generate runs main too, beside a running app.
	if os.Getenv("MYGO_GENERATE") == "" && !mygo.App.RequestSingleInstanceLock() {
		return
	}
	k := &Kintsugi{view: mainState{rangeIndex: 1}} // the overview of 30 days
	mygo.App.OnSecondInstance(func([]string, string) { go k.OpenWindow("") })
	// Checks GitHub Releases in the background and offers new versions;
	// builds without updates (development, no public key) never check.
	mygo.Use(updater.Plugin)

	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory) // menu bar only, no Dock icon
	mygo.App.OnWindowAllClosed(func() {})                        // keep running in the menu bar

	mygo.App.WhenReady(func() {
		if err := k.start(); err != nil {
			log.Println("kintsugi:", err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// start opens the data, then shows the menu bar icon and registers the
// shortcut. It runs on the main thread once the app is ready.
func (k *Kintsugi) start() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	if k.settings, err = settings.Load(filepath.Join(dir, "settings.json")); err != nil {
		return err
	}
	if k.db, err = store.Open(context.Background(), filepath.Join(dir, "kintsugi.db")); err != nil {
		return err
	}
	k.capture, err = capture.NewCapture()
	if err != nil {
		// Without the module (on another system), only the clipboard works.
		log.Println("capture module:", err)
		k.capture = nil
	}
	mygo.App.OnQuit(func() { k.db.Close() })

	k.makePanel()
	k.makeTray()
	s := k.settings.Get()
	if err := k.registerShortcuts(s); err != nil {
		log.Println("shortcuts:", err)
	}
	if !s.Onboarded || !k.HasAPIKey() || !k.Permission().Trusted && k.capture != nil {
		k.OpenWindow("")
	}
	return nil
}

// registerShortcuts makes s's shortcuts check and explain the selection,
// in place of the ones before. When one fails, the ones before are kept;
// at start, with none before, those that worked are kept.
func (k *Kintsugi) registerShortcuts(s settings.Settings) error {
	k.mu.Lock()
	old := [2]string{k.shortcut, k.explain}
	k.mu.Unlock()
	accels := [2]string{s.Shortcut, s.ExplainShortcut}
	runs := [2]func(){k.checkSelection, k.explainSelection}
	register := func(i int, accel string) error {
		return mygo.GlobalShortcut.Register(accel, func() { go runs[i]() })
	}
	// All of them first, so that the two can swap.
	for _, a := range old {
		if a != "" {
			mygo.GlobalShortcut.Unregister(a)
		}
	}
	var ok [2]bool
	var errs []error
	for i, a := range accels {
		if err := register(i, a); err != nil {
			errs = append(errs, fmt.Errorf("the shortcut %s: %w", a, err))
			continue
		}
		ok[i] = true
	}
	cur := accels
	if len(errs) > 0 {
		for i := range accels {
			if ok[i] && old[i] != "" {
				mygo.GlobalShortcut.Unregister(accels[i])
			}
		}
		for i := range accels {
			switch {
			case old[i] != "":
				if register(i, old[i]) == nil {
					cur[i] = old[i]
				} else {
					cur[i] = ""
				}
			case !ok[i]:
				cur[i] = ""
			}
		}
	}
	k.mu.Lock()
	k.shortcut, k.explain = cur[0], cur[1]
	k.mu.Unlock()
	return errors.Join(errs...)
}
