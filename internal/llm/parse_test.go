package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaskKeepsProseAndRestoresCode(t *testing.T) {
	text := "Please discuss about `fooBar()` in src/app/main.go and/or ~/notes, see https://x.dev/a?b=1.\n```go\nx := 1\n```"
	m := Mask(text)
	for _, code := range []string{"`fooBar()`", "src/app/main.go", "~/notes,", "https://x.dev/a?b=1.", "x := 1"} {
		if strings.Contains(m.Text, strings.TrimSuffix(code, ",")) {
			t.Errorf("masked text still has %q: %q", code, m.Text)
		}
	}
	if !strings.Contains(m.Text, "and/or") || !strings.Contains(m.Text, "discuss about") {
		t.Errorf("masked prose: %q", m.Text)
	}
	back, missing := m.unmask(m.Text)
	if back != text || len(missing) > 0 {
		t.Errorf("unmask = %q, missing %v", back, missing)
	}
}

func TestParse(t *testing.T) {
	m := Mask("  I want discuss about the plan with `npm run dev`.\n")
	answer := "```json\n" + `{
	  "corrected_text": "I want to discuss the plan with ⟦C1⟧.",
	  "issues": [
	    {"original": "discuss about the plan", "corrected": "discuss the plan", "category": "Preposition Collocation",
	     "pattern": "Discuss  About → Discuss", "severity": "Error", "explanation_zh": "discuss 是及物动词。",
	     "explanation_en": "Discuss takes a direct object.", "sentence": "I want discuss about the plan with ⟦C1⟧."},
	    {"original": "want discuss", "corrected": "want to discuss", "category": "grammar", "pattern": "",
	     "severity": "error", "explanation": "want 后接 to do。", "sentence": ""},
	    {"original": "the plan", "corrected": "the plan", "category": "article", "severity": "suggestion"}
	  ]
	}` + "\n```"
	res, err := Parse(answer, m)
	if err != nil {
		t.Fatal(err)
	}
	if want := "  I want to discuss the plan with `npm run dev`.\n"; res.CorrectedText != want {
		t.Errorf("corrected = %q, want %q", res.CorrectedText, want)
	}
	if len(res.Issues) != 2 {
		t.Fatalf("issues = %+v, want 2 (the unchanged one dropped)", res.Issues)
	}
	a, b := res.Issues[0], res.Issues[1]
	if a.Category != "preposition_collocation" || a.Pattern != "discuss about -> discuss" || a.Severity != "error" {
		t.Errorf("first issue = %+v", a)
	}
	if a.Sentence != "I want discuss about the plan with `npm run dev`." {
		t.Errorf("sentence = %q", a.Sentence)
	}
	if b.Category != "sentence_structure" || b.Pattern != "want discuss -> want to discuss" || b.ExplanationZh != "want 后接 to do。" {
		t.Errorf("second issue = %+v", b)
	}
}

func TestParseRejectsDroppedCode(t *testing.T) {
	m := Mask("Run `make` now.")
	if _, err := Parse(`{"corrected_text": "Run it now.", "issues": []}`, m); err == nil {
		t.Fatal("an answer without the code's placeholder was accepted")
	}
}

func TestParseKeepsTextWhenChangesHaveNoIssues(t *testing.T) {
	m := Mask("This is fine.")
	res, err := Parse(`{"corrected_text": "This is totally fine.", "issues": []}`, m)
	if err != nil || res.CorrectedText != "This is fine." {
		t.Fatalf("Parse = %+v, %v", res, err)
	}
}

func TestCountWords(t *testing.T) {
	if n := CountWords(Mask("Run `npm i` then open ./a/b.ts - it's 2 steps.").Text); n != 6 {
		t.Errorf("CountWords = %d, want 6 (Run, then, open, it's, 2, steps)", n)
	}
}

