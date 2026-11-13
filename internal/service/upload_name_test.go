package service

import "testing"

func TestUploadFilename(t *testing.T) {
	cases := []struct {
		file, pv, lang string
		tmpl           bool
		want           string
	}{
		{"id.po", "Constrict main", "id", false, "constrict.main.id.po"},
		{"pt_BR.po", "Constrict 1.2", "pt_BR", false, "constrict.1.2.pt_BR.po"},
		{"messages.pot", "Constrict main", "", true, "constrict.main.pot"},
		{"jv.po", "Foo Bar 3", "jv", false, "foo.bar.3.jv.po"}, // same as the file's language
		{"tecla.po", "Tecla main", "id", false, "tecla.po"},    // already names the program
		{"gdm.master.id.po", "gdm master", "id", false, "gdm.master.id.po"},
		{"id.po", "PACKAGE VERSION", "id", false, "id.po"}, // template placeholder
		{"id.po", "", "id", false, "id.po"},                // nothing to go on
		{"id.po", "Papers (GNOME) 48!", "id", false, "papers.gnome.48.id.po"},
		// domains, branches and placeholders only: no better than "id.po"
		{"ui.main.id.po", "Constrict main", "id", false, "constrict.main.id.po"},
		{"main.id.po", "Constrict main", "id", false, "constrict.main.id.po"},
		{"ui.id.po", "Constrict main", "id", false, "constrict.main.id.po"},
		{"master.pt_BR.po", "Tecla 1.0", "pt_BR", false, "tecla.1.0.pt_BR.po"},
		{"ui.main.pot", "Constrict main", "", true, "constrict.main.pot"},
		{"1.2.id.po", "Constrict 1.2", "id", false, "constrict.1.2.id.po"},
		{"gnome-maps.main.id.po", "Maps main", "id", false, "gnome-maps.main.id.po"}, // names the program
		{"ui.main.id.po", "ui main", "id", false, "ui.main.id.po"},                   // the header is no better
	}
	for _, c := range cases {
		if got := uploadFilename(c.file, c.pv, c.lang, c.tmpl); got != c.want {
			t.Errorf("uploadFilename(%q, %q) = %q, want %q", c.file, c.pv, got, c.want)
		}
	}
}
