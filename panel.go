package main

// The correction panel: shown by the shortcut over the app the user writes
// in, which stays in front. Replace writes back, Copy copies, Esc ignores.
// The explain shortcut shows an explanation in it instead, to copy.
// It drags by any free part of it, and pinned, it stays where it was put
// and stays up as the user goes back to writing.

import (
	"fmt"
	"log"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/youngle316/kintsugi/internal/llm"
	"github.com/youngle316/kintsugi/internal/settings"
)

// panelState is what the panel shows beside the check.
type panelState struct {
	hot    int  // the issue under the pointer, -1 for none
	busy   bool // an action is running
	pinned bool // stays up when it loses the focus, where it was put
}

// panelView builds the panel.
func (k *Kintsugi) panelView(c *ui.Context) {
	theme.Apply(c)
	tk := theme.TokensOf(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ch := k.Current()
	done := ch != nil && ch.State == "done"
	explain := ch != nil && ch.Mode == modeExplain
	c.OnShortcut(0, ui.KeyEscape, func() { k.act("ignore") })
	if done && !explain {
		c.OnShortcut(ui.Cmd, ui.KeyEnter, func() { k.act(replaceAction(ch)) })
	}
	page := ui.Column(c).Fill().Padding(14, 16).Gap(10).DragWindow()
	if !c.Vibrancy() {
		page.Background(tk.BgElevated)
	}
	page.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Image(c, logo).Width(18).Height(18).TextColor(tk.Text)
			ui.Text(c, "Kintsugi").FontWeight(600).TextColor(tk.Text)
			ui.Spacer(c)
			if done && explain && ch.Explanation.Entry == nil && len(ch.Explanation.Terms) > 0 {
				display.Tag(c, plural(len(ch.Explanation.Terms), "term"), display.TagProps{Color: display.TagGold, Size: theme.Small})
			}
			if done && !explain && len(ch.Result.Issues) > 0 {
				errs := countErrors(ch.Result.Issues)
				if errs > 0 {
					display.Tag(c, plural(errs, "error"), display.TagProps{Color: display.TagError, Size: theme.Small})
				}
				if s := len(ch.Result.Issues) - errs; s > 0 {
					display.Tag(c, plural(s, "suggestion"), display.TagProps{Color: display.TagWarning, Size: theme.Small})
				}
			}
			pin, title := lucide.Pin(), "Pin"
			if k.board.pinned {
				pin, title = lucide.PinOff(), "Unpin"
			}
			if action.ActionIcon(c.Key("pin"), pin, action.ActionIconProps{Size: theme.Small, Active: k.board.pinned, Title: title}).Clicked() {
				k.board.pinned = !k.board.pinned
			}
		})
		switch {
		case ch == nil || ch.State == "loading":
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				feedback.Spin(c, feedback.SpinProps{}, nil).AlignSelf(ui.Center)
				verb := "Checking"
				if explain {
					verb = "Explaining"
				}
				label := verb + "…"
				switch {
				case explain && llm.IsPhrase(ch.Original):
					label = fmt.Sprintf("Looking up “%s”…", strings.TrimSpace(ch.Original))
				case ch != nil && ch.Original != "":
					label = fmt.Sprintf("%s %s…", verb, plural(len(strings.Fields(ch.Original)), "word"))
				}
				display.Text(c, label, display.TextProps{Shiny: true})
			})
		case ch.State == "error":
			ui.Column(c).Grow(1).Children(func() {
				feedback.Alert(c, "Couldn't "+ch.Mode, feedback.AlertProps{Type: feedback.Error, Description: ch.Error, Selectable: true})
			})
		case explain:
			explained(c, tk, ch.Explanation)
		case len(ch.Result.Issues) == 0:
			ui.Column(c).Grow(1).Center().Children(func() {
				feedback.Result(c, feedback.ResultProps{Status: feedback.ResultSuccess, Title: "Looks good", SubTitle: "No mistakes found."})
			})
		default:
			k.corrections(c, tk, ch)
		}
		k.panelFooter(c, tk, ch)
	})
}

// explained shows the text in plain English, and the terms explained; or
// the dictionary entry of a word or phrase.
func explained(c *ui.Context, tk *theme.Tokens, ex *llm.Explanation) {
	if ex.Entry != nil {
		entry(c, tk, ex.Entry)
		return
	}
	ui.Scroll(c.Key("terms")).Grow(1).Basis(0).Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.Column(c).Gap(6).Padding(10, 12).Radius(tk.BorderRadiusLG).Background(tk.FillQuaternary).Border(1, tk.BorderSecondary).Children(func() {
				ui.Text(c, "In plain English").FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
				ui.RichText(c, ui.Span{Text: ex.Plain}).Selectable().LineHeight(1.6)
			})
			for i, t := range ex.Terms {
				ui.Column(c.Key(i)).Gap(4).Padding(8, 10).Children(func() {
					ui.Text(c, t.Term).FontWeight(600).TextColor(tk.Text)
					ui.Text(c, t.Meaning).FontSize(tk.FontSizeSM).TextColor(tk.TextSecondary)
					if t.Example != "" {
						ui.Text(c, "“"+t.Example+"”").FontSize(tk.FontSizeSM).Italic().TextColor(tk.TextTertiary)
					}
				})
			}
		})
	})
}

