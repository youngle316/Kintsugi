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

// Result is the model's correction of a text.
type Result struct {
	CorrectedText string  `json:"correctedText"`
	Issues        []Issue `json:"issues"`
	Model         string  `json:"model"`
	// WordCount counts the words of the text, leaving out code.
	WordCount int `json:"wordCount"`
}

// Client calls a chat model.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// ErrNoAPIKey means the client has no API key.
var ErrNoAPIKey = errors.New("no SiliconFlow API key: add one in Settings")

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

Task: find mistakes in the user's text and return JSON only.

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
  issues is [].

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
// that is not valid JSON, or loses a placeholder, is asked for once more.
func (c *Client) Check(ctx context.Context, text, model string) (Result, error) {
	if c.APIKey == "" {
		return Result{}, ErrNoAPIKey
	}
	m := Mask(text)
	var res Result
	err := c.ask(ctx, model, Prompt(), m.Text, func(content string) (err error) {
		res, err = Parse(content, m)
		return err
	})
	if err != nil {
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
