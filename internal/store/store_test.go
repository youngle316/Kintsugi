package store

import (
	"context"
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
	first, err := s.Save(ctx, Submission{CreatedAt: now, SourceApp: "com.amp", SourceAppName: "Amp", OriginalText: "x",
		Result: llm.Result{WordCount: 40, Issues: []llm.Issue{
			issue("discuss about -> discuss", "preposition_collocation", "error"),
			issue("missing article", "article", "error"),
		}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Save(ctx, Submission{CreatedAt: now.Add(time.Second), SourceApp: "md.obsidian", OriginalText: "y",
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
	rows, err := s.Issues(ctx, Filter{SourceApp: "md.obsidian"})
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
