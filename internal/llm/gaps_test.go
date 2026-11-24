package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const mixed = "Make it 可以拖动, don't 挡住 the text I am write."

const filled = `{"corrected_text": "Make it draggable, don't cover the text I am writing.",
 "gaps": [
  {"original": "可以拖动", "corrected": "draggable", "alternatives": ["movable", " ", "Draggable", "movable", "a", "b", "c"],
   "note_zh": "区别", "note_en": "difference", "example": "The panel is draggable.", "sentence": "s"},
  {"original": "挡住", "corrected": "cover"},
  {"original": "挡住", "corrected": ""},
  {"original": "am", "corrected": "is"}],
 "issues": [{"original": "am write", "corrected": "am writing", "category": "tense", "severity": "error"}]}`

func TestParseGaps(t *testing.T) {
	res, err := Parse(filled, Mask(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Gaps) != 2 || len(res.Issues) != 1 {
		t.Fatalf("gaps %+v, issues %+v", res.Gaps, res.Issues)
	}
	g := res.Gaps[0]
	if g.Original != "可以拖动" || g.Corrected != "draggable" || g.NoteZh != "区别" || g.NoteEn != "difference" || g.Example != "The panel is draggable." || g.Sentence != "s" {
		t.Errorf("gap = %+v", g)
	}
	if len(g.Alternatives) != 3 || g.Alternatives[0] != "movable" || g.Alternatives[1] != "a" {
		t.Errorf("alternatives = %q", g.Alternatives)
	}
}

func TestParseKeepsAFillWithoutIssues(t *testing.T) {
	res, err := Parse(`{"corrected_text": "Don't cover it.", "gaps": [{"original": "挡住", "corrected": "cover"}], "issues": []}`, Mask("Don't 挡住 it."))
	if err != nil || res.CorrectedText != "Don't cover it." {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestParseFillListedAsIssueIsAGap(t *testing.T) {
	issue := `{"original": "挡住", "corrected": "cover", "category": "word_choice", "severity": "error", "explanation_zh": "说明"}`
	for _, gaps := range []string{`[]`, `[{"original": "挡住", "corrected": "cover", "note_zh": "说明"}]`} {
		res, err := Parse(`{"corrected_text": "Don't cover it.", "gaps": `+gaps+`, "issues": [`+issue+`]}`, Mask("Don't 挡住 it."))
		if err != nil || len(res.Issues) != 0 || len(res.Gaps) != 1 || res.Gaps[0].NoteZh != "说明" {
			t.Errorf("%+v, %v", res, err)
		}
	}
}

func TestParseKeepsAnIssueNextToQuotedChinese(t *testing.T) {
	res, err := Parse(`{"corrected_text": "Click the “设置” button.", "issues": [{"original": "Click “设置”", "corrected": "Click the “设置”", "category": "article", "severity": "error"}]}`, Mask("Click “设置” button."))
	if err != nil || len(res.Issues) != 1 || len(res.Gaps) != 0 {
		t.Errorf("%+v, %v", res, err)
	}
}

func TestCountWordsLeavesOutChinese(t *testing.T) {
	if n := CountWords("Don't 挡住 the text, 可以拖动."); n != 3 {
		t.Errorf("CountWords = %d, want 3", n)
	}
}

// answering returns a client whose model gives answers in turn, the last
// one again, and the number of requests so far.
func answering(t *testing.T, answers ...string) (*Client, *int) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := answers[min(n, len(answers)-1)]
		n++
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": a}}}})
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: "k"}, &n
}

func TestCheckAsksAgainForChineseLeft(t *testing.T) {
	left := `{"corrected_text": "Make it draggable, don't 挡住 the text I am writing.", "gaps": [{"original": "可以拖动", "corrected": "draggable"}], "issues": []}`
	for _, c := range []struct {
		name    string
		answers []string
		gaps    int
	}{
		{"filled the second time", []string{left, filled}, 2},
		{"left twice: what there is", []string{left, left}, 1},
		{"a broken second answer: the first", []string{left, "sorry"}, 1},
	} {
		client, n := answering(t, c.answers...)
		res, err := client.Check(context.Background(), mixed, "m")
		if err != nil || *n != 2 || len(res.Gaps) != c.gaps || res.Model != "m" {
			t.Errorf("%s: %d requests, %+v, %v", c.name, *n, res, err)
		}
	}
}

func TestCheckLeavesQuotedChinese(t *testing.T) {
	for _, text := range []string{"Click “设置” now.", `Click "设置" now.`, "Click 「设置」 now.", "Click `设置` now."} {
		m := Mask(text)
		answer, _ := json.Marshal(map[string]any{"corrected_text": m.Text, "issues": []any{}})
		client, n := answering(t, string(answer))
		res, err := client.Check(context.Background(), text, "m")
		if err != nil || *n != 1 || res.CorrectedText != text {
			t.Errorf("%s: %d requests, %+v, %v", text, *n, res, err)
		}
	}
}

func TestCheckDoesNotTranslate(t *testing.T) {
	for text, refused := range map[string]bool{
		"可以拖动":        false,
		"把这个弹窗固定到桌面上": false, // 11 characters
		"这个弹窗支持拖动并且可以选择固定到桌面":                        true,
		"修改一个 bug，点击 quit 无法退出，还得用命令退出":              true,
		"Fix the bug where 点击退出按钮之后应用没有反应":           false,
		"The button “这个弹窗支持拖动并且可以选择固定到桌面” is wrong.": false,
		"组件 `这个弹窗支持拖动并且可以选择固定到桌面` 有问题":               false,
	} {
		client, n := answering(t, `{"corrected_text": "Ok.", "gaps": [{"original": "拖动", "corrected": "drag"}], "issues": []}`)
		_, err := client.Check(context.Background(), text, "m")
		if errors.Is(err, ErrMostlyChinese) != refused || (refused && *n != 0) {
			t.Errorf("%s: %v after %d requests, want refused %v", text, err, *n, refused)
		}
	}
}
