package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
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

	// the owner sees the queue and the change; kiki cannot decide on her own request
	owner.get(base+"/files").status(http.StatusOK).contains("Menunggu tinjauan", page)
	owner.get(page).status(http.StatusOK).contains(`&#43;msgstr &#34;Tutup jendela&#34;`, "Mohon dicek istilahnya.", "Setujui &amp; commit")
	kiki.get(page).status(http.StatusOK).lacks("Setujui &amp; commit").contains("Batalkan permintaan tinjauan")
	kiki.post(page+"/approve", nil).status(http.StatusForbidden)

	// sent back with a note
	owner.post(page+"/reject", url.Values{"note": {""}}).status(http.StatusBadRequest)
	owner.post(page+"/reject", url.Values{"note": {"Pakai \"Tutup jendelanya\"."}}).redirectsTo(page)
	mails.waitFor(t, kiki.Email, "Tutup jendelanya")
	kiki.get(base + "/my-files").status(http.StatusOK).contains("Perlu perbaikan")

	// fixed and asked again; meanwhile someone changes another string on the branch
	save("Close the window", "Tutup jendelanya")
	save("Remove the directory", "Hapus direktori")
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).redirectsTo(base + "/files")
	rid = scalar[int64](t, `SELECT id FROM module_reviews WHERE catalog_id = $1 AND status = 'requested'`, kc)
	page = fmt.Sprintf("%s/reviews/%d", base, rid)
	ghFake.SetFile(repo, "3.14", "library/os.po", poFile("os", "Open the file", "Buka berkas tersebut", "Close the window", "", "Remove the directory", ""))

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
	for _, want := range []string{`msgstr "Tutup jendelanya"`, `msgstr "Hapus direktori"`, `msgstr "Buka berkas tersebut"`} {
		if !strings.Contains(body, want) {
			t.Errorf("committed file lacks %s:\n%s", want, body)
		}
	}
	mails.waitFor(t, kiki.Email, "masuk repositori")
	kiki.get(base+"/my-files").status(http.StatusOK).contains("Masuk repositori", cm.SHA[:7])
	if st := scalar[string](t, `SELECT status FROM module_reviews WHERE id = $1`, rid); st != "pushed" {
		t.Fatalf("status = %s", st)
	}

	// nothing new: nothing to send; a newer change can be sent by the owner straight away
	kiki.post(fmt.Sprintf("/catalogs/%d/review", kc), nil).status(http.StatusBadRequest).contains("Tidak ada perubahan")
	save("Open the file", "Buka berkas")
	kiki.post(fmt.Sprintf("/catalogs/%d/push", kc), nil).status(http.StatusForbidden)
	owner.post(fmt.Sprintf("/catalogs/%d/push", kc), nil).redirectsTo(base + "/files")
	if n := len(ghFake.LastCommits()); n != 2 {
		t.Fatalf("commits after push = %d", n)
	}
	if got := string(ghFake.File(repo, "3.14", "library/os.po")); !strings.Contains(got, `msgstr "Buka berkas"`) || !strings.Contains(got, `msgstr "Tutup jendelanya"`) {
		t.Fatalf("branch file:\n%s", got)
	}
}
