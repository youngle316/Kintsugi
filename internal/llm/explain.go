package llm

import (
	"context"
	"errors"
	"strings"
)

// Explanation is the model's explanation, in plain English, of English the
// user reads: of a text, Plain and Terms; of a word or a short phrase
// (IsPhrase), Entry.
type Explanation struct {
	// Plain is the text again in plain English.
	Plain string `json:"plain"`
	Terms []Term `json:"terms"`
	// Entry is the dictionary entry of a word or phrase, nil for a text.
	Entry *Entry `json:"entry,omitempty"`
	Model string `json:"model"`
}

// Term is a word or phrase of the text a learner may not know.
type Term struct {
	Term    string `json:"term"`    // as the text has it, with its part of speech
	Meaning string `json:"meaning"` // what it means in this text
	Example string `json:"example"` // a new sentence that uses it
}

// Entry is a learner's dictionary entry of a word or phrase. Kintsugi
// reads only the selection, not the sentence around it, so the entry has
// the common meanings rather than the one meant.
type Entry struct {
	Headword     string    `json:"headword"`       // the dictionary form
	PartOfSpeech string    `json:"part_of_speech"` // "verb", "idiom", …
	Register     string    `json:"register"`       // "formal", "informal", …; "" when neutral
	Meanings     []Meaning `json:"meanings"`       // the most common first
	Synonyms     []string  `json:"synonyms"`
	Opposites    []string  `json:"opposites"`
	Collocations []string  `json:"collocations"`
	WordFamily   []string  `json:"word_family"` // other forms, with their part of speech
}

// Meaning is a meaning of an entry.
type Meaning struct {
	Meaning string `json:"meaning"`
	Example string `json:"example"` // a sentence that uses it
}

const explainPrompt = `You help a native Chinese speaker read English. They select English they
read, such as documentation, an article or a message, and you explain it in
plain English. Never use Chinese.

Write for a B1–B2 learner: common words, short sentences. Do not explain a
hard word with another hard word.

Return JSON only:
{
  "plain": "...",
  "terms": [
    {"term": "...", "meaning": "...", "example": "..."}
  ]
}

plain: the same meaning in plain English. Keep every point and the order of
the text; do not add opinions or summarize away details. Keep its
paragraphs.

terms: the words, idioms, phrasal verbs and collocations of the text a B1–B2
learner may not know, at most 8, in the order they appear. Leave out easy
words, names and technical terms the reader already uses. Each one:
- term: as it appears in the text, then its part of speech in parentheses,
  e.g. "roll out (phrasal verb)", "mitigate (verb)".
- meaning: what it means in this text, in one short sentence.
- example: one new, everyday sentence that uses it the same way.

Tokens like ⟦C1⟧ stand for code that was removed. Copy them into plain where
they belong, and never list them as terms.

When the text has nothing hard, plain is a short plain version and terms
is [].`

const dictionaryPrompt = `You are a learner's dictionary for a native Chinese speaker who reads
English. They select a word or a short phrase, without the sentence it is
in, and you explain it in plain English. Never use Chinese.

Write for a B1–B2 learner: common words, short sentences. Do not explain a
hard word with another hard word.

Return JSON only:
{
  "headword": "...",
  "part_of_speech": "...",
  "register": "...",
  "meanings": [{"meaning": "...", "example": "..."}],
  "synonyms": ["..."],
  "opposites": ["..."],
  "collocations": ["..."],
  "word_family": ["..."]
}

- headword: the dictionary form: "running" is "run", "rolled out" is
  "roll out". Keep a fixed phrase or idiom whole.
- part_of_speech: such as noun, verb, adjective, adverb, phrasal verb,
  idiom or phrase. For a word that is often more than one, the most common.
- register: one of formal, informal, technical, literary, old-fashioned,
  offensive; "" when it is neutral.
- meanings: the 1–3 most common meanings, the most common first. meaning is
  one short sentence; example is a new, everyday sentence that uses it.
- synonyms: up to 3 simpler words or phrases that mean about the same.
- opposites: up to 3, or [].
- collocations: up to 3 short, common combinations with the headword, such
  as "mitigate the risk" or "roll out a feature".
- word_family: up to 3 other forms with their part of speech, such as
  "mitigation (noun)"; [] for idioms and phrases.
- When it is not English, or a misspelling, the one meaning says so and
  names the word that was probably meant.`