// TestCheckRetries checks that a client asks again without JSON mode when
// the model refuses it, and again when the answer is not JSON.
func TestCheckRetries(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var b map[string]any
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &b)
		bodies = append(bodies, b)
		if _, ok := b["response_format"]; ok {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"message":"response_format is not supported"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"content": `{"corrected_text": "This is a test.", "issues": [{"original": "are", "corrected": "is", "category": "subject_verb_agreement", "pattern": "this are -> this is", "severity": "error"}]}`},
		}}})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k"}
	res, err := c.Check(context.Background(), "This are a test.", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[1]["response_format"] != nil || bodies[1]["enable_thinking"] != nil {
		t.Errorf("requests = %v", bodies)
	}
	if res.Model != "m" || res.WordCount != 4 || len(res.Issues) != 1 {
		t.Errorf("result = %+v", res)
	}
}

func TestParseExplanation(t *testing.T) {
	m := Mask("Run `make` to roll out the fix.")
	answer := "Sure:\n```json\n" + `{
	  "plain": "Use ⟦C1⟧ to put the fix live for everyone.",
	  "terms": [
	    {"term": " roll out (phrasal verb) ", "meaning": "To make something available to users.", "example": "They rolled out the update on Monday."},
	    {"term": "fix", "meaning": ""}
	  ]
	}` + "\n```"
	ex, err := ParseExplanation(answer, m)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Use `make` to put the fix live for everyone."; ex.Plain != want {
		t.Errorf("plain = %q, want %q", ex.Plain, want)
	}
	if len(ex.Terms) != 1 || ex.Terms[0].Term != "roll out (phrasal verb)" {
		t.Errorf("terms = %+v, want only roll out (the one without a meaning dropped)", ex.Terms)
	}
	if _, err := ParseExplanation(`{"plain": " ", "terms": []}`, m); err == nil {
		t.Error("an answer without a plain version was accepted")
	}
}

func TestIsPhrase(t *testing.T) {
	for text, want := range map[string]bool{
		"mitigate":            true,
		"  mitigate. ":        true, // a word selected with its period
		"bite the bullet":     true,
		"“roll out”":          true,
		"once in a while":     true, // 4 words
		"once in a blue moon": false,
		"It works. Ship it":   false, // two sentences
		"roll\nout":           false,
		"run `make`":          false,
		"see https://x.dev":   false,
		"":                    false,
	} {
		if got := IsPhrase(text); got != want {
			t.Errorf("IsPhrase(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestParseEntry(t *testing.T) {
	answer := `{
	  "headword": " roll out ", "part_of_speech": "Phrasal Verb", "register": "Neutral",
	  "meanings": [
	    {"meaning": "To make something new available.", "example": "They rolled out the app."},
	    {"meaning": " "},
	    {"meaning": "To push something flat.", "example": ""},
	    {"meaning": "To arrive."},
	    {"meaning": "A fourth one."}
	  ],
	  "synonyms": ["launch", "Launch", " ", "release", "ship", "debut"],
	  "collocations": ["roll out a feature"]
	}`
	ex, err := ParseEntry(answer, Mask("rolled out"))
	if err != nil {
		t.Fatal(err)
	}
	e := ex.Entry
	if e == nil || e.Headword != "roll out" || e.PartOfSpeech != "phrasal verb" || e.Register != "" {
		t.Fatalf("entry = %+v", e)
	}
	if len(e.Meanings) != 3 || e.Meanings[1].Meaning != "To push something flat." {
		t.Errorf("meanings = %+v, want the 3 first that are not blank", e.Meanings)
	}
	if strings.Join(e.Synonyms, ",") != "launch,release,ship" || len(e.Opposites) != 0 || e.Opposites == nil {
		t.Errorf("synonyms = %q, opposites = %#v", e.Synonyms, e.Opposites)
	}
	if _, err := ParseEntry(`{"headword": "xyz", "meanings": []}`, Mask("xyz")); err == nil {
		t.Error("an entry without a meaning was accepted")
	}
}
