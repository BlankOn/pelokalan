package models

import (
	"testing"
	"time"
)

func TestReviewReady(t *testing.T) {
	pushedAt := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	full := CatalogStats{Total: 10, Translated: 10}
	cases := []struct {
		name string
		c    Catalog
		want bool
	}{
		{"complete, never sent", Catalog{CanPush: true, Stats: full}, true},
		{"no repository", Catalog{Stats: full}, false},
		{"untranslated left", Catalog{CanPush: true, Stats: CatalogStats{Total: 10, Translated: 9, Untranslated: 1}}, false},
		{"needs review (fuzzy or AI)", Catalog{CanPush: true, Stats: CatalogStats{Total: 10, Translated: 10, Fuzzy: 1}}, false},
		{"review open", Catalog{CanPush: true, Stats: full, Review: &ModuleReview{Status: ReviewRequested}}, false},
		{"in the repository, unchanged", Catalog{CanPush: true, Stats: full, UpdatedAt: pushedAt.Add(-time.Hour),
			Review: &ModuleReview{Status: ReviewPushed, ReviewedAt: &pushedAt}}, false},
		{"edited after the commit", Catalog{CanPush: true, Stats: full, UpdatedAt: pushedAt.Add(time.Hour),
			Review: &ModuleReview{Status: ReviewPushed, ReviewedAt: &pushedAt}}, true},
		{"sent back earlier", Catalog{CanPush: true, Stats: full, Review: &ModuleReview{Status: ReviewRejected}}, true},
		{"empty file", Catalog{CanPush: true}, false},
	}
	for _, tc := range cases {
		if got := tc.c.ReviewReady(); got != tc.want {
			t.Errorf("%s: ReviewReady = %v, want %v", tc.name, got, tc.want)
		}
	}
}