// IsPhrase reports whether text is a word or a short phrase, which gets a
// dictionary entry, rather than a text to explain: up to 4 words without
// code, line breaks or the punctuation of a sentence inside it.
func IsPhrase(text string) bool {
	text = strings.Trim(strings.TrimSpace(text), `.,;:!?"'“”‘’()`)
	n := len(strings.Fields(text))
	return n > 0 && n <= 4 && !strings.ContainsAny(text, ".!?;:\n`/") && !strings.Contains(text, "://")
}

// Explain explains text with model in plain English: a word or a short
// phrase as a dictionary entry, anything longer again in plain words with
// its hard terms. Code in it is masked as for Check and put back.
func (c *Client) Explain(ctx context.Context, text, model string) (Explanation, error) {
	if c.APIKey == "" {
		return Explanation{}, ErrNoAPIKey
	}
	m := Mask(text)
	prompt, parse := explainPrompt, ParseExplanation
	if IsPhrase(text) {
		prompt, parse = dictionaryPrompt, ParseEntry
	}
	var ex Explanation
	err := c.ask(ctx, model, prompt, strings.TrimSpace(m.Text), func(content string) (err error) {
		ex, err = parse(content, m)
		return err
	})
	if err != nil {
		return Explanation{}, err
	}
	ex.Model = model
	return ex, nil
}

// ParseExplanation reads the model's answer to the explanation of m.
func ParseExplanation(content string, m Masked) (Explanation, error) {
	var raw Explanation
	if err := decodeAnswer(content, &raw); err != nil {
		return Explanation{}, err
	}
	// A plain version may leave out code it does not need, so missing
	// placeholders are not an error here.
	plain, _ := m.unmask(strings.TrimSpace(raw.Plain))
	if plain == "" {
		return Explanation{}, errors.New("the model's answer has no plain version")
	}
	ex := Explanation{Plain: plain, Terms: []Term{}}
	for _, t := range raw.Terms {
		t.Term, _ = m.unmask(strings.TrimSpace(t.Term))
		t.Meaning, _ = m.unmask(strings.TrimSpace(t.Meaning))
		t.Example, _ = m.unmask(strings.TrimSpace(t.Example))
		if t.Term == "" || t.Meaning == "" {
			continue
		}
		ex.Terms = append(ex.Terms, t)
	}
	return ex, nil
}

// ParseEntry reads the model's answer to the dictionary entry of m.
func ParseEntry(content string, m Masked) (Explanation, error) {
	var raw Entry
	if err := decodeAnswer(content, &raw); err != nil {
		return Explanation{}, err
	}
	e := Entry{
		Headword:     strings.TrimSpace(raw.Headword),
		PartOfSpeech: strings.ToLower(strings.TrimSpace(raw.PartOfSpeech)),
		Register:     strings.ToLower(strings.TrimSpace(raw.Register)),
		Meanings:     []Meaning{},
		Synonyms:     words(raw.Synonyms, 3),
		Opposites:    words(raw.Opposites, 3),
		Collocations: words(raw.Collocations, 3),
		WordFamily:   words(raw.WordFamily, 3),
	}
	if e.Headword == "" {
		e.Headword = strings.TrimSpace(m.Text)
	}
	if e.Register == "neutral" || e.Register == "none" {
		e.Register = ""
	}
	for _, mn := range raw.Meanings {
		mn.Meaning, mn.Example = strings.TrimSpace(mn.Meaning), strings.TrimSpace(mn.Example)
		if mn.Meaning != "" && len(e.Meanings) < 3 {
			e.Meanings = append(e.Meanings, mn)
		}
	}
	if len(e.Meanings) == 0 {
		return Explanation{}, errors.New("the model's answer has no meaning")
	}
	return Explanation{Terms: []Term{}, Entry: &e}, nil
}

// words tidies a list of the model: without blanks, the same word twice,
// or more than max.
func words(list []string, max int) []string {
	out := []string{}
	for _, w := range list {
		w = strings.TrimSpace(w)
		if w == "" || len(out) == max || containsFold(out, w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, w := range list {
		if strings.EqualFold(w, s) {
			return true
		}
	}
	return false
}
