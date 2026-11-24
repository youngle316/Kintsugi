package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func clipTexts(t *testing.T, s *Store) []string {
	t.Helper()
	cs, err := s.Clips(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

func save(t *testing.T, s *Store, text, app string, at time.Time) {
	t.Helper()
	if err := s.SaveClip(context.Background(), Clip{Text: text, App: app, CopiedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func TestCopyingAgainMovesAClipToTheTop(t *testing.T) {
	s, now := openTest(t), time.Now()
	save(t, s, "a", "Mail", now)
	save(t, s, "b", "Mail", now.Add(time.Second))
	save(t, s, "a", "Notes", now.Add(2*time.Second))
	if got := fmt.Sprint(clipTexts(t, s)); got != "[a b]" {
		t.Fatal(got)
	}
	cs, _ := s.Clips(context.Background())
	if cs[0].App != "Notes" {
		t.Fatalf("app %q", cs[0].App)
	}
}

func TestClipsKeepTheNewest(t *testing.T) {
	s, now := openTest(t), time.Now()
	for i := range MaxClips + 3 {
		save(t, s, fmt.Sprint(i), "", now.Add(time.Duration(i)*time.Millisecond))
	}
	got := clipTexts(t, s)
	if len(got) != MaxClips || got[0] != fmt.Sprint(MaxClips+2) || got[MaxClips-1] != "3" {
		t.Fatalf("%d clips, %s to %s", len(got), got[0], got[len(got)-1])
	}
}

func TestDeleteAndClearClips(t *testing.T) {
	s, ctx, now := openTest(t), context.Background(), time.Now()
	save(t, s, "a", "", now)
	save(t, s, "b", "", now.Add(time.Second))
	save(t, s, "c", "", now.Add(2*time.Second))
	cs, _ := s.Clips(ctx)
	if err := s.DeleteClip(ctx, cs[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(clipTexts(t, s)); got != "[c a]" {
		t.Fatal(got)
	}
	if err := s.ClearClips(ctx); err != nil {
		t.Fatal(err)
	}
	if got := clipTexts(t, s); len(got) != 0 {
		t.Fatal(got)
	}
}
