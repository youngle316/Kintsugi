package main

// The main window: the overview, the mistakes, the settings, and the
// first-run guide, in native UI drawn with Lobe MyGo's components.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/choice"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/motion"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/youngle316/kintsugi/internal/llm"
	"github.com/youngle316/kintsugi/internal/settings"
	"github.com/youngle316/kintsugi/internal/store"
)

// The pages of the main window.
const (
	pageWelcome  = "welcome"
	pageOverview = "overview"
	pageMistakes = "mistakes"
	pageSettings = "settings"
)

var navPages = []struct {
	id, label string
	icon      func() *ui.SVG
}{
	{pageOverview, "Overview", lucide.ChartPie},
	{pageMistakes, "Mistakes", lucide.BookOpenCheck},
	{pageSettings, "Settings", lucide.Settings},
}

// overviewRanges are the periods the overview sums up, in days (0 for all).
var overviewRanges = []int{7, 30, 90, 0}

// issueRanges are the periods the mistakes list filters by, in days.
var issueRanges = []int{0, 1, 7, 30, 90}

var models = []field.AutoCompleteOption{
	{Value: "deepseek-ai/DeepSeek-V4.1-Flash"},
	{Value: "zai-org/GLM-5.3-Flash"},
	{Value: "deepseek-ai/DeepSeek-V4-Pro"},
	{Value: "moonshotai/Kimi-K3"},
}

// mainState is what the main window shows.
type mainState struct {
	page string
	gen  int // the newest load: the results of older ones are dropped
	err  string

	settings settings.Settings
	hasKey   bool
	perm     PermissionState

	rangeIndex int // into overviewRanges
	overview   store.Overview

	category, severity, pattern string
	sinceIndex                  int // into issueRanges
	issues                      []store.IssueRow

	// The settings page edits form, filled once each time it opens.
	formReady     bool
	form          settings.Settings
	hotkey        string // form.Shortcut, as display.Hotkey writes it
	explainHotkey string // form.ExplainShortcut, the same way
	keyInput      string
	login         bool
	version       VersionInfo
	status        string
	statusErr     bool
	testOut       string
	testErr       bool
	testing       bool
}

// navigate shows a page of the main window.
func (k *Kintsugi) navigate(page string) {
	v := &k.view
	if page != v.page {
		v.formReady, v.status, v.testOut, v.keyInput = false, "", "", ""
	}
	v.page = page
	k.load()
}

// reload loads the main window's page again, as after a check.
func (k *Kintsugi) reload() {
	mygo.RunOnMain(func() {
		if k.main != nil && !k.main.IsDestroyed() {
			k.load()
		}
	})
}

// load reads what the page shows off the main thread. It runs on the main
// thread.
func (k *Kintsugi) load() {
	v := &k.view
	v.gen++
	gen, page, win := v.gen, v.page, k.main
	days := overviewRanges[v.rangeIndex]
	f := store.Filter{Category: v.category, Severity: v.severity, Pattern: v.pattern}
	if d := issueRanges[v.sinceIndex]; d > 0 {
		f.Since = time.Now().AddDate(0, 0, -d).UnixMilli()
	}
	login := page == pageSettings && mygo.App.OpenAtLogin()
	go func() {
		ctx := context.Background()
		s, hasKey, perm := k.settings.Get(), k.HasAPIKey(), k.Permission()
		var o store.Overview
		var issues []store.IssueRow
		var err error
		switch page {
		case pageOverview:
			o, err = k.Overview(ctx, days)
		case pageMistakes:
			issues, err = k.db.Issues(ctx, f)
		}
		win.Update(func() {
			if gen != v.gen {
				return
			}
			v.settings, v.hasKey, v.perm, v.overview, v.issues, v.err = s, hasKey, perm, o, issues, ""
			if err != nil {
				v.err = err.Error()
			}
			if page == pageSettings && !v.formReady {
				v.form, v.hotkey, v.explainHotkey, v.login, v.version, v.formReady = s, toHotkey(s.Shortcut), toHotkey(s.ExplainShortcut), login, k.Version(), true
			}
		})
	}()
}

