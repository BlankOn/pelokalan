package service

import (
	"testing"

	"pelokalan/internal/models"
)

// The "ai" filter lists AI drafts only; "fuzzy" no longer includes them; "todo" includes both
func TestMatchStatusAI(t *testing.T) {
	ai := &models.Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Origin: models.OriginAI}
	fz := &models.Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Fuzzy: true}
	if !matchStatus(ai, "ai") || matchStatus(ai, "fuzzy") || matchStatus(ai, "translated") || !matchStatus(ai, "todo") {
		t.Error("AI draft matched wrongly")
	}
	if matchStatus(fz, "ai") || !matchStatus(fz, "fuzzy") {
		t.Error("fuzzy entry matched wrongly")
	}
	l := &EntryListing{Stats: models.CatalogStats{Fuzzy: 5}, AI: 2}
	if l.FuzzyOnly() != 3 {
		t.Errorf("FuzzyOnly %d", l.FuzzyOnly())
	}
}
