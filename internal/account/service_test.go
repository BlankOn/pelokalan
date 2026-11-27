package account

import (
	"strings"
	"testing"
)

func TestValidUsername(t *testing.T) {
	ok := []string{"ani", "admin", "budi.s", "a_b-c", "user123", "abcdefghijklmnopqrstuvwxyz0123"}
	bad := []string{"", "ab", "Ani", "-ani", "ani-", "a..b", "ani budi", "glossary", "po", strings.Repeat("a", 31), "ánі"}
	for _, u := range ok {
		if err := validUsername(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := validUsername(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if NormalizeUsername(" @Ani.S ") != "ani.s" {
		t.Error("normalize")
	}
}