// mainView builds the main window.
func (k *Kintsugi) mainView(c *ui.Context) {
	theme.Apply(c)
	tk := theme.TokensOf(c)
	v := &k.view
	c.OnShortcut(ui.Cmd, ui.KeyW, func() { k.main.Close() })
	root := ui.Row(c).Fill().AlignItems(ui.Stretch)
	if !c.Vibrancy() {
		root.Background(tk.BgLayout)
	} else {
		c.Root().Background(ui.Transparent)
	}
	root.Children(func() {
		k.sidebar(c, tk)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Margin(8, 8, 8, 0).
			Radius(tk.BorderRadiusLG).Background(tk.BgContainer).Border(1, tk.BorderSecondary).Clip().
			Children(func() {
				ui.Box(c).Height(28).Shrink(0).DragWindow()
				ui.Scroll(c.Key("page-" + v.page)).Grow(1).Basis(0).Children(func() {
					ui.Column(c).AlignItems(ui.Center).Padding(4, 32, 40, 32).Children(func() {
						ui.Column(c).Width(880).MaxWidthPercent(100).Gap(20).Children(func() {
							if v.err != "" {
								feedback.Alert(c, v.err, feedback.AlertProps{Type: feedback.Error, Selectable: true})
							}
							switch v.page {
							case pageWelcome:
								k.welcomePage(c, tk)
							case pageMistakes:
								k.mistakesPage(c, tk)
							case pageSettings:
								k.settingsPage(c, tk)
							default:
								k.overviewPage(c, tk)
							}
						})
					})
				})
			})
	})
}

func (k *Kintsugi) sidebar(c *ui.Context, tk *theme.Tokens) {
	v := &k.view
	top := float32(44)
	if bar := c.TitleBar(); bar.Height > 0 {
		top = bar.Height + 8
	}
	ui.Column(c).Width(208).Shrink(0).Padding(0, 8, 12, 12).Gap(2).Children(func() {
		ui.Box(c).Height(top).Shrink(0).Margin(0, -8, 0, -12).DragWindow() // the title bar
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 8, 16, 8).Children(func() {
			ui.Image(c, logo).Width(22).Height(22).TextColor(tk.Text)
			ui.Text(c, "Kintsugi").FontSize(16).FontWeight(600).TextColor(tk.Text)
		})
		for _, p := range navPages {
			if navItem(c.Key(p.id), tk, p.label, p.icon(), v.page == p.id) {
				k.navigate(p.id)
			}
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 8).Children(func() {
			display.Text(c, "Check", display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
			display.Hotkey(c, toHotkey(v.settings.Shortcut), display.HotkeyProps{Size: theme.Small})
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 8).Children(func() {
			display.Text(c, "Explain", display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
			display.Hotkey(c, toHotkey(v.settings.ExplainShortcut), display.HotkeyProps{Size: theme.Small})
		})
	})
}

func navItem(c *ui.Context, tk *theme.Tokens, label string, icon *ui.SVG, active bool) bool {
	row := ui.ButtonBase(c).Label(label).Role(ui.RoleTab).AccessSelected(active).Row().Height(36).PaddingX(8).
		Gap(10).AlignItems(ui.Center).Justify(ui.Start).Radius(tk.BorderRadius).Cursor(ui.CursorPointer)
	hovered := row.Hovered()
	switch {
	case active && hovered:
		row.Background(tk.FillSecondary)
	case active, hovered:
		row.Background(tk.FillTertiary)
	}
	text := tk.TextSecondary
	if active {
		text = tk.Text
	}
	row.Children(func() {
		ui.Icon(c, icon).FontSize(17).TextColor(text)
		ui.Text(c, label).TextColor(text).SingleLine()
	})
	return row.Clicked()
}

// pageHeader is a page's title, with controls at its end.
func pageHeader(c *ui.Context, title string, extra func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(32).Children(func() {
		display.Text(c, title, display.TextProps{Heading: 3})
		ui.Spacer(c)
		if extra != nil {
			extra()
		}
	})
}

// section is a titled card.
func section(c *ui.Context, tk *theme.Tokens, title, extra string, body func()) ui.Element {
	return layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined}).Padding(16, 20).Gap(12).
		Radius(tk.BorderRadiusLG).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontWeight(600).TextColor(tk.Text)
			if extra != "" {
				display.Text(c, extra, display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
			}
		})
		body()
	})
}

