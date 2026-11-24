// Package llm checks English text with a chat model of SiliconFlow's
// OpenAI-compatible API, and turns its answer into issues Kintsugi stores.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is SiliconFlow's API in mainland China; the global one is
// https://api.siliconflow.com/v1. A key works only with its own platform.
const DefaultBaseURL = "https://api.siliconflow.cn/v1"

// Categories are the kinds of mistakes, in the order of the requirements.
var Categories = []string{
	"article", "tense", "subject_verb_agreement", "number_countability",
	"preposition_collocation", "part_of_speech", "word_choice", "chinglish",
	"sentence_structure", "punctuation", "spelling", "capitalization",
	"wordiness", "tone",
}

// Issue is one mistake the model found.
type Issue struct {
	Original      string `json:"original"`
	Corrected     string `json:"corrected"`
	Category      string `json:"category"`
	Pattern       string `json:"pattern"`
	Severity      string `json:"severity"` // "error" or "suggestion"
	ExplanationZh string `json:"explanationZh"`
	ExplanationEn string `json:"explanationEn"`
	Sentence      string `json:"sentence"`
}

// Gap is Chinese the user wrote where they did not know the English, and
// the English that fills it. It is not a mistake.
type Gap struct {
	Original  string `json:"original"` // the Chinese
	Corrected string `json:"corrected"`
	// Alternatives are other ways to say it; the notes tell them apart.
	Alternatives []string `json:"alternatives"`
	NoteZh       string   `json:"noteZh"`
	NoteEn       string   `json:"noteEn"`
	Example      string   `json:"example"`
	Sentence     string   `json:"sentence"`
}

// Result is the model's correction of a text.
type Result struct {
	CorrectedText string  `json:"correctedText"`
	Issues        []Issue `json:"issues"`
	Gaps          []Gap   `json:"gaps"`
	Model         string  `json:"model"`
	// WordCount counts the words of the text, leaving out code.
	WordCount int `json:"wordCount"`
}

// Changed reports whether there is anything to review: a mistake or a gap.
func (r *Result) Changed() bool { return len(r.Issues) > 0 || len(r.Gaps) > 0 }

// Client calls a chat model.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// ErrNoAPIKey means the client has no API key.
var ErrNoAPIKey = errors.New("no SiliconFlow API key: add one in Settings")

// ErrMostlyChinese means the text is Chinese to translate, rather than
// English with words missing.
var ErrMostlyChinese = errors.New("this is mostly Chinese, and Kintsugi does not translate: write it in English first, with Chinese only for the words you are stuck on")

var errUnfilled = errors.New("the model left Chinese in the text")

// APIError is an error the API answered with.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("SiliconFlow answered %d: %s", e.Status, strings.TrimSpace(e.Body))
}

const systemPrompt = `You are an English writing tutor for a native Chinese speaker who writes
technical messages to AI coding tools.

Task: find mistakes in the user's text, fill in the English for words they
left in Chinese, and return JSON only.

Rules:
- Keep the user's meaning and tone. Do not rewrite for style unless it is
  clearly unnatural.
- "error" = grammatically wrong. "suggestion" = correct but unnatural.
- Ignore code, file paths, identifiers, URLs, and text inside backticks.
- Tokens like ⟦C1⟧ stand for code that was removed. Copy every one of them
  unchanged, in place, into corrected_text, and never report them as issues.
- category must be one of: [%s]
- pattern: a short, reusable, lower-case description of the mistake, e.g.
  "discuss about -> discuss", "missing article before singular noun".
- explanation_zh: one sentence in Simplified Chinese; explanation_en: the
  same in English.
- original and corrected are the smallest spans that show the fix, copied
  exactly from the text and from corrected_text.
- sentence is the whole sentence of the user's text the issue is in.
- When there is nothing to fix, corrected_text is the text unchanged and
  issues and gaps are [].

Gaps: where the user does not know the English, they write Chinese in the
middle of their English. Put the English that fits the sentence in its place
in corrected_text, and change nothing else because of it; the rest of the
text gets the same check as ever. Each gap:
- original: the Chinese, copied exactly; corrected: the English in its
  place, copied exactly from corrected_text, as short as a writer of this
  sentence would say it.
- alternatives: up to 3 other English words or phrases that fit this
  sentence, or [].
- note_zh: one sentence in Simplified Chinese on how corrected differs from
  the alternatives, or when to use it; note_en: the same in English.
- example: one new, everyday English sentence that uses corrected.
- sentence: the whole sentence of the user's text the gap is in.
A gap is not a mistake: never list it in issues. Grammar the user wrote
wrong around it still is an issue.
Keep Chinese inside quotation marks or brackets such as 「」 as it is, like
the name of a button: it is quoted, and not a gap. Write a Chinese person's
name in pinyin. When the whole text is a Chinese word or phrase, it is one
gap, and corrected_text is only its English.

Output:
{
  "corrected_text": "...",
  "issues": [
    {
      "original": "...",
      "corrected": "...",
      "category": "...",
      "pattern": "...",
      "severity": "error|suggestion",
      "explanation_zh": "...",
      "explanation_en": "...",
      "sentence": "..."
    }
  ],
  "gaps": [
    {
      "original": "...",
      "corrected": "...",
      "alternatives": ["..."],
      "note_zh": "...",
      "note_en": "...",
      "example": "...",
      "sentence": "..."
    }
  ]
}`

