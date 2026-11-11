package service

import (
	"testing"

	"pelokalan/internal/models"
)

func TestValidateEntryChecksOff(t *testing.T) {
	c := &models.Catalog{NPlurals: 1}
	e := &models.Entry{MsgID: "Hello %s ", MsgStr: []string{"Halo"}, Flags: "c-format"}
	if got := validateEntry(c, e); len(got) != 2 {
		t.Fatalf("want placeholders and space_end, got %v", got)
	}
	c.ChecksOff = []string{"space_end"}
	if got := validateEntry(c, e); len(got) != 1 || got[0] != "placeholders" {
		t.Fatalf("space_end not switched off: %v", got)
	}
	c.ChecksOff = []string{"space_end", "placeholders"}
	if got := validateEntry(c, e); got != nil {
		t.Fatalf("want no issues, got %v", got)
	}
}
