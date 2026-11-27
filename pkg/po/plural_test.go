package po

import "testing"

func TestPluralSingleValueForms(t *testing.T) {
	cases := []struct {
		forms  string
		single []bool
	}{
		{"nplurals=1; plural=0;", []bool{false}},
		{"nplurals=2; plural=(n != 1);", []bool{true, false}},
		{"nplurals=2; plural=(n > 1);", []bool{false, false}}, // French: form 0 is 0 and 1
		{"nplurals=3; plural=(n==1) ? 0 : (n>=2 && n<=4) ? 1 : 2;", []bool{true, false, false}},
		{"nplurals=6; plural=(n==0 ? 0 : n==1 ? 1 : n==2 ? 2 : n%100>=3 && n%100<=10 ? 3 : n%100>=11 ? 4 : 5);",
			[]bool{true, true, true, false, false, false}},
	}
	for _, c := range cases {
		p := PluralFor(c.forms, 0)
		if p.N != len(c.single) {
			t.Fatalf("%s: nplurals %d", c.forms, p.N)
		}
		for i, want := range c.single {
			if p.SingleValue(i) != want {
				t.Errorf("%s: form %d single = %v, want %v", c.forms, i, p.SingleValue(i), want)
			}
		}
	}
	// no formula: gettext's default for two forms
	if p := PluralFor("", 2); !p.SingleValue(0) || p.SingleValue(1) {
		t.Errorf("default two forms: %+v", p)
	}
}

// The checks msgfmt -c makes on plural placeholders (the errors reported by l10n.gnome.org).
func TestValidatePluralPlaceholdersLikeMsgfmt(t *testing.T) {
	entry := func(tr ...string) *Entry {
		return &Entry{ID: "One file", IDPlural: "%d files", Translation: tr, Flags: []string{"c-format"}}
	}
	id := PluralFor("nplurals=1; plural=0;", 1)
	if got := ValidateWith(entry("Satu berkas"), id); !has(got, IssuePluralPlaceholders) {
		t.Errorf("Indonesian msgstr[0] without %%d must fail like msgfmt, got %v", got)
	}
	if got := ValidateWith(entry("%d berkas"), id); len(got) != 0 {
		t.Errorf("Indonesian msgstr[0] with %%d is fine, got %v", got)
	}
	en := PluralFor("nplurals=2; plural=(n != 1);", 2)
	if got := ValidateWith(entry("One file", "%d files"), en); len(got) != 0 {
		t.Errorf("English singular form may drop %%d, got %v", got)
	}
	if got := ValidateWith(entry("One file", "Many files"), en); !has(got, IssuePluralPlaceholders) {
		t.Errorf("English plural form must keep %%d, got %v", got)
	}
	fr := PluralFor("nplurals=2; plural=(n > 1);", 2)
	if got := ValidateWith(entry("Un fichier", "%d fichiers"), fr); !has(got, IssuePluralPlaceholders) {
		t.Errorf("French form 0 covers 0 and 1, so it must keep %%d, got %v", got)
	}
	// two placeholders in the plural source
	two := &Entry{ID: "%s has one file", IDPlural: "%s has %d files", Translation: []string{"%s punya satu berkas"}, Flags: []string{"c-format"}}
	if got := ValidateWith(two, id); !has(got, IssuePluralPlaceholders) {
		t.Errorf("missing %%d next to %%s must fail, got %v", got)
	}
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
