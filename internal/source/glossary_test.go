package source

import (
	"strings"
	"testing"

	"pelokalan/internal/models"
)

func TestSortVersions(t *testing.T) {
	vs := []*models.ModuleGroup{
		{Name: "gnome", Version: "50"}, {Name: "gnome", Version: "100"}, {Name: "gnome", Version: "51"},
		{Name: "blankon", Version: "13"}, {Name: "gnome", Version: "9.1"},
	}
	SortGroups(vs)
	var got []string
	for _, v := range vs {
		got = append(got, v.Label())
	}
	want := "blankon-13 gnome-100 gnome-51 gnome-50 gnome-9.1"
	if g := strings.Join(got, " "); g != want {
		t.Errorf("order = %q, want %q", g, want)
	}
}
