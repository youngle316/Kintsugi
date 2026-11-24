package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/youngle316/kintsugi/internal/llm"
)

func gapCheck() *Check {
	return &Check{Mode: modeCheck, State: "done", Language: "zh", CanReplace: true,
		Original: "Don't 挡住 the text.",
		Result: &llm.Result{CorrectedText: "Don't cover the text.", Issues: []llm.Issue{}, Gaps: []llm.Gap{{
			Original: "挡住", Corrected: "cover", Alternatives: []string{"block", "hide"},
			NoteZh: "cover 指盖在上面。", NoteEn: "Cover means to be on top.", Example: "Snow covered the car."}}}}
}

func TestPanelShowsGaps(t *testing.T) {
	k := &Kintsugi{current: gapCheck()}
	tt := ui.NewTester(k.panelView, 520, 440)
	for _, s := range []string{"补词", "block", "hide", "cover 指盖在上面。", "“Snow covered the car.”"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	if tt.HasText("Looks good") {
		t.Error("a text with gaps looks good")
	}
	if _, ok := tt.Find("Replace (⌘↩)"); !ok {
		t.Error("a text with gaps can't be replaced")
	}
}

func TestPanelShowsGapsInEnglish(t *testing.T) {
	ch := gapCheck()
	ch.Language = "en"
	tt := ui.NewTester((&Kintsugi{current: ch}).panelView, 520, 440)
	if !tt.HasText("Cover means to be on top.") || tt.HasText("cover 指盖在上面。") {
		t.Errorf("%q", tt.Texts())
	}
}

func TestHighlightMarksGapsAndIssuesInOrder(t *testing.T) {
	res := &llm.Result{CorrectedText: "The panel shows it and covers text.",
		Gaps:   []llm.Gap{{Original: "挡住", Corrected: "covers"}},
		Issues: []llm.Issue{{Original: "show", Corrected: "shows", Severity: "error"}, {Original: "and cover", Corrected: "and covers", Severity: "error"}}}
	tk := &theme.Tokens{}
	var marked, all string
	for _, s := range highlight(tk, res, -1) {
		all += s.Text
		if s.DecorationThickness > 0 {
			marked += "[" + s.Text + "]"
		}
	}
	if all != res.CorrectedText || marked != "[shows][covers]" {
		t.Errorf("%q, marked %q", all, marked)
	}
}