// filterSelect is field.Select at its small size, whose popup scrolls:
// Lobe MyGo's caps the popup at 256 but lets the options spill out below.
func filterSelect(c *ui.Context, selected *string, options []field.SelectOption) {
	tk := theme.TokensOf(c)
	sel := ui.SelectBase(c, selected)
	tr := sel.Trigger.MinHeight(24).PaddingX(8).Gap(8).Radius(tk.BorderRadiusSM).FontSize(12).
		AlignItems(ui.Center).TextColor(tk.Text).Background(tk.BgContainer).Cursor(ui.CursorPointer).Unselectable().
		Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Ease: motion.EaseOut, Colors: true, Shadows: true})
	border := tk.BorderSecondary
	switch {
	case sel.Open():
		border = tk.Primary
		tr.Shadow(0, 0, 0, 2, tk.PrimaryBg)
	case tr.Hovered():
		border = tk.Border
	}
	tr.Border(1, border)
	label := ""
	for _, o := range options {
		if o.Value == *selected {
			label = o.Label
		}
	}
	tr.Children(func() {
		ui.Text(c, label).Grow(1).MinWidth(0).SingleLine()
		turn := float32(0)
		if sel.Open() {
			turn = 180
		}
		arrow := ui.Icon(c, lucide.ChevronDown()).FontSize(16).TextColor(tk.TextSecondary).Shrink(0)
		arrow.Rotate(arrow.AnimateTo("turn", turn, ui.Animation{Duration: 150 * time.Millisecond, Ease: motion.EaseOut}))
	})
	width := tr.Bounds().W
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Radius(tk.BorderRadius).MinWidth(width).Clip().
			Background(tk.BgElevated).Transition(motion.Popup()).AttachedOrigin()
		tk.Elevate(panel, theme.BoxShadowSecondary)
		panel.Children(func() {
			ui.Scroll(c).MaxHeight(256).Children(func() {
				ui.Column(c).Padding(4).Children(func() {
					for _, o := range options {
						c.Key(o.Value)
						item := sel.Item(o.Value).Padding(6, 12).MinHeight(32).Gap(8).AlignItems(ui.Center).
							Radius(tk.BorderRadiusSM).AlignSelf(ui.Stretch).FontSize(14).TextColor(tk.Text).Cursor(ui.CursorPointer)
						if item.Highlighted() {
							item.Background(tk.FillTertiary)
						}
						item.Children(func() {
							ui.Text(c, o.Label).Grow(1).SingleLine()
							if o.Value == *selected {
								ui.Icon(c, lucide.Check()).FontSize(14).TextColor(tk.Primary).Margin(0, 0, 0, 8)
							}
						})
					}
				})
			})
		})
	})
}

// ---- Overview ----

func (k *Kintsugi) overviewPage(c *ui.Context, tk *theme.Tokens) {
	v := &k.view
	o := v.overview
	pageHeader(c, "Overview", func() {
		before := v.rangeIndex
		action.Segmented(c, &v.rangeIndex, []string{"7 days", "30 days", "90 days", "All time"}, action.SegmentedProps{Size: theme.Small})
		if v.rangeIndex != before {
			k.load()
		}
	})
	stats := []struct {
		value, label string
		gold         bool
	}{
		{fmt.Sprint(o.Submissions), "Checks", false},
		{fmt.Sprint(o.Words), "Words", false},
		{fmt.Sprintf("%.2f", o.ErrorsPer100), "Errors per 100 words", true},
		{fmt.Sprint(o.Errors), "Errors", false},
		{fmt.Sprint(o.Suggestions), "Suggestions", false},
		{fmt.Sprint(o.CleanChecks), "Clean checks", false},
	}
	for row := range 2 {
		ui.Row(c.Key(row)).Gap(12).AlignItems(ui.Stretch).Children(func() {
			for _, s := range stats[row*3 : row*3+3] {
				layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined}).Grow(1).Basis(0).
					Padding(14, 16).Gap(4).Radius(tk.BorderRadiusLG).Children(func() {
					color := tk.Text
					if s.gold {
						color = tk.Gold
					}
					ui.Text(c, s.value).FontSize(26).FontWeight(600).FontFeatures("tnum").TextColor(color)
					display.Text(c, s.label, display.TextProps{Type: display.TextSecondary, FontSize: tk.FontSizeSM})
				})
			}
		})
	}
	if o.Submissions == 0 {
		layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined}).Padding(40, 20).Radius(tk.BorderRadiusLG).Children(func() {
			display.Empty(c, display.EmptyProps{
				Icon:        lucide.TextSelect(),
				Title:       "No checks yet",
				Description: "Select some English you wrote in any app and press " + prettyAccel(v.settings.Shortcut) + ".",
			})
		})
		return
	}
	lang := v.settings.Language
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		section(c, tk, "Mistakes by type", "", func() {
			var top int64
			for _, cat := range o.Categories {
				top = max(top, cat.Count)
			}
			if len(o.Categories) == 0 {
				display.Text(c, "None yet", display.TextProps{Type: display.TextSecondary})
			}
			for i, cat := range o.Categories {
				ui.Column(c.Key(i)).Gap(6).Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, categoryLabel(cat.Key, lang)).Grow(1).FontSize(tk.FontSizeSM).TextColor(tk.TextSecondary).SingleLine()
						ui.Text(c, fmt.Sprint(cat.Count)).FontSize(tk.FontSizeSM).FontFeatures("tnum").TextColor(tk.Text)
					})
					ui.Box(c).Height(6).Radius(3).Background(tk.FillTertiary).Children(func() {
						ui.Box(c).Height(6).Radius(3).Background(tk.Gold).WidthPercent(float32(cat.Count) / float32(top) * 100)
					})
				})
			}
		}).Grow(1).Basis(0)
		section(c, tk, "Top 10 patterns", "last 30 days", func() {
			if len(o.TopPatterns) == 0 {
				display.Text(c, "None yet", display.TextProps{Type: display.TextSecondary})
			}
			ui.Column(c).Gap(2).Margin(0, -8).Children(func() {
				for i, p := range o.TopPatterns {
					row := ui.ButtonBase(c.Key(i)).Label(p.Key).Row().Gap(10).Padding(6, 8).AlignItems(ui.Center).
						Justify(ui.Start).Radius(tk.BorderRadius).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(tk.FillQuaternary)
					}
					row.Children(func() {
						ui.Text(c, fmt.Sprint(i+1)).Width(16).FontSize(tk.FontSizeSM).TextColor(tk.TextQuaternary).FontFeatures("tnum")
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, p.Key).TextColor(tk.Text).SingleLine()
							ui.Text(c, categoryLabel(p.Label, lang)).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary).SingleLine()
						})
						display.Tag(c, fmt.Sprint(p.Count), display.TagProps{Size: theme.Small})
					})
					if row.Clicked() {
						v.category, v.severity, v.sinceIndex, v.pattern = "", "", 0, p.Key
						k.navigate(pageMistakes)
					}
				}
			})
		}).Grow(1).Basis(0)
	})
}

