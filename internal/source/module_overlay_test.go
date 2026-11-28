package source

import (
	"testing"

	"pelokalan/pkg/ai"
)

func TestModuleOverlay(t *testing.T) {
	base := ai.NewGlossary([]ai.Term{
		{Source: "Window", Translation: "Jendela", Share: 0.9, Words: 1},
		{Source: "Folder", Translation: "Map", Share: 0.8, Words: 1},
		{Source: "Tab", Translation: "Tab", Share: 1, Words: 1},
		{Source: "Pane", Translation: "Panel", Share: 1, Words: 1, AI: true},
	})
	g := ModuleOverlay(base, []ModuleTerm{
		{Source: "window", Translation: "Layar", Count: 1, Total: 1, Variants: 1, Words: 1},     // only one: wins
		{Source: "Folder", Translation: "Direktori", Count: 1, Total: 3, Variants: 2, Words: 1}, // once of several: no
		{Source: "Tab", Translation: "Tab", Count: 5, Total: 5, Variants: 1, Words: 1},          // same as the project
		{Source: "Pane", Translation: "Bilah", Count: 2, Total: 2, Variants: 1, Words: 1, AI: true},
		{Source: "Shelf", Translation: "Rak", Count: 3, Total: 4, Variants: 2, Words: 1}, // only in the module
	})
	check := func(src, want string, module bool) {
		t.Helper()
		got, ok := g.Lookup(src)
		if !ok || got.Translation != want || got.Module != module {
			t.Fatalf("%s = %+v, want %s (module %v)", src, got, want, module)
		}
	}
	check("Window", "Layar", true)
	check("Folder", "Map", false)
	check("Tab", "Tab", false)
	check("Pane", "Bilah", true) // AI over AI may replace
	check("Shelf", "Rak", true)
	if w, _ := g.Lookup("Window"); w.Source != "Window" || w.Project != "Jendela" || w.Share != 1 {
		t.Fatalf("window = %+v", w)
	}
	if tr, ok := g.Exact("Window"); !ok || tr != "Layar" {
		t.Fatalf("exact = %q %v", tr, ok)
	}
	if tr, _ := base.Exact("Window"); tr != "Jendela" {
		t.Fatal("the base glossary changed")
	}
	// an AI-only module pair never replaces one people agreed on
	g = ModuleOverlay(base, []ModuleTerm{{Source: "Window", Translation: "Layar", Count: 4, Total: 4, Variants: 1, AI: true}})
	if w, _ := g.Lookup("Window"); w.Translation != "Jendela" {
		t.Fatalf("AI module pair won: %+v", w)
	}
	if g.Len() != 4 {
		t.Fatalf("len = %d", g.Len())
	}
}
