// Package store keeps Kintsugi's checks and the mistakes they found in a
// local SQLite database, through MyGo's sqlite plugin.
package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/youngle316/kintsugi/internal/llm"
)

// Actions a user takes on a check.
const (
	ActionReplaced = "replaced"
	ActionCopied   = "copied"
	ActionIgnored  = "ignored"
)

// Store is the database.
type Store struct{ db *sqlite.DB }

var schema = []string{
	`CREATE TABLE IF NOT EXISTS submissions (
		id INTEGER PRIMARY KEY,
		created_at INTEGER NOT NULL,
		source_app TEXT NOT NULL DEFAULT '',
		source_app_name TEXT NOT NULL DEFAULT '',
		capture_method TEXT NOT NULL DEFAULT '',
		original_text TEXT NOT NULL,
		corrected_text TEXT NOT NULL,
		word_count INTEGER NOT NULL DEFAULT 0,
		action TEXT NOT NULL DEFAULT 'ignored',
		model TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS issues (
		id INTEGER PRIMARY KEY,
		submission_id INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
		span_original TEXT NOT NULL,
		span_corrected TEXT NOT NULL,
		category TEXT NOT NULL,
		pattern TEXT NOT NULL,
		severity TEXT NOT NULL CHECK (severity IN ('error', 'suggestion')),
		explanation_zh TEXT NOT NULL DEFAULT '',
		explanation_en TEXT NOT NULL DEFAULT '',
		sentence TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS submissions_created ON submissions(created_at)`,
	`CREATE INDEX IF NOT EXISTS issues_submission ON issues(submission_id)`,
	`CREATE INDEX IF NOT EXISTS issues_pattern ON issues(pattern)`,
}

