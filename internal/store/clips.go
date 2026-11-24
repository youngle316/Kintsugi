package store

import (
	"context"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// MaxClips is how many clips the history keeps.
const MaxClips = 200

// Clip is a text the user copied.
type Clip struct {
	ID       int64
	Text     string
	App      string // the app in front when it was copied, "" when unknown
	CopiedAt time.Time
}

// SaveClip adds a clip to the history, or moves it to the top when its
// text is there, and drops the oldest over MaxClips.
func (s *Store) SaveClip(ctx context.Context, c Clip) error {
	_, err := s.db.Transaction(ctx, []sqlite.Statement{
		{
			SQL: `INSERT INTO clips (text, app, copied_at) VALUES (?, ?, ?)
				ON CONFLICT(text) DO UPDATE SET app = excluded.app, copied_at = excluded.copied_at`,
			Args: []any{c.Text, c.App, c.CopiedAt.UnixMilli()},
		},
		{
			SQL: `DELETE FROM clips WHERE id NOT IN
				(SELECT id FROM clips ORDER BY copied_at DESC, id DESC LIMIT ?)`,
			Args: []any{MaxClips},
		},
	})
	return err
}

// DeleteClip drops a clip.
func (s *Store) DeleteClip(ctx context.Context, id int64) error {
	_, err := s.db.Execute(ctx, `DELETE FROM clips WHERE id = ?`, id)
	return err
}

// ClearClips drops every clip.
func (s *Store) ClearClips(ctx context.Context) error {
	_, err := s.db.Execute(ctx, `DELETE FROM clips`)
	return err
}

// Clips lists the history, the newest first.
func (s *Store) Clips(ctx context.Context) ([]Clip, error) {
	rows, err := s.db.Query(ctx, `SELECT id, text, app, copied_at FROM clips ORDER BY copied_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	out := make([]Clip, 0, len(rows.Values))
	for _, v := range rows.Values {
		out = append(out, Clip{ID: i64(v[0]), Text: str(v[1]), App: str(v[2]), CopiedAt: time.UnixMilli(i64(v[3]))})
	}
	return out, nil
}