// entry shows a dictionary entry: the headword, its meanings with an
// example each, then the related words.
func entry(c *ui.Context, tk *theme.Tokens, e *llm.Entry) {
	ui.Scroll(c.Key("entry")).Grow(1).Basis(0).Children(func() {
		ui.Column(c).Gap(14).Padding(0, 2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Baseline).Wrap().Children(func() {
				ui.Text(c, e.Headword).FontSize(22).FontWeight(600).TextColor(tk.Text)
				if e.PartOfSpeech != "" {
					ui.Text(c, e.PartOfSpeech).Italic().TextColor(tk.TextTertiary)
				}
				if e.Register != "" {
					display.Tag(c, e.Register, display.TagProps{Color: display.TagGold, Size: theme.Small})
				}
			})
			ui.Column(c).Gap(10).Children(func() {
				for i, mn := range e.Meanings {
					ui.Row(c.Key(i)).Gap(10).AlignItems(ui.Start).Children(func() {
						ui.Text(c, fmt.Sprint(i+1)).FontWeight(600).TextColor(tk.Gold).Width(12).Shrink(0)
						ui.Column(c).Grow(1).Basis(0).Gap(4).Children(func() {
							ui.Text(c, mn.Meaning).LineHeight(1.5).TextColor(tk.Text)
							if mn.Example != "" {
								ui.Text(c, "“"+mn.Example+"”").FontSize(tk.FontSizeSM).Italic().TextColor(tk.TextTertiary)
							}
						})
					})
				}
			})
			ui.Column(c).Gap(8).Children(func() {
				related(c, tk, "Synonyms", e.Synonyms)
				related(c, tk, "Opposites", e.Opposites)
				related(c, tk, "Collocations", e.Collocations)
				related(c, tk, "Word family", e.WordFamily)
			})
		})
	})
}

// related shows words related to an entry under a label, none when empty.
func related(c *ui.Context, tk *theme.Tokens, label string, words []string) {
	if len(words) == 0 {
		return
	}
	ui.Row(c.Key(label)).Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary).Width(84).Shrink(0).Padding(2, 0, 0, 0)
		ui.Row(c).Grow(1).Basis(0).Gap(6).Wrap().Children(func() {
			for i, w := range words {
				display.Tag(c.Key(i), w, display.TagProps{Size: theme.Small})
			}
		})
	})
}

// explanationText is what Copy copies of an explanation.
func explanationText(ex *llm.Explanation) string {
	e := ex.Entry
	if e == nil {
		return ex.Plain
	}
	var b strings.Builder
	b.WriteString(e.Headword)
	if e.PartOfSpeech != "" {
		fmt.Fprintf(&b, " (%s)", e.PartOfSpeech)
	}
	for i, mn := range e.Meanings {
		fmt.Fprintf(&b, "\n%d. %s", i+1, mn.Meaning)
		if mn.Example != "" {
			fmt.Fprintf(&b, "\n   “%s”", mn.Example)
		}
	}
	for _, r := range []struct {
		label string
		words []string
	}{{"Synonyms", e.Synonyms}, {"Opposites", e.Opposites}, {"Collocations", e.Collocations}, {"Word family", e.WordFamily}} {
		if len(r.words) > 0 {
			fmt.Fprintf(&b, "\n%s: %s", r.label, strings.Join(r.words, ", "))
		}
	}
	return b.String()
}

// corrections shows the corrected text with each issue marked, and the
// issues explained.
func (k *Kintsugi) corrections(c *ui.Context, tk *theme.Tokens, ch *Check) {
	res := ch.Result
	lang := settings.Language(ch.Language)
	ui.Scroll(c.Key("issues")).Grow(1).Basis(0).Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.Box(c).Padding(10, 12).Radius(tk.BorderRadiusLG).Background(tk.FillQuaternary).Border(1, tk.BorderSecondary).Children(func() {
				ui.RichText(c, highlight(tk, res.CorrectedText, res.Issues, k.board.hot)...).Selectable().LineHeight(1.6)
			})
			for i, is := range res.Issues {
				row := ui.Column(c.Key(i)).Gap(4).Padding(8, 10).Radius(tk.BorderRadius)
				if row.Hovered() {
					row.Background(tk.FillQuaternary)
					k.board.hot = i
				} else if k.board.hot == i {
					k.board.hot = -1
				}
				row.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						severityTag(c, is.Severity)
						ui.Text(c, categoryLabel(is.Category, lang)).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
					})
					diff(c, tk, is.Original, is.Corrected)
					ui.Text(c, explanation(lang, is.ExplanationZh, is.ExplanationEn)).FontSize(tk.FontSizeSM).TextColor(tk.TextSecondary)
				})
			}
		})
	})
}