// Prompt is the system prompt of a check.
func Prompt() string { return fmt.Sprintf(systemPrompt, strings.Join(Categories, ", ")) }

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model          string            `json:"model"`
	Messages       []message         `json:"messages"`
	Temperature    float64           `json:"temperature"`
	MaxTokens      int               `json:"max_tokens"`
	Stream         bool              `json:"stream"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	EnableThinking *bool             `json:"enable_thinking,omitempty"`
}

type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Check corrects text with model. Code in it (fenced blocks, backticks,
// URLs and paths) is replaced by placeholders the model must keep, and put
// back in the result, so it is neither sent as prose nor changed. An answer
// that is not valid JSON, or loses a placeholder, is asked for once more;
// so is one that leaves Chinese unfilled, which is taken if it is all there
// is. A text that is mostly Chinese is ErrMostlyChinese.
func (c *Client) Check(ctx context.Context, text, model string) (Result, error) {
	if c.APIKey == "" {
		return Result{}, ErrNoAPIKey
	}
	m := Mask(text)
	if mostlyChinese(m.Text) {
		return Result{}, ErrMostlyChinese
	}
	toFill := hasHan(unquoted(m.Text))
	var res Result
	answered := false
	err := c.ask(ctx, model, Prompt(), m.Text, func(content string) error {
		r, err := Parse(content, m)
		if err != nil {
			return err
		}
		res, answered = r, true
		if toFill && (len(r.Gaps) == 0 || hasHan(unquoted(Mask(r.CorrectedText).Text))) {
			return errUnfilled
		}
		return nil
	})
	if err != nil && !answered {
		return Result{}, err
	}
	res.Model = model
	res.WordCount = CountWords(m.Text)
	return res, nil
}

// ask sends text to model with the system prompt, and gives the answer to
// parse. An answer parse refuses is asked for once more, and a model that
// refuses JSON mode or enable_thinking is asked without them.
func (c *Client) ask(ctx context.Context, model, prompt, text string, parse func(string) error) error {
	var lastErr error
	plain := false // without JSON mode and enable_thinking, for models that refuse them
	for attempt := 0; attempt < 2; attempt++ {
		content, err := c.complete(ctx, model, prompt, text, plain)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest && !plain {
			// The model may not take response_format or enable_thinking:
			// ask again with the prompt alone holding the format.
			plain = true
			lastErr = err
			continue
		}
		if err != nil {
			return err
		}
		if err = parse(content); err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func (c *Client) complete(ctx context.Context, model, prompt, text string, plain bool) (string, error) {
	req := request{
		Model:       model,
		Messages:    []message{{Role: "system", Content: prompt}, {Role: "user", Content: text}},
		Temperature: 0.2,
		MaxTokens:   4096,
	}
	if !plain {
		off := false
		req.ResponseFormat = map[string]string{"type": "json_object"}
		req.EnableThinking = &off
	}
	body, _ := json.Marshal(req)
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := h.Do(hr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Body: string(data)}
	}
	var r response
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("unexpected answer from SiliconFlow: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", errors.New("SiliconFlow answered with no choices")
	}
	if r.Choices[0].FinishReason == "length" {
		return "", errors.New("the model's answer was cut off: try a shorter text")
	}
	return r.Choices[0].Message.Content, nil
}

// Ping checks the key and the model with a short request.
func (c *Client) Ping(ctx context.Context, model string) error {
	_, err := c.Check(ctx, "This are a test.", model)
	return err
}
