package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The review editor's consistency check: the review's strings go through the glossary extraction and their terms
// are compared with the glossary ("Window" → "Jendela" from a short string) and with each other.
func TestReviewConsistency(t *testing.T) {
	skipWithoutDB(t)
	ctx := context.Background()
	fake := newFakeExtractAI(map[string][][2]string{
		"Close the window":              {{"window", "jendela"}, {"Close", "Tutup"}},
		"Move the window to the left":   {{"window", "jendela"}, {"left", "kiri"}},
		"Resize the window from a side": {{"window", "layar"}},
	})
	defer fake.Close()

	owner := signUp(t, "csowner")
	slug := owner.newProject("Consistency")
	base := "/projects/" + slug
	pid := projectID(t, slug)
	kiki := signUp(t, "cskiki")
	addMember(t, owner, slug, kiki, "translator")
	rina := signUp(t, "csrina")
	addMember(t, owner, slug, rina, "reviewer")
	owner.post(base+"/ai", url.Values{"base_url": {fake.URL}, "model": {"gpt-4o-mini"}, "api_key": {"sk-test-0123456789"}}).
		redirectsTo(base + "/settings")

	repo := "pelokalan-test/" + unique("cons")
	g, err := env.Repos.Glossary.GetOrCreateGroup(ctx, pid, "docs", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Repos.Glossary.SetGroupGit(ctx, g.ID, repo, "main"); err != nil {
		t.Fatal(err)
	}
	file := poFile("ui", "Window", "Jendela", "Close the window", "", "Move the window to the left", "",
		"Resize the window from a side", "")
	owner.upload(base+"/module-groups/import", url.Values{"name": {"docs"}, "version_name": {"1"}, "language": {"id"}},
		"docs.tar.gz", targz(t, map[string][]byte{"ui.po": file})).redirectsTo(base + "/settings?")
	ghFake.Install(repo, true)
	ghFake.SetFile(repo, "main", "ui.po", file)
	owner.post(base+"/module-groups/"+strconv.FormatInt(g.ID, 10)+"/github/direct", url.Values{"on": {"1"}}).redirectsTo(base + "/settings?")

	pf := scalar[int64](t, `SELECT id FROM project_files WHERE project_id = $1 AND repo_path = 'ui.po'`, pid)
	kiki.post(fmt.Sprintf("%s/project-files/%d/take", base, pf), nil)
	kc := scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND owner_id = $2`, pid, kiki.ID)
	for msgid, text := range map[string]string{"Close the window": "Tutup jendela",
		"Move the window to the left": "Pindahkan jendela ke kiri", "Resize the window from a side": "Ubah ukuran layar dari sisi"} {
		id, ver := entry(t, kc, msgid)
		kiki.post(fmt.Sprintf("/catalogs/%d/entries/%d", kc, id), url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {text}}).
			status(http.StatusOK)
	}
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).redirectsTo(base + "/files")
	rid := scalar[int64](t, `SELECT id FROM module_reviews WHERE catalog_id = $1 AND status = 'requested'`, kc)
	page := fmt.Sprintf("%s/reviews/%d", base, rid)
	check := page + "/consistency"

	// reviewers get the button and the panel; the translator (in their own editor) and visitors do not
	rina.get(page).status(http.StatusOK).contains(`id="btn-consistency"`, `id="cons-panel"`)
	ob := owner.get(page).status(http.StatusOK).contains(`id="btn-consistency"`, `data-review-view="raw"`).Body
	// the review toolbar: Tabel | Diff | Teks, then Konsistensi, Validasi, PO
	order := []string{`data-review-view="table"`, `data-review-view="diff"`, `data-review-view="raw"`, `id="btn-consistency"`,
		`id="btn-validate"`, `<span>PO</span>`}
	for i := 1; i < len(order); i++ {
		if a, b := strings.Index(ob, order[i-1]), strings.Index(ob, order[i]); a < 0 || b < a {
			t.Fatalf("%s should come before %s (%d, %d)", order[i-1], order[i], a, b)
		}
	}
	kiki.get(fmt.Sprintf("/catalogs/%d", kc)).lacks(`id="btn-consistency"`)
	kiki.post(check, nil).status(http.StatusForbidden)
	visitor(t).get(check).redirectsTo("/login")

	rina.get(check).status(http.StatusOK).contains("Periksa")
	rina.post(check, nil).status(http.StatusOK)
	deadline := time.Now().Add(10 * time.Second)
	var body string
	for {
		body = rina.get(check).status(http.StatusOK).Body
		if !strings.Contains(body, "every 2s") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the check did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// "window" is "Jendela" in the glossary; one string says "layar": flagged, both uses listed, the glossary's green
	for _, want := range []string{"3 string", "window", "glosarium:", "Jendela", "layar", "cons-ok", "cons-diff",
		"beragam di berkas ini", "data-goto-entries="} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "kiri") { // consistent terms are not listed
		t.Fatalf("a consistent term is listed:\n%s", body)
	}
	// the strings were sent once and stored: a second check sends nothing
	sent := len(fake.requests())
	rina.post(check, nil).status(http.StatusOK)
	time.Sleep(200 * time.Millisecond)
	if n := len(fake.requests()); n != sent {
		t.Fatalf("a second check sent %d more requests", n-sent)
	}
}