// ---- Mistakes ----

func (k *Kintsugi) mistakesPage(c *ui.Context, tk *theme.Tokens) {
	v := &k.view
	lang := v.settings.Language
	pageHeader(c, "Mistakes", func() {
		display.Text(c, fmt.Sprintf("%d shown", len(v.issues)), display.TextProps{Type: display.TextSecondary})
	})
	categories := []field.SelectOption{{Value: "", Label: "All types"}}
	for _, cat := range llm.Categories {
		categories = append(categories, field.SelectOption{Value: cat, Label: categoryLabel(cat, lang)})
	}
	periods := make([]field.SelectOption, len(issueRanges))
	for i, d := range issueRanges {
		periods[i] = field.SelectOption{Value: fmt.Sprint(i), Label: "Any time"}
		if d == 1 {
			periods[i].Label = "Last day"
		} else if d > 1 {
			periods[i].Label = fmt.Sprintf("Last %d days", d)
		}
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		category, severity, since := v.category, v.severity, fmt.Sprint(v.sinceIndex)
		ui.Box(c).Width(200).Children(func() {
			filterSelect(c.Key("category"), &category, categories)
		})
		ui.Box(c).Width(180).Children(func() {
			filterSelect(c.Key("severity"), &severity, []field.SelectOption{
				{Value: "", Label: "Errors & suggestions"}, {Value: "error", Label: "Errors"}, {Value: "suggestion", Label: "Suggestions"},
			})
		})
		ui.Box(c).Width(132).Children(func() {
			filterSelect(c.Key("since"), &since, periods)
		})
		if v.pattern != "" {
			display.Tag(c, "Pattern: "+v.pattern, display.TagProps{Closable: true, Color: display.TagGold, OnClose: func() {
				v.pattern = ""
				k.load()
			}})
		}
		if category != v.category || severity != v.severity || since != fmt.Sprint(v.sinceIndex) {
			v.category, v.severity = category, severity
			fmt.Sscan(since, &v.sinceIndex)
			k.load()
		}
	})
	if len(v.issues) == 0 {
		layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined}).Padding(40, 20).Radius(tk.BorderRadiusLG).Children(func() {
			display.Empty(c, display.EmptyProps{Icon: lucide.SearchX(), Title: "No mistakes match", Description: "Change the filters, or check more of your writing."})
		})
		return
	}
	layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined}).Radius(tk.BorderRadiusLG).Clip().Children(func() {
		for i, r := range v.issues {
			if i > 0 {
				ui.Box(c.Key(fmt.Sprint("divider", r.ID))).Height(1).Background(tk.BorderSecondary)
			}
			ui.Row(c.Key(r.ID)).Gap(16).Padding(14, 20).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(6).Children(func() {
					diff(c, tk, r.Original, r.Corrected)
					if r.Sentence != "" {
						ui.Text(c, r.Sentence).FontSize(tk.FontSizeSM).TextColor(tk.TextTertiary)
					}
					ui.Text(c, explanation(lang, r.ExplanationZh, r.ExplanationEn)).FontSize(tk.FontSizeSM).TextColor(tk.TextSecondary)
				})
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Start).Children(func() {
					severityTag(c, r.Severity)
					ui.Text(c, categoryLabel(r.Category, lang)).FontSize(tk.FontSizeSM).TextColor(tk.TextSecondary)
					ui.Text(c, time.UnixMilli(r.CreatedAt).Format("Jan 2, 15:04")).FontSize(tk.FontSizeSM).TextColor(tk.TextQuaternary)
				})
			})
		}
	})
}

