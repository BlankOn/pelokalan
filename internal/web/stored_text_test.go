package web

import "testing"

func TestStoredText(t *testing.T) {
	// %d arguments come back as numbers (stored as text); %s ones stay text even when they look like numbers
	got := storedText("id", "err.review_upstream_conflict|6|“A”, “B”")
	if want := "6 string"; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("storedText = %q", got)
	}
	if got := storedText("id", "commit.from_upstream|51"); got == "" || got == "commit.from_upstream|51" {
		t.Errorf("from_upstream = %q", got)
	}
	if got := storedText("id", "not.a.key|x"); got != "not.a.key|x" {
		t.Errorf("unknown key = %q", got)
	}
}
