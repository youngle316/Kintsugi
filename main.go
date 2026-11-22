// Kintsugi is a menu bar app that corrects the English the user writes in
// other apps, keeps every mistake, and shows which ones they make most.
//
// A global shortcut reads the selected text of the app in front (package
// capture, a Swift native module), a chat model of SiliconFlow corrects it
// (internal/llm), a panel that leaves that app in front shows the result,
// and every check is saved in SQLite (internal/store). The main window, a
// web page, shows the statistics, the mistakes and the settings.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
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
	k := &Kintsugi{}
	mygo.App.OnSecondInstance(func([]string, string) { go k.OpenWindow("") })
	mygo.Bind(k)

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
	if err := k.registerShortcut(k.settings.Get().Shortcut); err != nil {
		log.Println("shortcut:", err)
	}
	if k.capture != nil {
		if err := mygo.GlobalShortcut.Register(LabShortcut, func() { go k.runLab() }); err != nil {
			log.Println("lab shortcut:", err)
		}
	}
	s := k.settings.Get()
	if !s.Onboarded || !k.HasAPIKey() || !k.Permission().Trusted && k.capture != nil {
		k.OpenWindow("")
	}
	return nil
}

// registerShortcut makes accel the shortcut that checks the selection,
// in place of the one before.
func (k *Kintsugi) registerShortcut(accel string) error {
	k.mu.Lock()
	old := k.shortcut
	k.mu.Unlock()
	if old == accel && mygo.GlobalShortcut.IsRegistered(accel) {
		return nil
	}
	if old != "" {
		mygo.GlobalShortcut.Unregister(old)
	}
	err := mygo.GlobalShortcut.Register(accel, func() { go k.checkSelection() })
	if err != nil {
		if old != "" && old != accel {
			// Keep the shortcut that worked.
			if mygo.GlobalShortcut.Register(old, func() { go k.checkSelection() }) == nil {
				return errors.Join(err, errors.New("kept "+old))
			}
		}
		return err
	}
	k.mu.Lock()
	k.shortcut = accel
	k.mu.Unlock()
	return nil
}
