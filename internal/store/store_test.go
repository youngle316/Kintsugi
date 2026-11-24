package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/youngle316/kintsugi/internal/llm"
)

func TestSaveAndCount(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	issue := func(pattern, cat, sev string) llm.Issue {
		return llm.Issue{Original: "a", Corrected: "b", Category: cat, Pattern: pattern, Severity: sev}
	}
	first, err := s.Save(ctx, Submission{CreatedAt: now, OriginalText: "x",
		Result: llm.Result{WordCount: 40, Issues: []llm.Issue{
			issue("discuss about -> discuss", "preposition_collocation", "error"),
			issue("missing article", "article", "error"),
		}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Save(ctx, Submission{CreatedAt: now.Add(time.Second), OriginalText: "y",
		Result: llm.Result{WordCount: 60, Issues: []llm.Issue{
			issue("discuss about -> discuss", "preposition_collocation", "error"),
			issue("in order to -> to", "wordiness", "suggestion"),
		}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, Submission{CreatedAt: now, OriginalText: "Looks good.", Result: llm.Result{WordCount: 2}}); err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("both submissions got ID %d", first)
	}
	if err := s.SetAction(ctx, first, ActionReplaced); err != nil {
		t.Fatal(err)
	}

	// Each issue belongs to its own submission.
	rows, err := s.Issues(ctx, Filter{Since: now.Add(time.Second).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].SubmissionID != second || rows[1].SubmissionID != second {
		t.Fatalf("issues of the second submission = %+v", rows)
	}
	if rows, _ := s.Issues(ctx, Filter{Severity: "error", Category: "article"}); len(rows) != 1 || rows[0].SubmissionID != first {
		t.Fatalf("article errors = %+v", rows)
	}

	o, err := s.Overview(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// 3 errors in 102 words; suggestions don't count.
	if o.Submissions != 3 || o.CleanChecks != 1 || o.Words != 102 || o.Errors != 3 || o.Suggestions != 1 {
		t.Errorf("overview = %+v", o)
	}
	if want := 300.0 / 102; o.ErrorsPer100 < want-1e-9 || o.ErrorsPer100 > want+1e-9 {
		t.Errorf("errors per 100 words = %v, want %v", o.ErrorsPer100, want)
	}
	if len(o.TopPatterns) != 3 || o.TopPatterns[0].Key != "discuss about -> discuss" || o.TopPatterns[0].Count != 2 {
		t.Errorf("top patterns = %+v", o.TopPatterns)
	}
	if len(o.Categories) != 3 || o.Categories[0].Key != "preposition_collocation" {
		t.Errorf("categories = %+v", o.Categories)
	}
}

func TestSaveGaps(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Save(ctx, Submission{CreatedAt: time.Now(), OriginalText: "x", Result: llm.Result{WordCount: 2}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.Save(ctx, Submission{CreatedAt: time.Now(), OriginalText: "Don't 挡住 it.", Result: llm.Result{WordCount: 2,
		Gaps: []llm.Gap{{Original: "挡住", Corrected: "cover", Alternatives: []string{"block"}, NoteZh: "区别", NoteEn: "d", Example: "e", Sentence: "s"}}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT submission_id, span_original, span_corrected, alternatives, note_zh, note_en, example, sentence FROM gaps`)
	if err != nil || len(rows.Values) != 1 {
		t.Fatalf("%v, %v", rows, err)
	}
	v := rows.Values[0]
	if i64(v[0]) != id {
		t.Errorf("submission %v, want %d", v[0], id)
	}
	for i, want := range []string{"挡住", "cover", `["block"]`, "区别", "d", "e", "s"} {
		if str(v[i+1]) != want {
			t.Errorf("column %d = %v, want %q", i+1, v[i+1], want)
		}
	}
	// A word filled in is not a mistake.
	o, err := s.Overview(ctx, time.Time{})
	if err != nil || o.CleanChecks != 2 || o.Errors+o.Suggestions != 0 {
		t.Errorf("overview = %+v, %v", o, err)
	}
}

func TestSaveLookup(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entry := llm.Explanation{Model: "m", Terms: []llm.Term{}, Entry: &llm.Entry{
		Headword: "roll out", PartOfSpeech: "phrasal verb",
		Meanings: []llm.Meaning{{Meaning: "To make something new available.", Example: "They rolled out the app."}},
	}}
	text := llm.Explanation{Model: "m", Plain: "Use it.", Terms: []llm.Term{{Term: "leverage (verb)", Meaning: "To use."}}}
	for _, l := range []Lookup{{Text: "rolled out", Explanation: entry}, {Text: "Leverage it.", Explanation: text}} {
		l.CreatedAt = time.Now()
		if err := s.SaveLookup(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	// A later vocabulary book finds the word by its headword, in any case,
	// and reads the whole entry back.
	rows, err := s.db.Query(ctx, `SELECT selected_text, explanation FROM lookups WHERE headword = ? COLLATE NOCASE`, "Roll Out")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Values) != 1 || str(rows.Values[0][0]) != "rolled out" {
		t.Fatalf("lookups of roll out = %v", rows.Values)
	}
	var back llm.Explanation
	if err := json.Unmarshal([]byte(str(rows.Values[0][1])), &back); err != nil {
		t.Fatal(err)
	}
	if back.Entry == nil || back.Entry.Meanings[0].Example != "They rolled out the app." {
		t.Errorf("entry read back = %+v", back.Entry)
	}
	if rows, _ := s.db.Query(ctx, `SELECT COUNT(*) FROM lookups WHERE headword = ''`); i64(rows.Values[0][0]) != 1 {
		t.Errorf("text lookups = %v, want 1", rows.Values)
	}
}
