package main

// The clipboard history: what the user copies is kept on this Mac, and a
// panel over the app in front pastes it there again.

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/youngle316/kintsugi/internal/store"
)

var errNoPaste = errors.New("no app to paste into")

// startClipboard makes the panel and starts recording, when it is on. It
// runs on the main thread.
func (k *Kintsugi) startClipboard() {
	b := &k.clips
	b.recording = k.settings.Get().RecordClipboard
	b.dismiss = func() { k.clipWin.Hide() }
	b.record = k.setRecording
	b.paste = k.pasteClip
	b.remove = func(c store.Clip) {
		k.changeClips(func(ctx context.Context) error { return k.db.DeleteClip(ctx, c.ID) })
	}
	k.clipWin = newPanel(b.view, func() bool { return b.pinned })
	k.clipChanged = make(chan struct{}, 1)
	go func() {
		for range k.clipChanged {
			k.recordClip()
		}
	}()
	k.watchClipboard(b.recording)
}

// watchClipboard starts or stops watching. It runs on the main thread.
func (k *Kintsugi) watchClipboard(on bool) {
	if k.stopWatch != nil {
		k.stopWatch()
		k.stopWatch = nil
	}
	if !on || k.capture == nil {
		return
	}
	k.stopWatch = mygo.Clipboard.OnChanged(func() {
		select {
		case k.clipChanged <- struct{}{}:
		default: // one to come reads the newest
		}
	})
}

// recordClip keeps what the clipboard holds now.
func (k *Kintsugi) recordClip() {
	cl, err := k.capture.ReadClip()
	if err != nil {
		log.Println("reading the clipboard:", err)
		return
	}
	// Paused since the change: not to be kept.
	if cl.Skip || !keepClip(cl.Text) || !k.settings.Get().RecordClipboard {
		return
	}
	err = k.saveClips(func(ctx context.Context) error {
		return k.db.SaveClip(ctx, store.Clip{Text: cl.Text, App: cl.App.Name, CopiedAt: time.Now()})
	})
	if err != nil {
		log.Println("saving the clip:", err)
	}
}

// saveClips changes the history and shows it in the panel, one change at a
// time, off the main thread.
func (k *Kintsugi) saveClips(change func(context.Context) error) error {
	k.clipMu.Lock()
	defer k.clipMu.Unlock()
	ctx := context.Background()
	if change != nil {
		if err := change(ctx); err != nil {
			return err
		}
	}
	clips, err := k.db.Clips(ctx)
	if err != nil {
		return err
	}
	k.clipWin.Update(func() { k.clips.show(clips) })
	return nil
}

func (k *Kintsugi) changeClips(change func(context.Context) error) {
	go func() {
		if err := k.saveClips(change); err != nil {
			log.Println("changing the clipboard history:", err)
			k.clipWin.Update(func() { k.clips.notice = "Couldn't change the history: " + err.Error() })
		}
	}()
}

// ClearClips empties the history.
func (k *Kintsugi) ClearClips() error {
	return k.saveClips(k.db.ClearClips)
}

// setRecording records what the user copies from now on, or stops. It runs
// on the main thread.
func (k *Kintsugi) setRecording(on bool) {
	s := k.settings.Get()
	s.RecordClipboard = on
	if _, err := k.settings.Set(s); err != nil {
		log.Println("saving the settings:", err)
		k.clips.notice = "Couldn't save the setting: " + err.Error()
		on = k.settings.Get().RecordClipboard
	}
	// What the settings page is editing follows.
	k.view.form.RecordClipboard, k.view.settings.RecordClipboard = on, on
	k.clips.recording = on
	if k.pauseItem != nil {
		k.pauseItem.SetChecked(!on)
	}
	k.watchClipboard(on)
	k.clipWin.Update(func() {})
	if k.main != nil && !k.main.IsDestroyed() {
		k.main.Update(func() {})
	}
}

// openClips shows the panel. It runs on a goroutine of its own.
func (k *Kintsugi) openClips() {
	pid := 0
	if k.capture != nil {
		if app, err := k.capture.FrontApp(); err == nil {
			pid = app.PID
		}
	}
	if err := k.saveClips(nil); err != nil {
		log.Println("reading the clipboard history:", err)
	}
	mygo.RunOnMain(func() {
		if k.clips.busy {
			return
		}
		k.clipTarget = pid
		k.clips.open()
		k.clipWin.Update(func() {})
		if !k.clips.pinned {
			place(k.clipWin, nil)
		}
		k.clipWin.Show()
	})
}

// pasteClip pastes into the app that was in front as the panel opened, or
// leaves the clip on the clipboard when it cannot.
func (k *Kintsugi) pasteClip(c store.Clip) {
	b := &k.clips
	b.busy, b.notice = true, ""
	pid := k.clipTarget
	k.clipWin.Hide()
	go func() {
		err := errNoPaste
		if k.capture != nil && pid != 0 {
			_, err = k.capture.PasteText(c.Text, pid)
		}
		if err != nil {
			log.Println("pasting a clip:", err)
			mygo.Clipboard.WriteText(c.Text)
		}
		mygo.RunOnMain(func() {
			b.busy = false
			if err != nil {
				b.notice = "Couldn't paste. The text is on the clipboard: press ⌘V where you want it"
			}
			b.focus = true
			k.clipWin.Update(func() {})
			if err != nil || b.pinned {
				k.clipWin.Show()
			}
		})
	}()
}
