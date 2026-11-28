package gitpatch

import (
	"strings"
	"testing"
)

const creditBase = `# SOME DESCRIPTIVE TITLE.
# Copyright (C) 2001-2025, Python Software Foundation
# This file is distributed under the same license as the Python package.
#
# Translators:
# Ana Lama <ana@example.org>, 2024
#
#, fuzzy
msgid ""
msgstr ""
"Project-Id-Version: Python 3.14\n"
"Last-Translator: Ana Lama <ana@example.org>, 2024\n"
"Language: id\n"

msgid "Hello"
msgstr ""

msgid "World"
msgstr "Dunia"
`

func TestCreditHeader(t *testing.T) {
	want := map[string]Target{"Hello": {Translation: []string{"Halo"}}}
	credit := &Credit{Name: "Budi Santoso", Email: "budi@example.org", Year: 2026}
	out, n := ApplyCredit([]byte(creditBase), want, credit)
	if n != 1 {
		t.Fatalf("changed %d entries", n)
	}
	s := string(out)
	for _, line := range []string{
		"# Ana Lama <ana@example.org>, 2024\n# Budi Santoso <budi@example.org>, 2026.\n#\n#, fuzzy\n", // right after the other credit
		`"Last-Translator: Budi Santoso <budi@example.org>\n"`,
		"msgid \"Hello\"\nmsgstr \"Halo\"\n",
		"msgid \"World\"\nmsgstr \"Dunia\"\n", // untouched
		`"Project-Id-Version: Python 3.14\n"`,
	} {
		if !strings.Contains(s, line) {
			t.Errorf("missing %q in\n%s", line, s)
		}
	}
	// the diff shows the same header lines
	d := DiffCredit("library/x.po", []byte(creditBase), want, credit)
	if d == nil || !strings.Contains(d.Hunks, "+# Budi Santoso <budi@example.org>, 2026.") ||
		!strings.Contains(d.Hunks, `-"Last-Translator: Ana Lama <ana@example.org>, 2024\n"`) || d.Changed != 1 {
		t.Fatalf("diff:\n%+v", d)
	}
	// a second round in another year adds the year to the same line; nothing to send = no header change either
	again, _ := ApplyCredit(out, map[string]Target{"Hello": {Translation: []string{"Halo!"}}}, &Credit{Name: "Budi Santoso", Email: "budi@example.org", Year: 2027})
	if !strings.Contains(string(again), "# Budi Santoso <budi@example.org>, 2026-2027.") {
		t.Errorf("year not added:\n%s", again)
	}
	if same, n := ApplyCredit(out, map[string]Target{"Hello": {Translation: []string{"Halo"}}}, credit); n != 0 || string(same) != s {
		t.Error("an unchanged file was rewritten")
	}
	// without a credit nothing in the header moves
	plain, _ := Apply([]byte(creditBase), want)
	if !strings.Contains(string(plain), "Last-Translator: Ana Lama") {
		t.Error("Apply without credit changed the header")
	}
}

// reviewers get their own lines after the translator's; Last-Translator stays the translator
func TestCreditReviewers(t *testing.T) {
	want := map[string]Target{"Hello": {Translation: []string{"Halo"}}}
	credit := &Credit{Name: "Budi Santoso", Email: "budi@example.org", Year: 2026, Reviewers: []Credit{
		{Name: "Citra Dewi", Email: "citra@example.org", Year: 2026},
		{Name: "Ana Lama", Email: "ana@example.org", Year: 2026},    // already credited: her line gains the year
		{Name: "Budi again", Email: "BUDI@example.org", Year: 2026}, // the translator: not twice
	}}
	out, _ := ApplyCredit([]byte(creditBase), want, credit)
	s := string(out)
	for _, line := range []string{
		"# Ana Lama <ana@example.org>, 2024, 2026.\n# Budi Santoso <budi@example.org>, 2026.\n# Citra Dewi <citra@example.org>, 2026.\n#\n",
		`"Last-Translator: Budi Santoso <budi@example.org>\n"`,
	} {
		if !strings.Contains(s, line) {
			t.Fatalf("missing %q in:\n%s", line, s)
		}
	}
	if strings.Contains(s, "Budi again") {
		t.Fatalf("the translator is credited twice:\n%s", s)
	}
}
