package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

// 0.6.0 kept what the user copied: opening its database must leave no
// trace of it in the files.
func TestOpenErasesTheClipsOf060(t *testing.T) {
	ctx, p := context.Background(), filepath.Join(t.TempDir(), "k.db")
	db, err := sqlite.Open(ctx, p, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Query(ctx, "PRAGMA journal_mode = WAL")
	for _, q := range []string{
		`CREATE TABLE clips (id INTEGER PRIMARY KEY, text TEXT NOT NULL UNIQUE, app TEXT NOT NULL DEFAULT '', copied_at INTEGER NOT NULL)`,
		`CREATE INDEX clips_copied ON clips(copied_at)`,
		`INSERT INTO clips (text, copied_at) VALUES ('hunter2-secret', 1)`,
		`INSERT INTO clips (text, copied_at) VALUES ('deleted-secret', 2)`,
		`DELETE FROM clips WHERE text = 'deleted-secret'`,
	} {
		if _, err := db.Execute(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.db.Query(ctx, `SELECT name FROM sqlite_master WHERE name LIKE 'clips%'`)
	if err != nil || len(rows.Values) != 0 {
		t.Fatal(rows.Values, err)
	}
	for _, suffix := range []string{"", "-wal"} {
		data, _ := os.ReadFile(p + suffix)
		for _, secret := range []string{"hunter2-secret", "deleted-secret"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s is still in the database%s", secret, suffix)
			}
		}
	}
}
