package main

// The clipboard panel's view: a search field and what the user copied, the
// newest first. Enter, a click or ⌘1 to ⌘9 pastes.

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/youngle316/kintsugi/internal/store"
)

// maxClipBytes is the longest text the history keeps.
const maxClipBytes = 100 * 1024

// maxLineRunes is how much of a clip a row lays out.
const maxLineRunes = 300

// clipBoard is the state of the clipboard panel, read and changed on the
// main thread only, and what it calls.
type clipBoard struct {
	clips     []store.Clip
	query     string
	sel       int // into the clips the query leaves
	pinned    bool
	recording bool
	busy      bool   // a paste is running
	notice    string // why the last paste could not be done
	focus     bool   // the search takes the focus in the next frame
	reveal    bool   // the selected row scrolls into view in the next frame

	paste, remove func(store.Clip)
	dismiss       func()
	record        func(on bool)
}

// open starts the panel over, as it shows.
func (b *clipBoard) open() {
	b.query, b.sel, b.notice, b.focus, b.reveal = "", 0, "", true, true
}

// show lists clips, the same one selected while it is there.
func (b *clipBoard) show(clips []store.Clip) {
	if old := filterClips(b.clips, b.query); b.sel > 0 && b.sel < len(old) {
		for i, c := range filterClips(clips, b.query) {
			if c.ID == old[b.sel].ID {
				b.sel = i
				break
			}
		}
	}
	b.clips = clips
}

func (b *clipBoard) view(c *ui.Context) {
	theme.Apply(c)
	tk := theme.TokensOf(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	shown := filterClips(b.clips, b.query)
	b.sel = moveSelection(b.sel, 0, len(shown))
	move := func(by int) func() {
		return func() { b.sel, b.reveal = moveSelection(b.sel, by, len(shown)), true }
	}
	pasteAt := func(i int) {
		if !b.busy && i < len(shown) {
			b.paste(shown[i])
		}
	}
	remove := func() {
		if !b.busy && b.sel < len(shown) {
			b.remove(shown[b.sel])
		}
	}
	// On the input, which has the focus; Enter is its submission.
	keys := func(on func(ui.Modifiers, ui.Key, func())) {
		on(0, ui.KeyEscape, b.dismiss)
		on(0, ui.KeyUp, move(-1))
		on(0, ui.KeyDown, move(1))
		for i := range 9 {
			on(ui.Cmd, ui.Key1+ui.Key(i), func() { pasteAt(i) })
		}
	}
	page := ui.Column(c).Fill().Padding(14, 16).Gap(10).DragWindow()
	if !c.Vibrancy() {
		page.Background(tk.BgElevated)
	}
	page.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
				before, submitted := b.query, false
				in := field.SearchBar(c.Key("search"), &b.query, field.SearchBarProps{
					Placeholder: "Search copied text", Variant: field.InputBorderless, Size: theme.Small,
					OnSearch: func(string) { submitted = true },
				}).Label("Search copied text")
				keys(func(m ui.Modifiers, k ui.Key, fn func()) { in.OnShortcut(m, k, fn) })
				// The input would take ⌘⌫ to clear its line.
				in.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind != ui.InputKeyDown || ev.Key != ui.KeyBackspace || ev.Mods != ui.Cmd {
						return false
					}
					if !ev.Repeat {
						remove()
					}
					return true
				})
				if b.focus {
					in.Focus()
					b.focus = false
				}
				if b.query != before {
					b.sel, b.reveal = 0, true
				}
				if submitted {
					shown = filterClips(b.clips, b.query)
					pasteAt(b.sel)
				}
			})
			b.icons(c)
		})
		if !b.recording {
			ui.Text(c, "Recording is paused").FontSize(tk.FontSizeSM).TextColor(tk.Warning)
		}
		if b.notice != "" {
			ui.Text(c, b.notice).FontSize(tk.FontSizeSM).TextColor(tk.Error)
		}
		shown = filterClips(b.clips, b.query)
		switch {
		case len(b.clips) == 0:
			b.empty(c, tk, "Nothing copied yet")
		case len(shown) == 0:
			b.empty(c, tk, "No matches")
		default:
			b.list(c, tk, shown, pasteAt)
		}
		ui.Row(c).Gap(16).Children(func() {
			for _, s := range []string{"↩ Paste", "⌘1–9 Paste that one", "⌘⌫ Delete"} {
				ui.Text(c.Key(s), s).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
			}
		})
	})
}