// diff shows a mistake and its correction.
func diff(c *ui.Context, tk *theme.Tokens, original, corrected string) {
	spans := []ui.Span{}
	if original != "" {
		spans = append(spans, ui.Span{Text: original, Color: tk.Error, Strikethrough: true}, ui.Span{Text: "  →  ", Color: tk.TextQuaternary})
	}
	spans = append(spans, ui.Span{Text: corrected, Color: tk.Success, Weight: 600})
	ui.RichText(c, spans...)
}

func severityTag(c *ui.Context, severity string) {
	if severity == "error" {
		display.Tag(c, "Error", display.TagProps{Color: display.TagError, Size: theme.Small})
	} else {
		display.Tag(c, "Suggestion", display.TagProps{Color: display.TagWarning, Size: theme.Small})
	}
}

// ---- Settings ----

func (k *Kintsugi) settingsPage(c *ui.Context, tk *theme.Tokens) {
	v := &k.view
	pageHeader(c, "Settings", nil)
	if !v.formReady {
		return
	}
	f := &v.form
	section(c, tk, "SiliconFlow", "", func() {
		settingRow(c, "API key", func() {
			placeholder := "sk-…"
			if v.hasKey {
				placeholder = "Saved: type to replace"
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
					field.InputPassword(c.Key("key"), &v.keyInput, field.InputProps{Placeholder: placeholder}).Label("API key")
				})
				if action.Button(c, "Save", action.ButtonProps{}).Disabled(strings.TrimSpace(v.keyInput) == "").Clicked() {
					k.saveKey(v.keyInput)
				}
				if v.hasKey && action.Button(c, "Remove", action.ButtonProps{}).Clicked() {
					k.saveKey("")
				}
			})
		})
		settingRow(c, "API address", func() {
			field.Input(c.Key("base"), &f.BaseURL, field.InputProps{}).Label("API address")
		})
		settingRow(c, "Checking model", func() {
			field.AutoComplete(c.Key("check-model"), &f.CheckModel, field.AutoCompleteProps{Options: models}).Label("Checking model")
		})
		settingRow(c, "High-quality model", func() {
			field.AutoComplete(c.Key("quality-model"), &f.QualityModel, field.AutoCompleteProps{Options: models}).Label("High-quality model")
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			label := "Test the checking model"
			if v.testing {
				label = "Checking…"
			}
			if action.Button(c, label, action.ButtonProps{}).Disabled(v.testing).Clicked() {
				k.testModel()
			}
			if v.testOut != "" {
				t := display.TextSuccess
				if v.testErr {
					t = display.TextDanger
				}
				display.Text(c, v.testOut, display.TextProps{Type: t, FontSize: tk.FontSizeSM}).Grow(1).Basis(0)
			}
		})
	})
	section(c, tk, "Checking", "", func() {
		settingRow(c, "Explanations", func() {
			lang := 0
			if f.Language == settings.LanguageEn {
				lang = 1
			}
			action.Segmented(c.Key("lang"), &lang, []string{"中文", "English"}, action.SegmentedProps{}).AlignSelf(ui.Start)
			f.Language = []settings.Language{settings.LanguageZh, settings.LanguageEn}[lang]
		})
		settingRow(c, "Shortcut", func() {
			field.HotkeyInput(c.Key("shortcut"), &v.hotkey, field.HotkeyInputProps{ResetValue: "mod+alt+g", Platform: display.HotkeyApple})
		})
		settingRow(c, "Explain in plain English", func() {
			field.HotkeyInput(c.Key("explain-shortcut"), &v.explainHotkey, field.HotkeyInputProps{ResetValue: "mod+alt+e", Platform: display.HotkeyApple})
		})
		settingRow(c, "Open at login", func() {
			login := v.login
			choice.Switch(c.Key("login"), &v.login, choice.SwitchProps{}).Label("Open at login")
			if login != v.login {
				if err := k.SetOpenAtLogin(v.login); err != nil {
					v.login = login
					v.status, v.statusErr = err.Error(), true
				}
			}
		})
	})
	section(c, tk, "Accessibility", "", func() {
		switch {
		case !v.perm.Available:
			feedback.Alert(c, "The capture module is not loaded here: Kintsugi reads only what you copy.", feedback.AlertProps{Type: feedback.Secondary})
		case v.perm.Trusted:
			feedback.Alert(c, "Kintsugi may read and write the selection of other apps.", feedback.AlertProps{Type: feedback.Success})
		default:
			feedback.Alert(c, "Kintsugi needs the Accessibility permission to read the selection and write corrections back.", feedback.AlertProps{Type: feedback.Warning})
			ui.Row(c).Children(func() {
				if action.Button(c, "Grant Accessibility…", action.ButtonProps{Variant: action.Primary}).Clicked() {
					k.guidePermission()
				}
			})
		}
	})
	section(c, tk, "About", "", func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Kintsugi "+v.version.Version).TextColor(tk.Text)
			if v.version.Available != "" {
				display.Tag(c, v.version.Available+" is available", display.TagProps{Color: display.TagSuccess})
			}
			ui.Spacer(c)
			if v.version.CanUpdate {
				if action.Button(c, "Check for Updates…", action.ButtonProps{}).Clicked() {
					k.CheckForUpdates()
				}
			}
		})
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		if action.Button(c, "Save settings", action.ButtonProps{Variant: action.Primary}).Clicked() {
			k.saveForm()
		}
		if v.status != "" {
			t := display.TextSuccess
			if v.statusErr {
				t = display.TextDanger
			}
			display.Text(c, v.status, display.TextProps{Type: t})
		}
	})
}

