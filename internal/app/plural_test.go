package app_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// poNoPlural is a PO file whose header has no Plural-Forms field
func poNoPlural(module string, extra string) []byte {
	return []byte(fmt.Sprintf("msgid \"\"\nmsgstr \"\"\n\"Project-Id-Version: %s main\\n\"\n\"Language: id\\n\"\n"+
		"\"MIME-Version: 1.0\\n\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n\"Content-Transfer-Encoding: 8bit\\n\"\n\n"+
		"msgid \"Chess Clock\"\nmsgstr \"Jam Catur\"\n\n%s", module, extra))
}

// Plural-Forms is written only when the file needs it (plural strings) or already had it
func TestPluralFormsHeader(t *testing.T) {
	skipWithoutDB(t)
	lia := signUp(t, "plural")
	slug := lia.newProject("Plural")
	base := "/projects/" + slug
	pid := projectID(t, slug)

	download := func(name string) string {
		t.Helper()
		cid := scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND filename = $2`, pid, name)
		return lia.get(fmt.Sprintf("/catalogs/%d/download", cid)).status(http.StatusOK).Body
	}
	upload := func(name string, data []byte) {
		t.Helper()
		lia.upload(base+"/upload", url.Values{"language": {"id"}}, name, data).status(http.StatusSeeOther)
	}

	// no plural strings, no field: none is added (the language's rule still applies to the checks)
	upload("clock.main.id.po", poNoPlural("clock", ""))
	if body := download("clock.main.id.po"); strings.Contains(body, "Plural-Forms") {
		t.Fatalf("Plural-Forms added to a file without plural strings:\n%s", body)
	}
	// plural strings need it: added
	upload("timer.main.id.po", poNoPlural("timer", "msgid \"%d second\"\nmsgid_plural \"%d seconds\"\nmsgstr[0] \"%d detik\"\n"))
	if body := download("timer.main.id.po"); !strings.Contains(body, `"Plural-Forms: nplurals=1; plural=0;\n"`) {
		t.Fatalf("Plural-Forms missing for plural strings:\n%s", body)
	}
	// a file the field was added to by an earlier upload loses it when its upstream copy has none
	lia.upload(base+"/module-groups/import", url.Values{"name": {"up"}, "version_name": {"1"}, "language": {"id"}},
		"up.tar.gz", targz(t, map[string][]byte{"board.main.id.po": poNoPlural("board", "")})).redirectsTo(base + "/settings?")
	upload("board.main.id.po", poNoPlural("board", ""))
	if _, err := env.DB.Exec(`UPDATE catalogs SET header = header || 'Plural-Forms: nplurals=1; plural=0;' || E'\n'
		WHERE project_id = $1 AND filename = 'board.main.id.po'`, pid); err != nil {
		t.Fatal(err)
	}
	if body := download("board.main.id.po"); strings.Contains(body, "Plural-Forms") {
		t.Fatalf("Plural-Forms kept although upstream has none:\n%s", body)
	}
	// ...but a file whose own upload had it keeps it
	upload("knight.main.id.po", poFile("knight", "Knight", "Kuda"))
	if body := download("knight.main.id.po"); !strings.Contains(body, "Plural-Forms") {
		t.Fatalf("Plural-Forms dropped from a file that had it:\n%s", body)
	}
}
