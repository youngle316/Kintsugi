package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Masked is a text whose code was replaced by placeholders.
type Masked struct {
	Text  string
	Codes []string // Codes[i] is what ⟦C<i+1>⟧ stands for
}

// codePatterns find what is not prose; the code is the pattern's last
// group, after what must precede it.
var codePatterns = []*regexp.Regexp{
	regexp.MustCompile("(?s)(```.*?```)"),          // fenced blocks
	regexp.MustCompile("(`[^`\n]+`)"),              // inline code
	regexp.MustCompile(`(https?://[^\s<>()"'⟦]+)`), // URLs
	// Paths: rooted (/, ./, ../, ~/) at the start of a word, as "and/or"
	// is not one, or relative with an extension, as src/main.go.
	regexp.MustCompile(`(^|[\s(\[])((?:~|\.{1,2})?/[\w.\-]+(?:/[\w.\-]+)*/?)`),
	regexp.MustCompile(`(^|[\s(\[])([\w.\-]+(?:/[\w.\-]+)+\.[A-Za-z0-9]{1,8})\b`),
}

func placeholder(i int) string { return fmt.Sprintf("⟦C%d⟧", i+1) }

// Mask replaces code, URLs and paths in text with placeholders.
func Mask(text string) Masked {
	m := Masked{Text: text}
	for _, re := range codePatterns {
		var b strings.Builder
		last := 0
		for _, loc := range re.FindAllStringSubmatchIndex(m.Text, -1) {
			start, end := loc[len(loc)-2], loc[len(loc)-1]
			code := m.Text[start:end]
			if strings.Contains(code, "⟦C") {
				continue
			}
			b.WriteString(m.Text[last:start])
			m.Codes = append(m.Codes, code)
			b.WriteString(placeholder(len(m.Codes) - 1))
			last = end
		}
		b.WriteString(m.Text[last:])
		m.Text = b.String()
	}
	return m
}

// unmask puts the code back, and reports the placeholders s lacks.
func (m Masked) unmask(s string) (string, []string) {
	var missing []string
	for i := len(m.Codes) - 1; i >= 0; i-- { // ⟦C12⟧ before ⟦C1⟧
		p := placeholder(i)
		if !strings.Contains(s, p) {
			missing = append(missing, p)
			continue
		}
		s = strings.ReplaceAll(s, p, m.Codes[i])
	}
	return s, missing
}

type rawAnswer struct {
	CorrectedText string `json:"corrected_text"`
	Issues        []struct {
		Original      string `json:"original"`
		Corrected     string `json:"corrected"`
		Category      string `json:"category"`
		Pattern       string `json:"pattern"`
		Severity      string `json:"severity"`
		Explanation   string `json:"explanation"`
		ExplanationZh string `json:"explanation_zh"`
		ExplanationEn string `json:"explanation_en"`
		Sentence      string `json:"sentence"`
	} `json:"issues"`
	Gaps []struct {
		Original     string   `json:"original"`
		Corrected    string   `json:"corrected"`
		Alternatives []string `json:"alternatives"`
		NoteZh       string   `json:"note_zh"`
		NoteEn       string   `json:"note_en"`
		Example      string   `json:"example"`
		Sentence     string   `json:"sentence"`
	} `json:"gaps"`
}

// Parse reads the model's answer to the check of m.
func Parse(content string, m Masked) (Result, error) {
	var raw rawAnswer
	if err := decodeAnswer(content, &raw); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(raw.CorrectedText) == "" {
		return Result{}, errors.New("the model's answer has no corrected_text")
	}
	corrected, missing := m.unmask(raw.CorrectedText)
	if len(missing) > 0 {
		return Result{}, fmt.Errorf("the model dropped code from the text (%s)", strings.Join(missing, ", "))
	}
	original, _ := m.unmask(m.Text)
	res := Result{CorrectedText: keepEdges(original, corrected), Issues: []Issue{}, Gaps: []Gap{}}
	for _, rg := range raw.Gaps {
		g := Gap{
			Original:     strings.TrimSpace(rg.Original),
			Corrected:    strings.TrimSpace(rg.Corrected),
			Alternatives: []string{},
			NoteZh:       strings.TrimSpace(rg.NoteZh),
			NoteEn:       strings.TrimSpace(rg.NoteEn),
			Example:      strings.TrimSpace(rg.Example),
		}
		g.Sentence, _ = m.unmask(strings.TrimSpace(rg.Sentence))
		if !hasHan(g.Original) || g.Corrected == "" || hasHan(g.Corrected) {
			continue
		}
		for _, a := range words(rg.Alternatives, len(rg.Alternatives)) {
			if !strings.EqualFold(a, g.Corrected) && len(g.Alternatives) < 3 {
				g.Alternatives = append(g.Alternatives, a)
			}
		}
		res.Gaps = append(res.Gaps, g)
	}
	for _, ri := range raw.Issues {
		is := Issue{
			Category:      NormalizeCategory(ri.Category),
			Pattern:       NormalizePattern(ri.Pattern),
			Severity:      "suggestion",
			ExplanationZh: strings.TrimSpace(ri.ExplanationZh),
			ExplanationEn: strings.TrimSpace(ri.ExplanationEn),
		}
		is.Original, _ = m.unmask(strings.TrimSpace(ri.Original))
		is.Corrected, _ = m.unmask(strings.TrimSpace(ri.Corrected))
		is.Sentence, _ = m.unmask(strings.TrimSpace(ri.Sentence))
		if strings.EqualFold(strings.TrimSpace(ri.Severity), "error") {
			is.Severity = "error"
		}
		if ri.Explanation != "" { // an answer in the draft's single-language format
			if is.ExplanationZh == "" && hasHan(ri.Explanation) {
				is.ExplanationZh = ri.Explanation
			} else if is.ExplanationEn == "" {
				is.ExplanationEn = ri.Explanation
			}
		}
		if is.Original == is.Corrected {
			continue // nothing changed: not an issue
		}
		if hasHan(is.Original) && is.Corrected != "" && !hasHan(is.Corrected) {
			// A gap the model took for a mistake, maybe beside the gap itself.
			if !hasGap(res.Gaps, is.Original) {
				res.Gaps = append(res.Gaps, Gap{Original: is.Original, Corrected: is.Corrected, Alternatives: []string{},
					NoteZh: is.ExplanationZh, NoteEn: is.ExplanationEn, Sentence: is.Sentence})
			}
			continue
		}
		if is.Pattern == "" {
			is.Pattern = NormalizePattern(is.Original + " -> " + is.Corrected)
		}
		res.Issues = append(res.Issues, is)
	}
	if !res.Changed() && strings.TrimSpace(corrected) != strings.TrimSpace(original) {
		// Changes without issues would be fixes nobody learns from, and a
		// replacement the user can't review: keep the text.
		res.CorrectedText = original
	}
	return res, nil
}