func (k *Kintsugi) panelFooter(c *ui.Context, tk *theme.Tokens, ch *Check) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		done := ch != nil && ch.State == "done"
		explain := ch != nil && ch.Mode == modeExplain
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			display.Hotkey(c, "esc", display.HotkeyProps{Size: theme.Small})
			esc := "ignore"
			if explain {
				esc = "close"
			}
			display.Text(c, esc, display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
			if done && !explain && len(ch.Result.Issues) > 0 {
				display.Hotkey(c, "mod+enter", display.HotkeyProps{Size: theme.Small})
				display.Text(c, strings.ToLower(replaceLabel(ch)), display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
			}
		})
		ui.Spacer(c)
		small := action.ButtonProps{Size: theme.Small}
		primary := action.ButtonProps{Size: theme.Small, Variant: action.Primary}
		switch {
		case ch != nil && ch.State == "error":
			if action.Button(c, "Settings…", small).Clicked() {
				k.act("ignore")
				go k.OpenWindow(pageSettings)
			}
		case done && explain:
			if action.Button(c, "Copy", small).Disabled(k.board.busy).Clicked() {
				k.act("copy")
			}
			if action.Button(c, "Done", primary).Clicked() {
				k.act("ignore")
			}
		case done && len(ch.Result.Issues) > 0:
			if action.Button(c, "Ignore", small).Disabled(k.board.busy).Clicked() {
				k.act("ignore")
			}
			if action.Button(c, "Copy", small).Disabled(k.board.busy).Clicked() {
				k.act("copy")
			}
			if action.Button(c, replaceLabel(ch), primary).Disabled(k.board.busy).Clicked() {
				k.act(replaceAction(ch))
			}
		case done:
			if action.Button(c, "Done", primary).Clicked() {
				k.act("ignore")
			}
		default:
			if action.Button(c, "Cancel", small).Clicked() {
				k.act("ignore")
			}
		}
	})
}

// act runs a button of the panel off the main thread: replacing waits for
// the app it writes to.
func (k *Kintsugi) act(what string) {
	ch := k.Current()
	if k.board.busy {
		return
	}
	if ch == nil || ch.State != "done" || what == "ignore" {
		k.Dismiss()
		return
	}
	if ch.Mode == modeExplain {
		go k.Copy(0, explanationText(ch.Explanation)) // id 0: no check to record the action of
		return
	}
	text := ch.Result.CorrectedText
	k.board.busy = true
	p := k.panel
	go func() {
		defer p.Update(func() { k.board.busy = false })
		if what == "copy" {
			k.Copy(ch.ID, text)
			return
		}
		if _, err := k.Replace(ch.ID, text); err != nil {
			log.Println("replace:", err)
		}
	}()
}

func replaceAction(ch *Check) string {
	if ch.ManualMode || !ch.CanReplace {
		return "copy"
	}
	return "replace"
}

func replaceLabel(ch *Check) string {
	switch {
	case ch.ManualMode || !ch.CanReplace:
		return "Copy"
	case ch.WholeField:
		return "Replace all"
	}
	return "Replace"
}

// highlight marks each issue's span in the corrected text, the first time
// it appears after the previous one; the issue hot is lit.
func highlight(tk *theme.Tokens, text string, issues []llm.Issue, hot int) []ui.Span {
	var spans []ui.Span
	at := 0
	for i, is := range issues {
		if is.Corrected == "" {
			continue
		}
		j := strings.Index(text[at:], is.Corrected)
		if j < 0 {
			continue
		}
		j += at
		if j > at {
			spans = append(spans, ui.Span{Text: text[at:j]})
		}
		s := ui.Span{Text: is.Corrected, Background: tk.GoldFillTertiary, Radius: 3, Underline: true, DecorationColor: tk.Gold, DecorationThickness: 2}
		if is.Severity != "error" {
			s.Underline, s.DottedUnderline = false, true
		}
		if i == hot {
			s.Background = tk.Gold
			s.Color = tk.TextLightSolid
		}
		spans = append(spans, s)
		at = j + len(is.Corrected)
	}
	return append(spans, ui.Span{Text: text[at:]})
}

func countErrors(issues []llm.Issue) int {
	n := 0
	for _, is := range issues {
		if is.Severity == "error" {
			n++
		}
	}
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
