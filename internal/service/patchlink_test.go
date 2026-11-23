package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"pelokalan/internal/models"
)

func TestPatchLinkToken(t *testing.T) {
	s := &AuthService{jwtSecret: []byte("secret")}
	u := &models.User{ID: 42, Email: "a@example.com"}
	now := time.Unix(1_800_000_000, 0)
	tok := s.PatchLinkToken(u, "c", 1234, now)
	// tampered tokens and expired ones are refused before any lookup
	for _, bad := range []string{"", "x", tok + "a", "p" + tok[1:], tok[:len(tok)-2] + "zz"} {
		if _, err := s.ParsePatchLink(context.Background(), bad, now); err != ErrInvalidToken {
			t.Errorf("%q: err = %v, want ErrInvalidToken", bad, err)
		}
	}
	if p, sig, ok := strings.Cut(tok, "."); !ok || strings.Contains(p, "_") || strings.Contains(sig, ".") {
		t.Errorf("token %q", tok)
	}
	if _, err := s.ParsePatchLink(context.Background(), tok, now.Add(PatchLinkTTL+time.Second)); err != ErrInvalidToken {
		t.Errorf("expired: err = %v", err)
	}
	other := &AuthService{jwtSecret: []byte("other")}
	if _, err := other.ParsePatchLink(context.Background(), tok, now); err != ErrInvalidToken {
		t.Errorf("other secret: err = %v", err)
	}
}
