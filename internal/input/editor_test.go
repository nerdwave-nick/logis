package input

import (
	"strings"
	"testing"
)

// Tests ported from Rust src/input/editor.rs.

func typed(text string) *Query {
	q := &Query{}
	q.Apply(Edit{Kind: InsertText, Text: text})
	return q
}

func apply(q *Query, k EditKind) bool { return q.Apply(Edit{Kind: k}) }

func wantQuery(t *testing.T, q *Query, text string, cursor int) {
	t.Helper()
	if q.Text() != text || q.Cursor() != cursor {
		t.Errorf("query %q at %d, want %q at %d", q.Text(), q.Cursor(), text, cursor)
	}
}

func TestInsertPutsTextAtTheCursor(t *testing.T) {
	q := typed("ac")
	if !apply(q, Left) || !q.Apply(Edit{Kind: InsertText, Text: "b"}) {
		t.Fatal("no change")
	}
	wantQuery(t, q, "abc", 2)
	if q.Apply(Edit{Kind: InsertText}) {
		t.Error("an empty insert changed the query")
	}
}

// e + combining acute, a flag (two regional indicators), a family emoji
// joined by ZWJs, a precomposed letter: each is one grapheme.
func TestBackspaceDeletesOneWholeGrapheme(t *testing.T) {
	for _, g := range []string{"é", "🇩🇪", "👨‍👩‍👧", "ö"} {
		q := typed("x" + g)
		if !apply(q, DeleteBack) {
			t.Errorf("%q: no change", g)
		}
		wantQuery(t, q, "x", 1)
	}
	if apply(&Query{}, DeleteBack) {
		t.Error("Backspace on an empty query changed it")
	}
}

func TestDeleteForwardRemovesTheGraphemeAfterTheCursor(t *testing.T) {
	q := typed("éx")
	apply(q, Home)
	if !apply(q, DeleteForward) {
		t.Fatal("no change")
	}
	wantQuery(t, q, "x", 0)
	apply(q, End)
	if apply(q, DeleteForward) {
		t.Error("Delete at the end changed the query")
	}
}

func TestTheCursorMovesByGraphemesAndStopsAtTheEnds(t *testing.T) {
	q := typed("a🇩🇪b")
	for range 2 {
		if !apply(q, Left) {
			t.Fatal("Left did not move")
		}
	}
	if q.Cursor() != 1 {
		t.Fatalf("cursor %d", q.Cursor())
	}
	if !apply(q, Right) || q.Cursor() != 1+len("🇩🇪") {
		t.Fatalf("cursor %d", q.Cursor())
	}
	if !apply(q, Home) || apply(q, Left) {
		t.Error("Left at the start moved")
	}
	if !apply(q, End) || apply(q, Right) || q.Cursor() != len(q.Text()) {
		t.Error("Right at the end moved")
	}
}

func TestCtrlWDeletesTrailingWhitespaceAndThenOneWord(t *testing.T) {
	q := typed("firefox --private  ")
	if !apply(q, DeleteWordBack) || q.Text() != "firefox " {
		t.Fatalf("%q", q.Text())
	}
	if !apply(q, DeleteWordBack) || q.Text() != "" {
		t.Fatalf("%q", q.Text())
	}
	if apply(q, DeleteWordBack) {
		t.Error("Ctrl+W on an empty query changed it")
	}
	// Any Unicode white space separates words.
	q = typed("rite of　")
	apply(q, DeleteWordBack)
	wantQuery(t, q, "rite ", len("rite "))
}

func TestCtrlWDeletesOnlyBeforeTheCursor(t *testing.T) {
	q := typed("über alles")
	for range len(" alles") {
		apply(q, Left)
	}
	if !apply(q, DeleteWordBack) {
		t.Fatal("no change")
	}
	wantQuery(t, q, " alles", 0)
}

func TestCtrlUClearsTextAndCursor(t *testing.T) {
	q := typed("Ave Omnissiah")
	if !apply(q, Clear) {
		t.Fatal("no change")
	}
	wantQuery(t, q, "", 0)
	if apply(q, Clear) {
		t.Error("clearing an empty query changed it")
	}
}

// The cursor stays on grapheme boundaries whatever is inserted.
func TestTheCursorStaysOnGraphemeBoundaries(t *testing.T) {
	q := typed(strings.Repeat("é", 3))
	apply(q, Left)
	if q.Cursor() != 2*len("é") {
		t.Errorf("cursor %d", q.Cursor())
	}
}
