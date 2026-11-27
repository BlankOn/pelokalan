package tpsite

import (
	"os"
	"strings"
	"testing"

	"pelokalan/pkg/po"
)

// testdata/team_id.html: https://translationproject.org/team/id.html as saved on 2026-11-25
func TestParseTeamPage(t *testing.T) {
	data, err := os.ReadFile("testdata/team_id.html")
	if err != nil {
		t.Fatal(err)
	}
	domains, err := ParseTeamPage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 147 {
		t.Errorf("domains = %d, want 147", len(domains))
	}
	by := map[string]Domain{}
	templates, restricted := 0, 0
	for _, d := range domains {
		by[d.Name] = d
		if d.Template {
			templates++
		}
		if d.Restricted {
			restricted++
		}
	}
	if templates != 63 || restricted != 78 {
		t.Errorf("templates = %d, restricted = %d", templates, restricted)
	}
	bash := by["bash"]
	if bash.Version != "5.3-rc2" || bash.FileURL != Base+"/PO-files/id/bash-5.3-rc2.id.po" || bash.Template ||
		bash.Translated != 589 || bash.Total != 612 || bash.Updated.Format("2006-01-02") != "2026-08-14" ||
		bash.Translator != "Arif Endro Nugroho" || !bash.Restricted {
		t.Errorf("bash = %+v", bash)
	}
	bee := by["beebase"]
	if !bee.Template || bee.FileURL != Base+"/POT-files/beebase-1.2.pot" || bee.Translated != 0 || bee.Total != 891 ||
		!bee.Updated.IsZero() || bee.Translator != "" || bee.Restricted {
		t.Errorf("beebase = %+v", bee)
	}
	if _, err := ParseTeamPage([]byte("<html><table><tr><td>x</td></tr></table></html>")); err != ErrLayout {
		t.Errorf("empty page: %v", err)
	}
}

func TestNames(t *testing.T) {
	for raw, want := range map[string]string{"id": "id", " pt_BR ": "pt_BR", "https://translationproject.org/team/id.html": "id",
		"sr@latin": "sr@latin"} {
		if got, ok := ParseTeam(raw); !ok || got != want {
			t.Errorf("ParseTeam(%q) = %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "ID", "https://example.com/team/id.html", "https://translationproject.org/domain/bash.html", "../x"} {
		if _, ok := ParseTeam(raw); ok {
			t.Errorf("ParseTeam(%q) accepted", raw)
		}
	}
	if got := SubmitName("bash", "5.3-rc2", "id"); got != "bash-5.3-rc2.id.po" {
		t.Errorf("SubmitName = %q", got)
	}
	if got := FileName("gnubik.x", "id"); got != "gnubik-x.id.po" {
		t.Errorf("FileName = %q", got)
	}
}

func TestPrepare(t *testing.T) {
	latin := []byte("msgid \"\"\nmsgstr \"\"\n\"Language: id\\n\"\n\"Content-Type: text/plain; charset=ISO-8859-1\\n\"\n\n" +
		"msgid \"Cafe\"\nmsgstr \"Caf\xe9\"\n")
	out, err := Prepare(latin, "id", false)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := po.ParseBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Entries[0].Translation[0] != "Café" || !strings.Contains(cat.HeaderField("Content-Type"), "UTF-8") {
		t.Errorf("latin-1 file: %q / %q", cat.Entries[0].Translation[0], cat.HeaderField("Content-Type"))
	}

	pot := []byte("#, fuzzy\nmsgid \"\"\nmsgstr \"\"\n\"Project-Id-Version: beebase 1.2\\n\"\n\"Language: \\n\"\n" +
		"\"Content-Type: text/plain; charset=CHARSET\\n\"\n\n#, c-format\nmsgid \"%d files\"\nmsgid_plural \"%d files\"\nmsgstr[0] \"\"\nmsgstr[1] \"\"\n")
	out, err = Prepare(pot, "id", true)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ = po.ParseBytes(out)
	if cat.HeaderFuzzy || cat.HeaderField("Language") != "id" || cat.HeaderField("Plural-Forms") != po.LookupLanguage("id").PluralForms ||
		!strings.Contains(cat.HeaderField("Content-Type"), "UTF-8") {
		t.Errorf("template header: %q", cat.Header)
	}
	if n := po.NPlurals(po.LookupLanguage("id").PluralForms); len(cat.Entries[0].Translation) != n {
		t.Errorf("plural forms = %d, want %d", len(cat.Entries[0].Translation), n)
	}

	// a UTF-8 file that needs nothing is kept byte for byte
	plain := []byte("msgid \"\"\nmsgstr \"\"\n\"Language: id\\n\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n\nmsgid \"a\"\nmsgstr \"b\"\n")
	if out, _ := Prepare(plain, "id", false); string(out) != string(plain) {
		t.Errorf("unchanged file rewritten:\n%s", out)
	}
}