// settingRow is a setting: its name, then its control.
func settingRow(c *ui.Context, label string, control func()) {
	tk := theme.TokensOf(c)
	ui.Row(c.Key(label)).Gap(24).AlignItems(ui.Center).MinHeight(40).Children(func() {
		// The control gives way first in a narrow window, not the label.
		ui.Text(c, label).Grow(1).Basis(0).MinWidth(136).TextColor(tk.Text)
		ui.Column(c).Basis(360).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(control)
	})
}

// saveForm saves the settings page.
func (k *Kintsugi) saveForm() {
	v := &k.view
	f := v.form
	f.Shortcut = toAccel(v.hotkey)
	f.ExplainShortcut = toAccel(v.explainHotkey)
	s, err := k.SaveSettings(f)
	if err != nil {
		v.status, v.statusErr = err.Error(), true
		return
	}
	v.form, v.settings, v.hotkey, v.explainHotkey, v.status, v.statusErr = s, s, toHotkey(s.Shortcut), toHotkey(s.ExplainShortcut), "Saved", false
}

// saveKey keeps the API key in the Keychain off the main thread; an empty
// key removes it.
func (k *Kintsugi) saveKey(key string) {
	v := &k.view
	win := k.main
	go func() {
		err := k.SetAPIKey(key)
		win.Update(func() {
			if err != nil {
				v.status, v.statusErr = err.Error(), true
				return
			}
			v.keyInput = ""
			k.load()
		})
	}()
}

// testModel saves the settings, then checks a sentence with the model.
func (k *Kintsugi) testModel() {
	v := &k.view
	k.saveForm()
	if v.statusErr {
		return
	}
	v.testing, v.testOut = true, ""
	model, win := v.form.CheckModel, k.main
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		out, err := k.TestModel(ctx, model)
		win.Update(func() {
			v.testing, v.testOut, v.testErr = false, out, err != nil
			if err != nil {
				v.testOut = err.Error()
			}
		})
	}()
}

// guidePermission opens the Accessibility guide, and shows the permission
// once it closes.
func (k *Kintsugi) guidePermission() {
	go func() {
		_, _ = k.GuidePermission(context.Background())
		k.reload()
	}()
}

// ---- First run ----

