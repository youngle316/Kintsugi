package llm

import (
	"context"
	"os"
	"testing"
)

// TestLiveGaps fills gaps with a real model; it needs SILICONFLOW_API_KEY,
// and takes KINTSUGI_BASE_URL and KINTSUGI_MODEL. Read its log with -v: how
// good the English is takes a person.
func TestLiveGaps(t *testing.T) {
	key, model := os.Getenv("SILICONFLOW_API_KEY"), os.Getenv("KINTSUGI_MODEL")
	if key == "" {
		t.Skip("no SILICONFLOW_API_KEY")
	}
	if model == "" {
		model = "deepseek-ai/DeepSeek-V4.1-Flash"
	}
	c := &Client{BaseURL: os.Getenv("KINTSUGI_BASE_URL"), APIKey: key}
	for _, x := range []struct {
		text         string
		gaps, issues int // issues -1: any number
	}{
		{"Can you make the popup 可以拖动, and don't let it 挡住 the text I am write?", 2, -1},
		{"Please add a 兜底 in case the request fails.", 1, 0},
		{"The list is 卡顿 when I scroll quickly.", 1, 0},
		{"Can we 顺便 fix the typo in `README.md`?", 1, 0},
		{"I want the two buttons to be 对齐.", 1, 0},
		{"This function is too 啰嗦, please simplify it.", 1, 0},
		{"He go to the meeting but 没赶上 the beginning.", 1, -1},
		{"The “设置” button does nothing.", 0, 0},
		{"Click the “设置” button and the window 闪退.", 1, -1},
		{"Please don't 一刀切, some users still need the old API.", 1, -1},
		{"The panel should 记住上次的位置 after I close it.", 1, 0},
		{"可以拖动", 1, 0},
		{"Run `make` and 看看有没有报错.", 1, 0},
		{"This is already fine.", 0, 0},
	} {
		res, err := c.Check(context.Background(), x.text, model)
		if err != nil {
			t.Errorf("%s: %v", x.text, err)
			continue
		}
		t.Logf("%s\n  -> %s", x.text, res.CorrectedText)
		for _, g := range res.Gaps {
			t.Logf("  gap %s -> %s %q | %s | %s", g.Original, g.Corrected, g.Alternatives, g.NoteZh, g.Example)
		}
		for _, is := range res.Issues {
			t.Logf("  issue %s -> %s (%s, %s)", is.Original, is.Corrected, is.Category, is.Severity)
		}
		if len(res.Gaps) != x.gaps || x.issues >= 0 && len(res.Issues) != x.issues {
			t.Errorf("%s: %d gaps, %d issues; want %d, %d", x.text, len(res.Gaps), len(res.Issues), x.gaps, x.issues)
		}
	}
}
