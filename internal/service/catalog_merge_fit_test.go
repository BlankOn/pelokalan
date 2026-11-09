package service

import (
	"testing"

	"pelokalan/internal/models"
)

func TestTemplateFits(t *testing.T) {
	ent := func(ids ...string) []*models.Entry {
		var out []*models.Entry
		for _, id := range ids {
			out = append(out, &models.Entry{MsgID: id})
		}
		return out
	}
	tmpl := ent("Open", "Save", "Quit")
	if !templateFits(tmpl, ent("Open", "Save", "Old")) {
		t.Error("same module with one removed string should fit")
	}
	if templateFits(tmpl, ent("Speech", "Braille", "Mouse", "Open")) {
		t.Error("another module must not fit")
	}
	if !templateFits(tmpl, nil) {
		t.Error("empty file fits")
	}
}
