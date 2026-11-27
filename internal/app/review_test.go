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

	"pelokalan/internal/models"
)

// Full GitHub integration: the app's access is checked, the integration turned on; a member asks for a review,
// the owner sends it back, then approves it, and Pelokalan commits the file (author: the translator) to the
// branch's newest version without undoing a change made there meanwhile
func TestGitHubReviews(t *testing.T) {
	skipWithoutDB(t)
	ctx := context.Background()
	owner := signUp(t, "rvowner")
	slug := owner.newProject("Docs")
	base := "/projects/" + slug
	pid := projectID(t, slug)
	kiki := signUp(t, "rvkiki")
	addMember(t, owner, slug, kiki, "translator")

	// a module group from a GitHub branch (imported by hand here: tests never download from GitHub)
	repo := "pelokalan-test/" + unique("docs")
	g, err := env.Repos.Glossary.GetOrCreateGroup(ctx, pid, "docs", "3.14")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Repos.Glossary.SetGroupGit(ctx, g.ID, repo, "3.14"); err != nil {
		t.Fatal(err)
	}
	file := poFile("os", "Open the file", "Buka berkasnya", "Close the window", "", "Remove the directory", "")
	owner.upload(base+"/module-groups/import", url.Values{"name": {"docs"}, "version_name": {"3.14"}, "language": {"id"}},
		"docs.tar.gz", targz(t, map[string][]byte{"library/os.po": file})).redirectsTo(base + "/settings?")
	gid := strconv.FormatInt(g.ID, 10)

	// the popup offers to install the app; checking without an installation reports it
	owner.get(base+"/settings?groups=list").status(http.StatusOK).contains("Integrasi penuh dengan repositori",
		"https://github.com/apps/pelokalan-test/installations/new")
	owner.post(base+"/module-groups/"+gid+"/github/check", nil).redirectsTo(base + "/settings?")
	owner.get(base + "/settings?groups=list").status(http.StatusOK).contains("belum terpasang")
	owner.post(base+"/module-groups/"+gid+"/github/direct", url.Values{"on": {"1"}}).status(http.StatusOK).contains("belum terpasang")

	// installed with write access: the integration can be turned on (by managers only)
	ghFake.Install(repo, true)
	ghFake.SetFile(repo, "3.14", "library/os.po", file)
	kiki.post(base+"/module-groups/"+gid+"/github/direct", url.Values{"on": {"1"}})
	if scalar[bool](t, `SELECT git_direct FROM module_groups WHERE id = $1`, g.ID) {
		t.Fatal("a translator turned the integration on")
	}
	owner.post(base+"/module-groups/"+gid+"/github/direct", url.Values{"on": {"1"}}).redirectsTo(base + "/settings?")
	if !scalar[bool](t, `SELECT git_direct FROM module_groups WHERE id = $1`, g.ID) {
		t.Fatal("integration not on")
	}

	// kiki takes the file; with nothing changed there is nothing to review
	pf := scalar[int64](t, `SELECT id FROM project_files WHERE project_id = $1 AND repo_path = 'library/os.po'`, pid)
	kiki.post(fmt.Sprintf("%s/project-files/%d/take", base, pf), nil)
	kc := scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND owner_id = $2`, pid, kiki.ID)
	kiki.get(base+"/my-files").status(http.StatusOK).contains("Ajukan tinjauan", fmt.Sprintf(`data-review="/catalogs/%d/review"`, kc))
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).status(http.StatusBadRequest).contains("Tidak ada perubahan")

	save := func(msgid, text string) {
		t.Helper()
		id, ver := entry(t, kc, msgid)
		kiki.post(fmt.Sprintf("/catalogs/%d/entries/%d", kc, id), url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {text}}).status(http.StatusOK)
	}
	save("Close the window", "Tutup jendela")
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), url.Values{"note": {"Mohon dicek istilahnya."}}).redirectsTo(base + "/files")
	mails.waitFor(t, owner.Email, "library/os.po")
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).status(http.StatusBadRequest).contains("sudah menunggu tinjauan")
	rid := scalar[int64](t, `SELECT id FROM module_reviews WHERE catalog_id = $1 AND status = 'requested'`, kc)
	page := fmt.Sprintf("%s/reviews/%d", base, rid)

	// a reviewer sees the queue and the review editor, but approving stays with owners and coordinators
	rina := signUp(t, "rvrina")
	addMember(t, owner, slug, rina, "reviewer")
	rina.get(base+"/files").status(http.StatusOK).contains("Menunggu tinjauan", page)
	rina.get(page).status(http.StatusOK).contains("review-mode", "Disetujui oleh pemilik atau koordinator").lacks("rv-approve")
	rina.post(page+"/approve", nil).status(http.StatusForbidden)
	// syncing with upstream is for owners and coordinators only
	rina.post(base+"/project-files/sync", nil).status(http.StatusForbidden)
	kiki.post(base+"/project-files/sync", nil).status(http.StatusForbidden)
	kiki.get(base + "/files").status(http.StatusOK).lacks("/project-files/sync")
	rkey := fmt.Sprintf("%d-%s", kc, models.KeyHashOf("Close the window"))
	rina.hx(base+"/discussions/po_entry/"+rkey+"/comments", url.Values{"body": {"Sudah pas."}}).status(http.StatusOK).
		contains(`data-review-open="1"`)
	rc := scalar[int64](t, `SELECT id FROM comments WHERE review_id = $1`, rid)
	rina.hx(fmt.Sprintf("%s/comments/%d/resolve", base, rc), nil).status(http.StatusOK).contains(`data-review-open="0"`)

	// the owner sees the queue and the change; kiki cannot decide on her own request
	owner.get(base+"/files").status(http.StatusOK).contains("Menunggu tinjauan", page)
	// in Berkas anggota lain the owner gets "Tinjau" for a file waiting for review
	owner.get(base + "/member-files").status(http.StatusOK).contains(`href="` + page + `">Tinjau`)
	// the review editor: the review's strings only, the diff, the note, the approval button; no edit pane tools
	owner.get(page).status(http.StatusOK).contains("review-mode", `&#43;msgstr &#34;Tutup jendela&#34;`, "Mohon dicek istilahnya.",
		"Setujui &amp; commit", `data-review-view="diff"`, "Close the window").lacks("Open the file", "tb-commit")
	// the translator who asked opens their full editor instead
	kiki.get(page).redirectsTo(base + "/" + kiki.Username + "/po/library__os.po")
	kiki.post(page+"/approve", nil).status(http.StatusForbidden)

	// sent back with a note
	owner.post(page+"/reject", url.Values{"note": {""}}).status(http.StatusBadRequest)
	owner.post(page+"/reject", url.Values{"note": {"Pakai \"Tutup jendelanya\"."}}).redirectsTo(page)
	mails.waitFor(t, kiki.Email, "Tutup jendelanya")
	kiki.get(base + "/my-files").status(http.StatusOK).contains("Perlu perbaikan")

	// fixed and asked again; meanwhile someone changes another string on the branch
	save("Close the window", "Tutup jendelanya")
	save("Remove the directory", "Hapus direktori")
	// the popup sends it in the background and stays on the page (JSON answer, the rows refresh)
	fetchReview := func() *response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/catalogs/%d/review", kc), strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Requested-With", "fetch")
		return kiki.do(req)
	}
	fetchReview().status(http.StatusOK).contains(`"message":"Tinjauan diajukan.`)
	fetchReview().status(http.StatusBadRequest).contains(`"error":`, "sudah menunggu tinjauan")
	rid = scalar[int64](t, `SELECT id FROM module_reviews WHERE catalog_id = $1 AND status = 'requested'`, kc)
	page = fmt.Sprintf("%s/reviews/%d", base, rid)
	ghFake.SetFile(repo, "3.14", "library/os.po", poFile("os", "Open the file", "Buka berkas tersebut", "Close the window", "", "Remove the directory", ""))

	// a review comment on a string: approval waits until it is resolved
	key := fmt.Sprintf("%d-%s", kc, models.KeyHashOf("Remove the directory"))
	owner.hx(base+"/discussions/po_entry/"+key+"/comments", url.Values{"body": {"Pakai \"direktorinya\"?"}}).status(http.StatusOK).
		contains("Sudah diselesaikan", "btn-approve rv-resolve", `data-review-open="1"`)
	kiki.hx(base+"/discussions/po_entry/"+key+"/comments", url.Values{"body": {"Sudah saya pikirkan."}}).status(http.StatusOK)
	if n := scalar[int](t, `SELECT count(*) FROM comments WHERE review_id = $1 AND resolved_at IS NULL`, rid); n != 1 {
		t.Fatalf("review comments = %d, want 1 (the translator's reply is a plain comment)", n)
	}
	owner.post(page+"/approve", nil).redirectsTo(page)
	if len(ghFake.LastCommits()) != 0 {
		t.Fatal("approved with an open review comment")
	}
	owner.get(page).status(http.StatusOK).contains("1 komentar tinjauan belum diselesaikan", "rv-approve", "disabled")
	kiki.get(base + "/my-files").status(http.StatusOK).contains("Perlu perbaikan · 1")
	// the translator's editor counts open review comments beside the sidebar handle; the review editor has none
	kiki.get(fmt.Sprintf("%s/%s/po/library__os.po", base, kiki.Username)).status(http.StatusOK).
		contains(`id="side-review-badge" data-n="1"`, `data-review-open="1"`)
	owner.get(page).status(http.StatusOK).lacks("side-review-badge")
	kiki.get(fmt.Sprintf("/catalogs/%d/list?status=review", kc)).status(http.StatusOK).contains("Remove the directory", "mark-review").lacks("Open the file")
	// the review editor's toggle: only the review's strings that still have an open comment
	owner.get(page).status(http.StatusOK).contains(`class="rv-open-filter"`, "data-filter-review", `name="review" value="`+strconv.FormatInt(rid, 10)+`"`)
	owner.get(fmt.Sprintf("/catalogs/%d/list?review=%d&status=review", kc, rid)).status(http.StatusOK).
		contains("Remove the directory", "mark-review").lacks("Close the window")
	owner.get(fmt.Sprintf("/catalogs/%d/list?review=%d", kc, rid)).status(http.StatusOK).contains("Remove the directory", "Close the window")
	// a link to the translator's file (discussion, email) takes a reviewer to the review editor on the same string
	fileURL := fmt.Sprintf("%s/%s/po/library__os.po", base, kiki.Username)
	owner.get(fileURL + "?entry=abc123").redirectsTo(page + "?entry=abc123")
	owner.get(fileURL + "?view=1").status(http.StatusOK)      // "Lihat" stays the view-only layout
	kiki.get(fileURL + "?entry=abc123").status(http.StatusOK) // the translator keeps their own editor
	cmt := scalar[int64](t, `SELECT id FROM comments WHERE review_id = $1`, rid)
	visitor(t).post(fmt.Sprintf("%s/comments/%d/resolve", base, cmt), nil)
	if scalar[bool](t, `SELECT resolved_at IS NOT NULL FROM comments WHERE id = $1`, cmt) {
		t.Fatal("a visitor resolved a review comment")
	}
	kiki.hx(fmt.Sprintf("%s/comments/%d/resolve", base, cmt), nil).status(http.StatusOK).contains("Diselesaikan oleh", `data-review-open="0"`)

	// the diff shows the header credit the commit adds for the translator
	year := strconv.Itoa(time.Now().Year())
	owner.get(page).status(http.StatusOK).contains(kiki.Email + "&gt;, " + year + ".")

	owner.post(page+"/approve", nil).redirectsTo(page)
	commits := ghFake.LastCommits()
	if len(commits) != 1 {
		t.Fatalf("commits = %d", len(commits))
	}
	cm := commits[0]
	if cm.AuthorEmail != kiki.Email || cm.Path != "library/os.po" || cm.Branch != "3.14" ||
		!strings.Contains(cm.Message, "Update translation of library/os.po") || !strings.Contains(cm.Message, "Reviewed-by: ") ||
		!strings.Contains(cm.Message, "2 strings translated") {
		t.Fatalf("commit: %+v", cm)
	}
	body := string(cm.Data)
	for _, want := range []string{`msgstr "Tutup jendelanya"`, `msgstr "Hapus direktori"`, `msgstr "Buka berkas tersebut"`,
		"<" + kiki.Email + ">, " + year + ".\nmsgid \"\""} { // credited in the header comments
		if !strings.Contains(body, want) {
			t.Errorf("committed file lacks %s:\n%s", want, body)
		}
	}
	mails.waitFor(t, kiki.Email, "masuk repositori")
	kiki.get(base+"/my-files").status(http.StatusOK).contains("☑ Sudah masuk repositori", cm.SHA[:7]).
		lacks("Semua string sudah diterjemahkan") // in the repository unchanged: no "Ajukan tinjauan" button
	// "Lihat" opens another member's file view-only: the review layout without actions or discussion
	owner.get(base + "/member-files").status(http.StatusOK).contains("library__os.po?view=1")
	owner.get(base+"/"+kiki.Username+"/po/library__os.po?view=1").status(http.StatusOK).
		contains("view-mode", "/take", "Remove the directory").lacks("rv-approve", "tb-commit", "filter-form")
	owner.get(base+"/"+kiki.Username+"/po/library__os.po").status(http.StatusOK).lacks("view-mode").
		contains("editor-others", "Ini berkas milik @"+kiki.Username, "Ambil salinan modul ini") // whose file, and how to get one's own
	kiki.get(base + "/" + kiki.Username + "/po/library__os.po").status(http.StatusOK).lacks("editor-others")
	if st := scalar[string](t, `SELECT status FROM module_reviews WHERE id = $1`, rid); st != "pushed" {
		t.Fatalf("status = %s", st)
	}

	// nothing new: nothing to send; a newer change can be sent by the owner straight away
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).status(http.StatusBadRequest).contains("Tidak ada perubahan")
	save("Open the file", "Buka berkas")
	// with "Wajibkan tinjauan" on, files go only through an approved review: no direct sending
	owner.post(base+"/settings", url.Values{"name": {"Docs"}, "slug": {slug}, "source_language": {"en"}, "access": {"private"},
		"review_required": {"on"}}).redirectsTo(base)
	owner.get(base + "/member-files").status(http.StatusOK).lacks(fmt.Sprintf("/catalogs/%d/push", kc))
	owner.post(fmt.Sprintf("/catalogs/%d/push", kc), nil).status(http.StatusBadRequest).contains("mewajibkan tinjauan")
	owner.post(base+"/settings", url.Values{"name": {"Docs"}, "slug": {slug}, "source_language": {"en"}, "access": {"private"}}).redirectsTo(base)
	owner.get(base + "/member-files").status(http.StatusOK).contains(fmt.Sprintf("/catalogs/%d/push", kc))
	kiki.post(fmt.Sprintf("/catalogs/%d/push", kc), nil).status(http.StatusForbidden)
	// "Open the file" was changed on the branch by someone else after kiki took the file ("Buka berkas tersebut"):
	// sending kiki's "Buka berkas" would overwrite it, so it is refused until she pulls the upstream change
	owner.post(fmt.Sprintf("/catalogs/%d/push", kc), nil).status(http.StatusBadRequest).
		contains("diubah di repositori", "Open the file")
	if n := len(ghFake.LastCommits()); n != 1 {
		t.Fatalf("commits after a refused push = %d", n)
	}
	if n := scalar[int](t, `SELECT count(*) FROM module_reviews WHERE catalog_id = $1 AND status = 'requested'`, kc); n != 0 {
		t.Fatalf("a refused direct send left %d open requests", n)
	}
	if got := string(ghFake.File(repo, "3.14", "library/os.po")); !strings.Contains(got, `msgstr "Buka berkas tersebut"`) {
		t.Fatalf("the upstream change was overwritten:\n%s", got)
	}
	// the first commit only counts kiki's own strings as sent: the other translator's string stays theirs
	if n := scalar[int](t, `SELECT entries FROM module_reviews WHERE id = $1`, rid); n != 2 {
		t.Fatalf("pushed entries = %d, want 2", n)
	}

	// a newer upstream copy: one translation changed upstream, one new string; kiki's own work does not count
	kiki.get(base + "/my-files").status(http.StatusOK).lacks("Pembaruan upstream")
	newer := poFile("os", "Open the file", "Buka berkasnya", "Close the window", "", "Remove the directory", "Hapus folder", "Rename the file", "")
	owner.upload(base+"/module-groups/import", url.Values{"name": {"docs"}, "version_name": {"3.14"}, "language": {"id"}},
		"docs.tar.gz", targz(t, map[string][]byte{"library/os.po": newer})).redirectsTo(base + "/settings?")
	kiki.get(base + "/my-files").status(http.StatusOK).contains("Pembaruan upstream · 2")
	kiki.get(base + "/" + kiki.Username + "/po/library__os.po").status(http.StatusOK).contains("tb-badge-up")
}
