package main

import (
	"strings"
	"testing"
	"time"

	"github.com/youngle316/kintsugi/internal/store"
)

func TestKeepClip(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       bool
	}{
		{"text", "hi", true}, {"empty", "", false}, {"blank", " \n\t", false},
		{"at the limit", strings.Repeat("a", maxClipBytes), true},
		{"over the limit", strings.Repeat("a", maxClipBytes+1), false},
	} {
		if keepClip(c.text) != c.want {
			t.Error(c.name)
		}
	}
}

func TestFilterClipsIgnoresCaseAndKeepsOrder(t *testing.T) {
	all := []store.Clip{{Text: "Beta"}, {Text: "alpha"}, {Text: "ALPHABET"}}
	got := filterClips(all, " alpha ")
	if len(got) != 2 || got[0].Text != "alpha" || got[1].Text != "ALPHABET" {
		t.Fatal(got)
	}
	if len(filterClips(all, "")) != 3 || len(filterClips(all, "zzz")) != 0 {
		t.Fatal("empty or unmatched query")
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("  a\n\t b  c "); got != "a b c" {
		t.Fatalf("%q", got)
	}
	if got := oneLine(strings.Repeat("字", 1000)); len([]rune(got)) > 301 || !strings.HasSuffix(got, "…") {
		t.Fatalf("%d runes", len([]rune(got)))
	}
}

func TestAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Second: "now", -time.Second: "now", 4 * time.Minute: "4m", 3 * time.Hour: "3h", 50 * time.Hour: "2d",
	} {
		if got := age(d); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
}

func TestMoveSelection(t *testing.T) {
	for _, c := range [][4]int{{0, -1, 3, 0}, {0, 1, 3, 1}, {2, 1, 3, 2}, {5, 0, 3, 2}, {0, 1, 0, 0}} {
		if got := moveSelection(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("%v: %d", c, got)
		}
	}
}