// decodeAnswer reads the JSON object of the model's answer into v, without
// the code fence or words models put around it.
func decodeAnswer(content string, v any) error {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return errors.New("the model did not answer with JSON")
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), v); err != nil {
		return fmt.Errorf("the model's JSON is invalid: %w", err)
	}
	return nil
}

// keepEdges gives corrected the leading and trailing white space of
// original, which models trim, so that replacing a selection keeps its
// line breaks.
func keepEdges(original, corrected string) string {
	lead := original[:len(original)-len(strings.TrimLeftFunc(original, unicode.IsSpace))]
	trail := original[len(strings.TrimRightFunc(original, unicode.IsSpace)):]
	return lead + strings.TrimSpace(corrected) + trail
}

// NormalizeCategory maps a category the model wrote to one of Categories,
// and anything else to word_choice.
func NormalizeCategory(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	c = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(c)
	if slices.Contains(Categories, c) {
		return c
	}
	switch c {
	case "articles":
		return "article"
	case "preposition", "collocation", "prepositions":
		return "preposition_collocation"
	case "agreement":
		return "subject_verb_agreement"
	case "plural", "countability", "number":
		return "number_countability"
	case "grammar", "word_order", "run_on", "fragment":
		return "sentence_structure"
	case "formality", "register", "style":
		return "tone"
	case "redundancy", "conciseness":
		return "wordiness"
	case "typo":
		return "spelling"
	}
	return "word_choice"
}

var spaces = regexp.MustCompile(`\s+`)

// NormalizePattern makes patterns that differ only in case, spacing,
// quotes or the arrow they use the same, so that they count together.
func NormalizePattern(p string) string {
	p = strings.ToLower(p)
	p = strings.NewReplacer("→", "->", "=>", "->", "⇒", "->", "“", "", "”", "", "\"", "", "‘", "'", "’", "'").Replace(p)
	p = spaces.ReplaceAllString(p, " ")
	p = strings.ReplaceAll(p, " ->", "->")
	p = strings.ReplaceAll(p, "-> ", "->")
	p = strings.ReplaceAll(p, "->", " -> ")
	return strings.Trim(strings.TrimSpace(p), ".")
}

// CountWords counts the English words of a masked text, leaving out
// placeholders and Chinese.
func CountWords(masked string) int {
	n := 0
	for _, f := range strings.Fields(masked) {
		if strings.HasPrefix(f, "⟦C") {
			continue
		}
		if strings.IndexFunc(f, func(r rune) bool { return (unicode.IsLetter(r) || unicode.IsDigit(r)) && !unicode.Is(unicode.Han, r) }) >= 0 {
			n++
		}
	}
	return n
}

func hasHan(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0
}

// hasGap reports whether a gap of gaps is in span.
func hasGap(gaps []Gap, span string) bool {
	return slices.ContainsFunc(gaps, func(g Gap) bool { return strings.Contains(span, g.Original) })
}

// quoted finds text in quotation marks; not in straight single ones, which
// are apostrophes too.
var quoted = regexp.MustCompile(`"[^"\n]*"|“[^”\n]*”|‘[^’\n]*’|「[^」\n]*」|『[^』\n]*』`)

// unquoted is s without the text in quotation marks, whose Chinese stays.
func unquoted(s string) string { return quoted.ReplaceAllString(s, " ") }

var (
	han          = regexp.MustCompile(`\p{Han}`)
	latinWords   = regexp.MustCompile(`[A-Za-z][A-Za-z'’-]*`)
	placeholders = regexp.MustCompile(`⟦C\d+⟧`)
)

// mostlyChinese reports whether a masked text is Chinese to translate: more
// than a phrase of it, with few English words among it.
func mostlyChinese(masked string) bool {
	s := placeholders.ReplaceAllString(unquoted(masked), " ")
	n := len(han.FindAllString(s, -1))
	return n > 12 && n > 4*len(latinWords.FindAllString(s, -1))
}
