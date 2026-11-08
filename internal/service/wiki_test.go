package service

import (
	"testing"

	"pelokalan/internal/models"
)

// tree builds: a > b > c, and d (top level)
func testTree() *WikiTree {
	id := func(n int64) *int64 { return &n }
	pages := []*models.WikiPage{
		{ID: 1, Title: "a"}, {ID: 2, Title: "b", ParentID: id(1)}, {ID: 3, Title: "c", ParentID: id(2)}, {ID: 4, Title: "d"},
	}
	t := &WikiTree{ByID: map[int64]*models.WikiPage{}}
	for _, p := range pages {
		t.ByID[p.ID] = p
	}
	for _, p := range pages {
		if p.ParentID != nil {
			par := t.ByID[*p.ParentID]
			par.Children = append(par.Children, p)
			p.SetParent(par)
		} else {
			t.Roots = append(t.Roots, p)
		}
	}
	var walk func([]*models.WikiPage, int)
	walk = func(l []*models.WikiPage, d int) {
		for _, p := range l {
			p.Depth = d
			walk(p.Children, d+1)
		}
	}
	walk(t.Roots, 1)
	return t
}

func optionIDs(o []WikiParentOption) []int64 {
	var out []int64
	for _, x := range o {
		out = append(out, x.ID)
	}
	return out
}

func TestWikiParentOptions(t *testing.T) {
	tr := testTree()
	cases := []struct {
		name string
		page *models.WikiPage
		want []int64
	}{
		{"new page: any page above level 3", nil, []int64{1, 2, 4}},
		{"leaf c can go under a, b or d", tr.ByID[3], []int64{1, 2, 4}},
		{"b has a child, so only a top-level parent fits (not itself or c)", tr.ByID[2], []int64{1, 4}},
		{"a has two levels below, so it must stay top level", tr.ByID[1], nil},
		{"d (leaf) can go under a or b", tr.ByID[4], []int64{1, 2}},
	}
	for _, c := range cases {
		got := optionIDs(tr.ParentOptions(c.page))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
	if bc := tr.Breadcrumbs(tr.ByID[3]); len(bc) != 2 || bc[0].ID != 1 || bc[1].ID != 2 {
		t.Errorf("breadcrumbs: %v", bc)
	}
}

func TestWikiSlug(t *testing.T) {
	for in, want := range map[string]string{"Panduan Gaya": "panduan-gaya", "Ab": "page-ab", "日本": "page", "Project plan": "project-plan"} {
		if got := wikiSlug(in); got != want {
			t.Errorf("wikiSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
