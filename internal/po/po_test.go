package po

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

const sample = `# Indonesian translation.
# Copyright (C) 2026
#, fuzzy
msgid ""
msgstr ""
"Project-Id-Version: demo 1.0\n"
"Language: id\n"
"Plural-Forms: nplurals=1; plural=0;\n"

#. TRANSLATORS: greeting on home page
#: src/main.c:10 src/main.c:20
#, c-format
msgid "Hello, %s!"
msgstr "Halo, %s!"

# a translator note
#: src/menu.c:5
msgctxt "menu"
msgid "Open"
msgstr ""

#, fuzzy
#| msgid "One file"
msgid "%d file"
msgid_plural "%d files"
msgstr[0] "%d berkas"

msgid ""
"A very long text that spans "
"multiple lines.\n"
"Second line."
msgstr "Teks \"panjang\"\tdengan tab"

#~ msgid "Removed"
#~ msgstr "Dihapus"
`

func TestParse(t *testing.T) {
	c, err := ParseBytes([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.HeaderField("Language"); got != "id" {
		t.Errorf("Language = %q", got)
	}
	if !c.HeaderFuzzy || len(c.HeaderComments) != 2 {
		t.Errorf("header comments/fuzzy wrong: %v %v", c.HeaderComments, c.HeaderFuzzy)
	}
	if len(c.Entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(c.Entries))
	}
	e := c.Entries[0]
	if e.ID != "Hello, %s!" || e.Translation[0] != "Halo, %s!" || !e.HasFlag("c-format") || len(e.References) != 1 || e.ExtractedComments[0] != "TRANSLATORS: greeting on home page" {
		t.Errorf("entry 0 wrong: %+v", e)
	}
	e = c.Entries[1]
	if !e.HasContext || e.Context != "menu" || e.IsTranslated() || e.TranslatorComments[0] != "a translator note" {
		t.Errorf("entry 1 wrong: %+v", e)
	}
	e = c.Entries[2]
	if !e.IsPlural() || !e.Fuzzy || e.Previous[0] != `msgid "One file"` || len(e.Translation) != 1 {
		t.Errorf("entry 2 wrong: %+v", e)
	}
	e = c.Entries[3]
	if e.ID != "A very long text that spans multiple lines.\nSecond line." || e.Translation[0] != "Teks \"panjang\"\tdengan tab" {
		t.Errorf("entry 3 wrong: %q / %q", e.ID, e.Translation)
	}
	if !c.Entries[4].Obsolete || c.Entries[4].Translation[0] != "Dihapus" {
		t.Errorf("entry 4 wrong: %+v", c.Entries[4])
	}
}

func TestRoundTrip(t *testing.T) {
	c, err := ParseBytes([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	out := c.Bytes()
	c2, err := ParseBytes(out)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, out)
	}
	if !bytes.Equal(out, c2.Bytes()) {
		t.Fatalf("not stable:\n%s\n---\n%s", out, c2.Bytes())
	}
	if len(c2.Entries) != len(c.Entries) || c2.Header != c.Header {
		t.Fatal("content changed")
	}
	if !strings.Contains(string(out), "#~ msgid \"Removed\"") {
		t.Errorf("obsolete entry lost:\n%s", out)
	}
}

func TestLongLineWrap(t *testing.T) {
	long := strings.Repeat("word ", 40)
	c := &Catalog{Header: "Language: id\n", Entries: []*Entry{{ID: long, Translation: []string{""}}}}
	out := string(c.Bytes())
	for _, line := range strings.Split(out, "\n") {
		if len(line) > wrapWidth {
			t.Errorf("line too long (%d): %q", len(line), line)
		}
	}
	c2, err := ParseBytes([]byte(out))
	if err != nil || c2.Entries[0].ID != long {
		t.Fatalf("wrap round trip failed: %v", err)
	}
}

func TestCompileMO(t *testing.T) {
	c, _ := ParseBytes([]byte(sample))
	mo := c.CompileMO()
	le := binary.LittleEndian
	if le.Uint32(mo[0:]) != 0x950412de {
		t.Fatal("bad magic")
	}
	// header is fuzzy so it is skipped; only "Hello" and the long text are
	// translated and not fuzzy.
	if n := le.Uint32(mo[8:]); n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	origTab := le.Uint32(mo[12:])
	l, off := le.Uint32(mo[origTab:]), le.Uint32(mo[origTab+4:])
	first := string(mo[off : off+l])
	if first != "A very long text that spans multiple lines.\nSecond line." {
		t.Errorf("first key = %q (keys must be sorted)", first)
	}
}

func TestValidate(t *testing.T) {
	e := &Entry{ID: "Hello %s, you have %d items", Flags: []string{"c-format"}, Translation: []string{"Halo %s"}}
	if issues := Validate(e, 1); len(issues) != 1 || issues[0] != IssuePlaceholders {
		t.Errorf("issues = %v", issues)
	}
	e.Translation = []string{"Halo %s, ada %d barang"}
	if issues := Validate(e, 1); len(issues) != 0 {
		t.Errorf("issues = %v", issues)
	}
	e = &Entry{ID: "Line\n", Translation: []string{"Baris"}}
	if issues := Validate(e, 1); len(issues) != 1 || issues[0] != IssueNewlineEnd {
		t.Errorf("issues = %v", issues)
	}
	e = &Entry{ID: "{name} joined", Flags: []string{"python-brace-format"}, Translation: []string{"{nama} bergabung"}}
	if issues := Validate(e, 1); len(issues) != 1 {
		t.Errorf("brace issues = %v", issues)
	}
	e = &Entry{ID: "%d file", IDPlural: "%d files", Flags: []string{"c-format"}, Translation: []string{"satu berkas", "%d berkas"}}
	if issues := Validate(e, 2); len(issues) != 0 {
		t.Errorf("plural issues = %v", issues)
	}
}

func TestHeaderFields(t *testing.T) {
	h := SetHeaderField("Language: en\nX-Foo: bar\n", "Language", "id")
	h = SetHeaderField(h, "Plural-Forms", "nplurals=1; plural=0;")
	if GetHeaderField(h, "Language") != "id" || NPlurals(GetHeaderField(h, "Plural-Forms")) != 1 {
		t.Errorf("header = %q", h)
	}
	if NPlurals("") != 2 || NPlurals(LookupLanguage("ar").PluralForms) != 6 || LookupLanguage("de_AT").PluralForms != pluralTwo {
		t.Error("plural lookup wrong")
	}
}

func TestAddContributor(t *testing.T) {
	base := []string{"Indonesian translation.", "Copyright (C) 2026", "FIRST AUTHOR <EMAIL@ADDRESS>, YEAR.", ""}
	got := AddContributor(base, "Ani", "ani@example.id", 2026)
	if got[2] != "Ani <ani@example.id>, 2026." {
		t.Fatalf("placeholder not replaced: %q", got)
	}
	got = AddContributor(got, "Ani", "ANI@example.id", 2027)
	if got[2] != "Ani <ani@example.id>, 2026, 2027." {
		t.Fatalf("year not appended: %q", got[2])
	}
	if again := AddContributor(got, "Ani", "ani@example.id", 2027); len(again) != len(got) || again[2] != got[2] {
		t.Fatal("not idempotent")
	}
	got = AddContributor(got, "Budi", "budi@example.id", 2027)
	if got[len(got)-1] != "Budi <budi@example.id>, 2027." {
		t.Fatalf("not appended: %q", got)
	}
}
