package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pelokalan/internal/models"
	"pelokalan/pkg/tpsite"
)

// a Translation Project team page with one translated domain and one only offered as a template
const tpTeamPage = `<html><body><table align="center" border="2">
<tr><th>Domain</th><th>Version</th><th>Translated</th><th>Updated</th><th>Assigned translator</th></tr>
<tr>
 <td align="left" class="restricted"><a href="../domain/kalku.html">kalku</a></td>
 <td><a href="../PO-files/id/kalku-2.1.id.po">2.1</a></td>
 <td class="level4">1 / 2</td>
 <td class="date">2026-10-01</td>
 <td align="left"><a href="mailto:x (at) example (dot) org">Budi Penerjemah</a></td>
</tr>
<tr>
 <td align="left" class="liberal"><a href="../domain/catat.html">catat</a></td>
 <td><a href="../POT-files/catat-0.9.pot">0.9</a></td>
 <td class="untranslated">0 / 1</td>
 <td class="date">&nbsp;</td>
 <td>&nbsp;</td>
</tr>
</table></body></html>`

// Translation Project team page → project files (translated domain and template) → a member's file
// downloaded under the robot's name, with the submission email
func TestTranslationProject(t *testing.T) {
	skipWithoutDB(t)
	var fileHits atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/team/id.html":
			fmt.Fprint(w, tpTeamPage)
		case "/PO-files/id/kalku-2.1.id.po":
			fileHits.Add(1)
			w.Write(poFile("kalku", "Add", "Tambah", "Divide", "")) // header still says "kalku main"
		case "/POT-files/catat-0.9.pot":
			fileHits.Add(1)
			fmt.Fprint(w, "#, fuzzy\nmsgid \"\"\nmsgstr \"\"\n\"Project-Id-Version: catat 0.9\\n\"\n\"Content-Type: text/plain; charset=CHARSET\\n\"\n\nmsgid \"Note\"\nmsgstr \"\"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	old := tpsite.Base
	tpsite.Base = site.URL
	defer func() { tpsite.Base = old }()

	owner := signUp(t, "tpowner")
	slug := owner.newProject("TP")
	base := "/projects/" + slug
	pid := projectID(t, slug)

	owner.upload(base+"/module-groups/sources", url.Values{"source": {"tp"}, "tp_team": {"bukan tim"}, "name": {"tp"}, "version_name": {"id"}}, "", nil).
		status(http.StatusOK).contains("Tim Translation Project tidak valid")
	owner.upload(base+"/module-groups/sources", url.Values{"source": {"tp"}, "tp_team": {"id"}, "name": {"tp"}, "version_name": {"id"}}, "", nil).
		redirectsTo(base + "/settings?")
	// the first download runs in the background
	deadline := time.Now().Add(10 * time.Second)
	for scalar[int](t, `SELECT count(*) FROM module_groups WHERE project_id = $1 AND last_fetched_at IS NOT NULL`, pid) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the team page was not synced")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e := scalar[string](t, `SELECT last_fetch_error FROM module_groups WHERE project_id = $1`, pid); e != "" {
		t.Fatalf("sync error: %s", e)
	}
	if n := scalar[int](t, `SELECT count(*) FROM project_files WHERE project_id = $1 AND language = 'id'`, pid); n != 2 {
		t.Fatalf("project files = %d, want 2", n)
	}
	files := owner.get(base + "/files").status(http.StatusOK)
	files.contains("kalku.id.po", "catat.id.po", "TP 2.1 · 50%", "TP 0.9 · 0%", "Budi Penerjemah", "perlu disclaimer")

	// an unchanged page downloads nothing again
	g, _ := env.Glossary.Groups(context.Background(), mustProject(t, pid))
	before := fileHits.Load()
	if out := env.Glossary.FetchGroup(context.Background(), g[0]); out.Err != nil {
		t.Fatal(out.Err)
	}
	if fileHits.Load() != before {
		t.Fatalf("unchanged domains downloaded again")
	}

	// take the translated domain; the menu offers the robot's file name and email
	fid := scalar[int64](t, `SELECT id FROM project_files WHERE project_id = $1 AND filename = 'kalku.id.po'`, pid)
	owner.post(fmt.Sprintf("%s/project-files/%d/take", base, fid), nil)
	cid := scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND owner_id = $2 AND deleted_at IS NULL`, pid, owner.ID)
	owner.get(base+"/my-files").status(http.StatusOK).
		contains("Unduh untuk Translation Project", "mailto:robot@translationproject.org?subject=kalku-2.1.id.po")
	dl := owner.get(fmt.Sprintf("/catalogs/%d/tp", cid)).status(http.StatusOK)
	if cd := dl.Header.Get("Content-Disposition"); !strings.Contains(cd, "kalku-2.1.id.po") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if !strings.Contains(dl.Body, `"Project-Id-Version: kalku 2.1\n"`) || !strings.Contains(dl.Body, `msgstr "Tambah"`) {
		t.Fatalf("TP file:\n%s", dl.Body)
	}
	// the editor's "Modul" opens the domain page; other members cannot download the owner's TP file
	loc := owner.get(fmt.Sprintf("/catalogs/%d", cid)).redirectsTo(base)
	owner.get(loc).status(http.StatusOK).contains("translationproject.org/domain/kalku.html")
	stranger := signUp(t, "tpstranger")
	if r := stranger.get(fmt.Sprintf("/catalogs/%d/tp", cid)); r.Status == http.StatusOK {
		t.Fatalf("someone else downloaded the owner's TP file")
	}
}

func mustProject(t *testing.T, id int64) *models.Project {
	t.Helper()
	p, err := env.Repos.Project.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