func (k *Kintsugi) welcomePage(c *ui.Context, tk *theme.Tokens) {
	v := &k.view
	ui.Column(c).Gap(6).Padding(12, 0, 4, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Image(c, logo).Width(30).Height(30).TextColor(tk.Text)
			display.Text(c, "Welcome to Kintsugi", display.TextProps{Heading: 2})
		})
		display.Text(c, "Mistakes aren't erased: they're kept, and mended with gold.", display.TextProps{Type: display.TextSecondary})
	})
	done := func(ok bool) func() {
		return func() {
			if ok {
				ui.Icon(c, lucide.CircleCheck()).FontSize(16).TextColor(tk.Success)
			}
		}
	}
	step := func(n int, title string, ok bool, body func()) {
		layout.Block(c.Key(n), layout.BlockProps{Variant: layout.BlockOutlined}).Padding(16, 20).Gap(10).Radius(tk.BorderRadiusLG).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%d. %s", n, title)).FontWeight(600).TextColor(tk.Text)
				done(ok)()
			})
			body()
		})
	}
	step(1, "Accessibility", v.perm.Trusted, func() {
		display.Text(c, "To read the text you select in any app and write corrections back, Kintsugi needs the Accessibility permission.", display.TextProps{Type: display.TextSecondary})
		switch {
		case !v.perm.Available:
			display.Text(c, "Not available in this build: Kintsugi reads what you copy.", display.TextProps{Type: display.TextSecondary})
		case !v.perm.Trusted:
			ui.Row(c).Children(func() {
				if action.Button(c, "Open System Settings…", action.ButtonProps{Variant: action.Primary}).Clicked() {
					k.guidePermission()
				}
			})
		}
	})
	step(2, "SiliconFlow API key", v.hasKey, func() {
		display.Text(c, "Kept in your Keychain. Get one at cloud.siliconflow.cn.", display.TextProps{Type: display.TextSecondary})
		ui.Row(c).Gap(8).Children(func() {
			placeholder := "sk-…"
			if v.hasKey {
				placeholder = "Saved"
			}
			ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
				field.InputPassword(c.Key("key"), &v.keyInput, field.InputProps{Placeholder: placeholder}).Label("API key")
			})
			if action.Button(c, "Save", action.ButtonProps{}).Disabled(strings.TrimSpace(v.keyInput) == "").Clicked() {
				k.saveKey(v.keyInput)
			}
		})
		if v.status != "" {
			display.Text(c, v.status, display.TextProps{Type: display.TextDanger, FontSize: tk.FontSizeSM})
		}
	})
	step(3, "Try it", false, func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
			display.Text(c, "Select some English you wrote anywhere and press", display.TextProps{Type: display.TextSecondary})
			display.Hotkey(c, toHotkey(v.settings.Shortcut), display.HotkeyProps{Size: theme.Small})
			display.Text(c, "With nothing selected, Kintsugi checks the whole field.", display.TextProps{Type: display.TextSecondary})
		})
	})
	ui.Row(c).Children(func() {
		label := "Skip for now"
		if v.hasKey && (v.perm.Trusted || !v.perm.Available) {
			label = "Start"
		}
		if action.Button(c, label, action.ButtonProps{Variant: action.Primary}).Clicked() {
			_ = k.FinishOnboarding()
			k.navigate(pageOverview)
		}
	})
}

// ---- Words ----

var categoryLabels = map[string][2]string{
	"article":                 {"冠词", "Articles"},
	"tense":                   {"时态", "Tense"},
	"subject_verb_agreement":  {"主谓一致", "Subject–verb agreement"},
	"number_countability":     {"单复数与可数性", "Number & countability"},
	"preposition_collocation": {"介词与搭配", "Prepositions & collocations"},
	"part_of_speech":          {"词性误用", "Part of speech"},
	"word_choice":             {"词汇选择", "Word choice"},
	"chinglish":               {"中式表达", "Chinglish"},
	"sentence_structure":      {"句子结构", "Sentence structure"},
	"punctuation":             {"标点", "Punctuation"},
	"spelling":                {"拼写", "Spelling"},
	"capitalization":          {"大小写", "Capitalization"},
	"wordiness":               {"冗余啰嗦", "Wordiness"},
	"tone":                    {"语气与正式程度", "Tone & formality"},
}

// categoryLabel names a kind of mistake in the explanations' language.
func categoryLabel(key string, lang settings.Language) string {
	l, ok := categoryLabels[key]
	switch {
	case !ok:
		return key
	case lang == settings.LanguageEn:
		return l[1]
	}
	return l[0] + " · " + l[1]
}

// explanation picks the explanation in lang, else the other one.
func explanation(lang settings.Language, zh, en string) string {
	if lang == settings.LanguageEn && en != "" || zh == "" {
		return en
	}
	return zh
}