func (b *clipBoard) icons(c *ui.Context) {
	rec, title := lucide.Pause(), "Pause recording"
	if !b.recording {
		rec, title = lucide.Play(), "Resume recording"
	}
	if action.ActionIcon(c.Key("record"), rec, action.ActionIconProps{Size: theme.Small, Active: !b.recording, Title: title}).Clicked() {
		b.record(!b.recording)
	}
	pin, title := lucide.Pin(), "Pin"
	if b.pinned {
		pin, title = lucide.PinOff(), "Unpin"
	}
	if action.ActionIcon(c.Key("pin"), pin, action.ActionIconProps{Size: theme.Small, Active: b.pinned, Title: title}).Clicked() {
		b.pinned = !b.pinned
	}
	if action.ActionIcon(c.Key("close"), lucide.X(), action.ActionIconProps{Size: theme.Small, Title: "Close (Esc)"}).Clicked() {
		b.dismiss()
	}
}

func (b *clipBoard) empty(c *ui.Context, tk *theme.Tokens, text string) {
	ui.Column(c.Key("empty")).Grow(1).Center().Children(func() {
		ui.Text(c, text).TextColor(tk.TextTertiary)
	})
}

func (b *clipBoard) list(c *ui.Context, tk *theme.Tokens, shown []store.Clip, pasteAt func(int)) {
	now := time.Now()
	ui.Scroll(c.Key("clips")).Grow(1).Basis(0).Children(func() {
		ui.Column(c).Gap(2).AlignItems(ui.Stretch).Children(func() {
			for i, cl := range shown {
				row := ui.ButtonBase(c.Key(cl.ID)).Label(oneLine(cl.Text)).AlignItems(ui.Stretch).Padding(8, 10).Radius(tk.BorderRadius)
				if i == b.sel {
					row.Background(tk.FillSecondary)
					if b.reveal {
						row.ScrollIntoView()
						b.reveal = false
					}
				} else if row.Hovered() {
					row.Background(tk.FillQuaternary)
				}
				if row.Clicked() {
					pasteAt(i)
				}
				row.Children(func() {
					ui.Row(c).Grow(1).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Text(c, oneLine(cl.Text)).Grow(1).Basis(0).MinWidth(0).MaxLines(1).TextColor(tk.Text)
						meta := age(now.Sub(cl.CopiedAt))
						if cl.App != "" {
							meta = cl.App + " · " + meta
						}
						ui.Text(c, meta).Shrink(0).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
						if i < 9 {
							ui.Text(c, fmt.Sprintf("⌘%d", i+1)).Shrink(0).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
						}
					})
				})
			}
		})
	})
}

// keepClip reports whether the history keeps a text.
func keepClip(text string) bool {
	return len(text) <= maxClipBytes && strings.TrimSpace(text) != ""
}

// filterClips is the clips that hold the query, whatever its case.
func filterClips(clips []store.Clip, query string) []store.Clip {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return clips
	}
	var out []store.Clip
	for _, cl := range clips {
		if strings.Contains(strings.ToLower(cl.Text), query) {
			out = append(out, cl)
		}
	}
	return out
}

// oneLine is the start of a text on one line.
func oneLine(text string) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if len(r) > maxLineRunes {
		return string(r[:maxLineRunes]) + "…"
	}
	return string(r)
}

// age says how long ago, shortly.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// moveSelection moves sel by a step within n rows.
func moveSelection(sel, by, n int) int { return max(0, min(sel+by, n-1)) }
