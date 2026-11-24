package app_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func skipWithoutDB(t *testing.T) {
	t.Helper()
	if env == nil {
		t.Skip("set TEST_DATABASE_URL to run integration tests (make test-integration)")
	}
}

// Sign up, create a project, upload a PO file, translate in the editor, commit, download
func TestTranslateFlow(t *testing.T) {
	skipWithoutDB(t)
	ani := signUp(t, "ani")
	slug := ani.newProject("Kalkulator")

	// a file named only by its language is renamed after the program (Project-Id-Version)
	loc := ani.upload("/projects/"+slug+"/upload", url.Values{"language": {"id"}}, "id.po",
		poFile("Kalkulator", "Open", "", "Save", "Simpan", "Quit", "")).redirectsTo("/projects/" + slug + "/" + ani.Username + "/po/")
	if !strings.HasSuffix(loc, "/po/kalkulator.main.id.po") {
		t.Fatalf("uploaded file at %s, want it renamed to kalkulator.main.id.po", loc)
	}
	cid := scalar[int64](t, `SELECT id FROM catalogs WHERE filename = 'kalkulator.main.id.po' AND created_by = $1`, ani.ID)

	ani.get(loc).status(http.StatusOK).contains("Open", "Simpan")
	ani.get("/projects/" + slug + "/files").status(http.StatusOK).contains("kalkulator.main.id.po")

	// save a translation in the editor
	eid, ver := entry(t, cid, "Open")
	base := "/catalogs/" + strconv.FormatInt(cid, 10)
	ani.post(base+"/entries/"+strconv.FormatInt(eid, 10), url.Values{
		"version": {strconv.Itoa(ver)}, "msgstr0": {"Buka"}}).status(http.StatusOK)
	if got := msgstr(t, eid); got != "Buka" {
		t.Fatalf("saved %q, want Buka", got)
	}

	// a save with the old version is refused and leaves the text alone
	ani.post(base+"/entries/"+strconv.FormatInt(eid, 10), url.Values{
		"version": {strconv.Itoa(ver)}, "msgstr0": {"Membuka"}}).status(http.StatusOK)
	if got := msgstr(t, eid); got != "Buka" {
		t.Fatalf("stale save overwrote the entry: %q", got)
	}

	// the activity log counts the save as translated
	if n := scalar[int](t, `SELECT count(*) FROM activity_events WHERE user_id = $1 AND catalog_id = $2`, ani.ID, cid); n != 1 {
		t.Fatalf("activity events = %d, want 1", n)
	}

	// commit, then the history lists the version
	ani.post(base+"/commit", url.Values{"message": {"Terjemahkan Open"}}).redirectsTo("/projects/" + slug)
	if n := scalar[int](t, `SELECT count(*) FROM catalog_commits WHERE catalog_id = $1 AND message = 'Terjemahkan Open'`, cid); n != 1 {
		t.Fatalf("commit not stored")
	}
	ani.get(loc + "/history").status(http.StatusOK).contains("Terjemahkan Open")

	// downloads: PO carries the translation and the translator credit, MO compiles
	po := ani.get(base + "/download").status(http.StatusOK)
	po.contains(`msgid "Open"`, `msgstr "Buka"`, "Last-Translator: Ani")
	ani.get(base + "/download.mo").status(http.StatusOK)

	// others can read the file but not change it
	budi := signUp(t, "budi")
	budi.get(loc).status(http.StatusOK)
	r := budi.post(base+"/entries/"+strconv.FormatInt(eid, 10), url.Values{"version": {strconv.Itoa(ver + 1)}, "msgstr0": {"Rusak"}})
	if r.Status == http.StatusOK && msgstr(t, eid) != "Buka" {
		t.Fatalf("another user changed the file")
	}
	if got := msgstr(t, eid); got != "Buka" {
		t.Fatalf("another user's save landed: %q", got)
	}
}