// Open opens the database at path, creating its tables.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, err
	}
	_, _ = db.Query(ctx, "PRAGMA journal_mode = WAL") // a speed-up only
	for _, s := range schema {
		if _, err := db.Execute(ctx, s); err != nil {
			db.Close()
			return nil, fmt.Errorf("creating the tables: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Submission is a check to save.
type Submission struct {
	CreatedAt     time.Time
	SourceApp     string // bundle ID
	SourceAppName string
	CaptureMethod string
	OriginalText  string
	Action        string
	Result        llm.Result
}

// Save stores a check and its issues, and returns its ID.
func (s *Store) Save(ctx context.Context, sub Submission) (int64, error) {
	if sub.Action == "" {
		sub.Action = ActionIgnored
	}
	stmts := []sqlite.Statement{{
		SQL: `INSERT INTO submissions (created_at, source_app, source_app_name, capture_method, original_text,
			corrected_text, word_count, action, model) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{sub.CreatedAt.UnixMilli(), sub.SourceApp, sub.SourceAppName, sub.CaptureMethod, sub.OriginalText,
			sub.Result.CorrectedText, sub.Result.WordCount, sub.Action, sub.Result.Model},
	}}
	for _, is := range sub.Result.Issues {
		// The transaction holds the write lock (BEGIN IMMEDIATE), so the
		// newest submission is the one inserted above.
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO issues (submission_id, span_original, span_corrected, category, pattern, severity,
				explanation_zh, explanation_en, sentence) VALUES ((SELECT MAX(id) FROM submissions), ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{is.Original, is.Corrected, is.Category, is.Pattern, is.Severity, is.ExplanationZh, is.ExplanationEn, is.Sentence},
		})
	}
	res, err := s.db.Transaction(ctx, stmts)
	if err != nil {
		return 0, err
	}
	return res[0].LastInsertID, nil
}

// SetAction records what the user did with a check.
func (s *Store) SetAction(ctx context.Context, id int64, action string) error {
	_, err := s.db.Execute(ctx, `UPDATE submissions SET action = ? WHERE id = ?`, action, id)
	return err
}

// IssueRow is an issue with its submission, as the mistakes list shows it.
type IssueRow struct {
	ID            int64  `json:"id"`
	SubmissionID  int64  `json:"submissionId"`
	CreatedAt     int64  `json:"createdAt"` // Unix milliseconds
	SourceApp     string `json:"sourceApp"`
	SourceAppName string `json:"sourceAppName"`
	Original      string `json:"original"`
	Corrected     string `json:"corrected"`
	Category      string `json:"category"`
	Pattern       string `json:"pattern"`
	Severity      string `json:"severity"`
	ExplanationZh string `json:"explanationZh"`
	ExplanationEn string `json:"explanationEn"`
	Sentence      string `json:"sentence"`
}

// Filter selects issues; empty fields select all.
type Filter struct {
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	SourceApp string `json:"sourceApp"`
	Since     int64  `json:"since"` // Unix milliseconds
	Until     int64  `json:"until"`
	Pattern   string `json:"pattern"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

// Issues lists the issues f selects, newest first.
func (s *Store) Issues(ctx context.Context, f Filter) ([]IssueRow, error) {
	where, args := f.where()
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := s.db.Query(ctx, `SELECT i.id, i.submission_id, s.created_at, s.source_app, s.source_app_name,
		i.span_original, i.span_corrected, i.category, i.pattern, i.severity, i.explanation_zh, i.explanation_en, i.sentence
		FROM issues i JOIN submissions s ON s.id = i.submission_id`+where+
		` ORDER BY s.created_at DESC, i.id LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	out := make([]IssueRow, 0, len(rows.Values))
	for _, v := range rows.Values {
		out = append(out, IssueRow{
			ID: i64(v[0]), SubmissionID: i64(v[1]), CreatedAt: i64(v[2]), SourceApp: str(v[3]), SourceAppName: str(v[4]),
			Original: str(v[5]), Corrected: str(v[6]), Category: str(v[7]), Pattern: str(v[8]), Severity: str(v[9]),
			ExplanationZh: str(v[10]), ExplanationEn: str(v[11]), Sentence: str(v[12]),
		})
	}
	return out, nil
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) { conds = append(conds, cond); args = append(args, v) }
	if f.Category != "" {
		add("i.category = ?", f.Category)
	}
	if f.Severity != "" {
		add("i.severity = ?", f.Severity)
	}
	if f.SourceApp != "" {
		add("s.source_app = ?", f.SourceApp)
	}
	if f.Pattern != "" {
		add("i.pattern = ?", f.Pattern)
	}
	if f.Since > 0 {
		add("s.created_at >= ?", f.Since)
	}
	if f.Until > 0 {
		add("s.created_at < ?", f.Until)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Count is a number of issues of one kind.
type Count struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count"`
}

// Overview sums up the checks since a time.
type Overview struct {
	Submissions    int64   `json:"submissions"`
	CleanChecks    int64   `json:"cleanChecks"`
	Words          int64   `json:"words"`
	Errors         int64   `json:"errors"`
	Suggestions    int64   `json:"suggestions"`
	ErrorsPer100   float64 `json:"errorsPer100"` // errors per hundred words
	Categories     []Count `json:"categories"`
	TopPatterns    []Count `json:"topPatterns"`
	SourceApps     []Count `json:"sourceApps"`
	SinceMillis    int64   `json:"since"`
	PatternsWindow int     `json:"patternsWindowDays"`
}

// Overview counts the checks and issues since since (all when zero), and
// the top patterns of the last 30 days.
func (s *Store) Overview(ctx context.Context, since time.Time) (Overview, error) {
	var o Overview
	o.PatternsWindow = 30
	from := int64(0)
	if !since.IsZero() {
		from = since.UnixMilli()
		o.SinceMillis = from
	}
	rows, err := s.db.Query(ctx, `SELECT COUNT(*), COALESCE(SUM(word_count), 0),
		COALESCE(SUM(NOT EXISTS (SELECT 1 FROM issues i WHERE i.submission_id = s.id)), 0)
		FROM submissions s WHERE created_at >= ?`, from)
	if err != nil {
		return o, err
	}
	if len(rows.Values) == 1 {
		o.Submissions, o.Words, o.CleanChecks = i64(rows.Values[0][0]), i64(rows.Values[0][1]), i64(rows.Values[0][2])
	}
	rows, err = s.db.Query(ctx, `SELECT i.severity, COUNT(*) FROM issues i JOIN submissions s ON s.id = i.submission_id
		WHERE s.created_at >= ? GROUP BY i.severity`, from)
	if err != nil {
		return o, err
	}
	for _, v := range rows.Values {
		if str(v[0]) == "error" {
			o.Errors = i64(v[1])
		} else {
			o.Suggestions = i64(v[1])
		}
	}
	if o.Words > 0 {
		o.ErrorsPer100 = float64(o.Errors) / float64(o.Words) * 100
	}
	if o.Categories, err = s.counts(ctx, `SELECT i.category, '', COUNT(*) FROM issues i JOIN submissions s ON s.id = i.submission_id
		WHERE s.created_at >= ? GROUP BY i.category ORDER BY 3 DESC`, from); err != nil {
		return o, err
	}
	monthAgo := time.Now().AddDate(0, 0, -30).UnixMilli()
	if o.TopPatterns, err = s.counts(ctx, `SELECT i.pattern, MIN(i.category), COUNT(*) FROM issues i JOIN submissions s ON s.id = i.submission_id
		WHERE s.created_at >= ? GROUP BY i.pattern ORDER BY 3 DESC, MAX(s.created_at) DESC LIMIT 10`, monthAgo); err != nil {
		return o, err
	}
	if o.SourceApps, err = s.counts(ctx, `SELECT source_app, MAX(source_app_name), COUNT(*) FROM submissions
		WHERE created_at >= ? GROUP BY source_app ORDER BY 3 DESC`, from); err != nil {
		return o, err
	}
	return o, nil
}

func (s *Store) counts(ctx context.Context, sql string, args ...any) ([]Count, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(rows.Values))
	for _, v := range rows.Values {
		out = append(out, Count{Key: str(v[0]), Label: str(v[1]), Count: i64(v[2])})
	}
	return out, nil
}

func i64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
