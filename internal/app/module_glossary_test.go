package app_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A module's own glossary translations come first in that module's files: the editor's chips (with the
// project-wide one beside), whole-string glossary pre-translation, and the glossary page's module filter
func TestModuleGlossary(t *testing.T) {
	skipWithoutDB(t)
	lia := signUp(t, "modgl")
	slug := lia.newProject("Module glossary")
	base := "/projects/" + slug
	pid := projectID(t, slug)

	// the project says "Window" → "Jendela" (two modules); calc says "Layar" (its only translation)
	files := map[string][]byte{
		"board.main.id.po": poFile("board", "Window", "Jendela", "Bishop", "Gajahmada"),
		"chess.main.id.po": poFile("chess", "Window", "Jendela"),
		"calc.main.id.po":  poFile("calc", "Window", "Layar", "Bishop", "Gajahmada"),
	}
	lia.upload(base+"/module-groups/import", url.Values{"name": {"apps"}, "version_name": {"1"}, "language": {"id"}},
		"apps.tar.gz", targz(t, files)).redirectsTo(base + "/settings?")
	if n := scalar[int](t, `SELECT COUNT(*) FROM glossary_term_modules WHERE project_id = $1 AND module_key = 'calc'`, pid); n != 2 {
		t.Fatalf("calc module rows = %d", n)
	}

	own := func(name, module string) int64 {
		t.Helper()
		lia.upload(base+"/upload", url.Values{"language": {"id"}}, name, poFile(module, "Window", "", "Window settings", "")).
			status(http.StatusSeeOther)
		return scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND filename = $2`, pid, name)
	}
	calc, board := own("calc.main.id.po", "calc"), own("board.main.id.po", "board")
	pane := func(cid int64) string {
		id, _ := entry(t, cid, "Window settings")
		return lia.get(fmt.Sprintf("/catalogs/%d/entries/%d", cid, id)).status(http.StatusOK).Body
	}
	if body := pane(calc); !strings.Contains(body, `data-insert="Layar"`) || !strings.Contains(body, "modul ini") ||
		!strings.Contains(body, `class="gl-insert gl-alt" data-insert="Jendela"`) {
		t.Fatalf("calc chips lack the module's translation first:\n%s", body)
	}
	if body := pane(board); !strings.Contains(body, `data-insert="Jendela"`) || strings.Contains(body, "Layar") ||
		strings.Contains(body, "gl-mod") {
		t.Fatalf("board chips:\n%s", body)
	}

	// glossary-only pre-translation applies the module's translation to a whole string
	for _, cid := range []int64{calc, board} {
		lia.post(fmt.Sprintf("/catalogs/%d/ai", cid), url.Values{"glossary_group": {"all"}}).status(http.StatusSeeOther)
		deadline := time.Now().Add(10 * time.Second)
		for scalar[int](t, `SELECT COUNT(*) FROM ai_jobs WHERE catalog_id = $1 AND status = 'running'`, cid) > 0 {
			if time.Now().After(deadline) {
				t.Fatal("pre-translation did not finish")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for cid, want := range map[int64]string{calc: "Layar", board: "Jendela"} {
		id, _ := entry(t, cid, "Window")
		if got := msgstr(t, id); got != want {
			t.Fatalf("catalog %d: Window → %q, want %q", cid, got, want)
		}
	}

	// the glossary's module filter reads every module of a pair
	lia.get(base + "/glossary?module=calc.main").status(http.StatusOK).contains("Layar")
	lia.get(base + "/glossary?module=chess.main").status(http.StatusOK).contains("Jendela").lacks("Gajahmada")
}