// toHotkey writes an accelerator ("Alt+Cmd+G") as display.Hotkey takes
// keys ("mod+alt+g").
func toHotkey(accel string) string {
	var mods []string
	parts := strings.Split(accel, "+")
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "cmd", "command", "cmdorctrl", "commandorcontrol", "super", "meta":
			mods = append(mods, "mod")
		case "ctrl", "control":
			mods = append(mods, "ctrl")
		case "alt", "option":
			mods = append(mods, "alt")
		case "shift":
			mods = append(mods, "shift")
		}
	}
	// In the order field.HotkeyInput records them.
	order := map[string]int{"mod": 0, "ctrl": 1, "alt": 2, "shift": 3}
	slices.SortFunc(mods, func(a, b string) int { return order[a] - order[b] })
	return strings.Join(append(mods, strings.ToLower(parts[len(parts)-1])), "+")
}

// toAccel writes display.Hotkey's keys ("mod+alt+g") as an accelerator
// ("Cmd+Alt+G").
func toAccel(keys string) string {
	parts := strings.Split(keys, "+")
	for i, p := range parts {
		switch p {
		case "mod", "meta":
			parts[i] = "Cmd"
		case "ctrl", "alt", "shift":
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		default:
			if len(p) == 1 {
				parts[i] = strings.ToUpper(p)
			} else if p != "" {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
	}
	return strings.Join(parts, "+")
}

// prettyAccel writes an accelerator as macOS does: Alt+Cmd+G is ⌥⌘G.
func prettyAccel(accel string) string {
	sym := map[string]string{"ctrl": "⌃", "control": "⌃", "alt": "⌥", "option": "⌥", "shift": "⇧", "cmd": "⌘", "command": "⌘", "cmdorctrl": "⌘"}
	parts := strings.Split(accel, "+")
	var out string
	for _, m := range []string{"⌃", "⌥", "⇧", "⌘"} {
		for _, p := range parts[:len(parts)-1] {
			if sym[strings.ToLower(p)] == m {
				out += m
			}
		}
	}
	return out + strings.ToUpper(parts[len(parts)-1])
}

// The mark, as resources/icon.svg draws it: a bowl broken in two along a
// crack, mended with gold, in a grid of 24, centered at 12, 13.6.
const (
	markBowl = "M1.37 6.25L1.27 6.28L1.15 6.35L1.09 6.44L1.05 6.57L1.05 8.60L1.08 8.73L1.18 8.87L1.33 8.94L2.05 8.95L2.15 9.67L2.30 10.32L2.49 10.97L2.73 11.60L3.01 12.21L3.34 12.80L3.71 13.37L4.34 14.17L5.05 14.91L5.84 15.56L6.70 16.14L7.31 16.48L8.25 16.90L9.24 17.23L10.26 17.45L11.19 17.57L12.62 14.17L9.83 11.28L12.55 7.37L12.55 6.25ZM12.82 17.57L14.08 17.39L14.76 17.23L15.75 16.90L16.69 16.48L17.59 15.96L18.16 15.56L18.95 14.91L19.66 14.17L20.29 13.37L20.66 12.80L20.99 12.21L21.27 11.60L21.51 10.97L21.70 10.32L21.85 9.67L21.95 8.95L22.67 8.94L22.82 8.87L22.92 8.73L22.95 8.60L22.95 6.60L22.91 6.44L22.82 6.33L22.73 6.28L22.60 6.25L14.05 6.25L14.05 7.83L11.77 11.12L14.38 13.83ZM8.94 20.74L9.03 20.85L9.20 20.90L14.89 20.89L15.01 20.81L15.09 20.69L15.69 18.69L15.69 18.51L15.57 18.35L15.40 18.30L12.51 18.30L12.39 18.59L11.70 18.30L8.51 18.31L8.35 18.43L8.31 18.54L8.31 18.66Z"
	markSeam = "M11.30 17.58L11.65 17.59L12.00 17.60L12.35 17.59L12.70 17.58L12.82 17.57L14.38 13.83L11.77 11.12L14.05 7.83L14.05 6.25L12.55 6.25L12.55 7.37L9.83 11.28L12.62 14.17L11.19 17.57ZM11.70 18.30L12.39 18.59L12.51 18.30Z"
)

// logo is the mark in the windows: the bowl in the color of the text, the
// seam in gold. Show it with ui.Image, which keeps the gold.
var logo = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 1.6 24 24"><path fill="currentColor" d="` + markBowl + `"/><path fill="#E3A93F" d="` + markSeam + `"/></svg>`))
