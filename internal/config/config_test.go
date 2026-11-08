package config

import "testing"

func TestWhitelist(t *testing.T) {
	w := ParseWhitelist(" Alice@Example.com, bob@example.org ,,@team.id ")

	cases := map[string]bool{
		"alice@example.com":  true,
		"ALICE@example.com ": true,
		"bob@example.org":    true,
		"carol@example.com":  false,
		"anyone@team.id":     true,
		"anyone@sub.team.id": false,
		"":                   false,
		"team.id":            false,
	}
	for email, want := range cases {
		if got := w.IsAllowed(email); got != want {
			t.Errorf("IsAllowed(%q) = %v, want %v", email, got, want)
		}
	}
	if w.Len() != 3 {
		t.Errorf("Len() = %d, want 3", w.Len())
	}
}

func TestEmptyWhitelistIsOpen(t *testing.T) {
	w := ParseWhitelist(" , ")
	if w.Enabled() || !w.IsAllowed("anyone@example.org") || w.IsAllowed("") {
		t.Fatal("an empty whitelist must allow every address")
	}
	if !ParseWhitelist("a@b.c").Enabled() {
		t.Fatal("a non-empty whitelist must be enabled")
	}
}
