package service

import (
	"testing"

	"pelokalan/internal/ai"
	"pelokalan/internal/models"
)

func TestBuildItemDraftMakesGlossaryOptional(t *testing.T) {
	g := ai.NewGlossary([]ai.Term{{Source: "Grid", Translation: "kisi", Share: 1, Words: 1}, {Source: "Learn", Translation: "Belajar", Share: 1, Words: 1}})
	c := &models.Catalog{NPlurals: 1}
	e := &models.Entry{ID: 1, MsgID: "Learn about viewing photos in a grid", MsgStr: []string{"Pelajari cara melihat foto dalam kotak"}, Fuzzy: true}

	fresh := buildItem(e, c, g, nil, nil)
	if len(fresh.Glossary) != 2 || fresh.Glossary[0].Optional || fresh.Glossary[1].Optional || fresh.Current != nil {
		t.Fatalf("without a draft the glossary is required: %+v", fresh)
	}
	draft := buildItem(e, c, g, nil, e.MsgStr)
	if len(draft.Glossary) != 2 || !draft.Glossary[0].Optional || !draft.Glossary[1].Optional || len(draft.Current) != 1 {
		t.Fatalf("with a draft every glossary hint must be optional: %+v", draft)
	}
	if p := ai.Verify(draft, draft.Current, 1); len(p) != 0 {
		t.Fatalf("an unchanged draft must not be flagged: %v", p)
	}
}
