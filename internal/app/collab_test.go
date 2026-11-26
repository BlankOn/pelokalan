package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func projectID(t *testing.T, slug string) int64 {
	return scalar[int64](t, `SELECT id FROM projects WHERE slug = $1`, slug)
}

func role(t *testing.T, slug string, u *user) string {
	var r string
	_ = env.DB.QueryRow(`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID(t, slug), u.ID).Scan(&r)
	return r
}

// Email invitations, invite links, open projects, roles and the public members API
func TestMembership(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "owner")
	slug := owner.newProject("Teks")
	base := "/projects/" + slug

	// email invitation, accepted after signing in with that address
	cici := signUp(t, "cici")
	owner.post(base+"/members/invite", url.Values{"email": {cici.Email}, "role": {"manager"}}).redirectsTo(base)
	mails.waitFor(t, cici.Email, "Teks")
	inv := scalar[int64](t, `SELECT id FROM project_invitations WHERE email = $1`, cici.Email)
	cici.get("/invitations").status(http.StatusOK).contains("Teks")
	cici.post("/invitations/"+strconv.FormatInt(inv, 10)+"/accept", nil).redirectsTo("/projects")
	if r := role(t, slug, cici); r != "manager" {
		t.Fatalf("cici's role = %q, want manager", r)
	}

	// private by default: joining is refused; an invite link works
	dodi := signUp(t, "dodi")
	dodi.post(base+"/join", url.Values{"next": {base + "/files"}})
	if r := role(t, slug, dodi); r != "" {
		t.Fatalf("joined a private project as %q", r)
	}
	cici.post(base+"/invite-links", url.Values{"role": {"translator"}, "days": {"7"}, "max_uses": {"1"}}).redirectsTo(base)
	token := scalar[string](t, `SELECT token FROM project_invite_links WHERE project_id = $1`, projectID(t, slug))
	visitor(t).get("/join/" + token).status(http.StatusOK).contains("/login?next=%2fjoin%2f" + token)
	dodi.get("/join/" + token).status(http.StatusOK)
	dodi.post("/join/"+token, nil).redirectsTo(base)
	if r := role(t, slug, dodi); r != "translator" {
		t.Fatalf("dodi's role = %q, want translator", r)
	}
	// the link had one use
	eko := signUp(t, "eko")
	eko.post("/join/"+token, nil)
	if r := role(t, slug, eko); r != "" {
		t.Fatalf("a used-up invite link let eko in as %q", r)
	}

	// an open project lets anyone signed in join as translator
	owner.post(base+"/settings", url.Values{"name": {"Teks"}, "slug": {slug}, "source_language": {"en"}, "access": {"open"}}).redirectsTo(base)
	eko.post(base+"/join", url.Values{"next": {base + "/files"}}).redirectsTo(base + "/files")
	if r := role(t, slug, eko); r != "translator" {
		t.Fatalf("eko's role after joining = %q", r)
	}

	// translators see Settings (notifications) but cannot change the project; managers can change roles
	eko.get(base + "/settings").status(http.StatusOK).lacks(`name="slug"`)
	eko.post(base+"/settings", url.Values{"name": {"Diambil alih"}, "slug": {slug}, "access": {"open"}})
	if n := scalar[string](t, `SELECT name FROM projects WHERE slug = $1`, slug); n != "Teks" {
		t.Fatalf("a translator renamed the project to %q", n)
	}
	cici.post(base+"/members/"+strconv.FormatInt(dodi.ID, 10)+"/role", url.Values{"role": {"manager"}}).redirectsTo(base)
	if r := role(t, slug, dodi); r != "manager" {
		t.Fatalf("role change: %q", r)
	}

	// the members page is public, emails only for members
	visitor(t).get(base + "/members").status(http.StatusOK).contains("@" + cici.Username).lacks(cici.Email)
	eko.get(base + "/members").status(http.StatusOK).contains(cici.Email)

	// public JSON API
	api := visitor(t).get("/api/projects/" + slug + "/members").status(http.StatusOK)
	if api.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("API lacks CORS header")
	}
	var out struct {
		Project struct {
			Slug string `json:"slug"`
		} `json:"project"`
		Members []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"members"`
	}
	if err := json.Unmarshal([]byte(api.Body), &out); err != nil {
		t.Fatalf("API JSON: %v\n%s", err, api.Body)
	}
	if out.Project.Slug != slug || len(out.Members) != 4 {
		t.Fatalf("API answer: %+v", out)
	}
}

// Topics, comments with @mentions (emailed), edits and soft deletes
func TestDiscussions(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "fani")
	slug := owner.newProject("Diskusi")
	base := "/projects/" + slug
	gita := signUp(t, "gita")
	owner.post(base+"/members/invite", url.Values{"email": {gita.Email}, "role": {"translator"}}).redirectsTo(base)
	inv := scalar[int64](t, `SELECT id FROM project_invitations WHERE email = $1`, gita.Email)
	gita.post("/invitations/"+strconv.FormatInt(inv, 10)+"/accept", nil).redirectsTo("/projects")

	loc := owner.post(base+"/topics/new", url.Values{"title": {"Istilah file"}, "body": {"Pakai **berkas**, @" + gita.Username + "?"}}).
		redirectsTo(base + "/topics/")
	key := loc[strings.LastIndex(loc, "/")+1:]
	if i := strings.IndexAny(key, "?#"); i >= 0 {
		key = key[:i]
	}
	visitor(t).get(loc).status(http.StatusOK).contains("Istilah file", "<strong>berkas</strong>")
	mails.waitFor(t, gita.Email, "Istilah file")

	// a reply; the starter is emailed
	gita.hx(base+"/discussions/topic/"+key+"/comments", url.Values{"body": {"Setuju."}}).status(http.StatusOK).contains("Setuju.")
	mails.waitFor(t, owner.Email, "Setuju.")
	cid := scalar[int64](t, `SELECT c.id FROM comments c JOIN discussions d ON d.id = c.discussion_id WHERE d.subject_key = $1 AND c.body = 'Setuju.'`, key)

	// only the author edits; the owner may delete (soft)
	owner.post(base+"/comments/"+strconv.FormatInt(cid, 10)+"/edit", url.Values{"body": {"Diubah pemilik"}})
	if b := scalar[string](t, `SELECT body FROM comments WHERE id = $1`, cid); b != "Setuju." {
		t.Fatalf("the owner edited someone else's comment: %q", b)
	}
	gita.post(base+"/comments/"+strconv.FormatInt(cid, 10)+"/edit", url.Values{"body": {"Setuju sekali."}})
	if b := scalar[string](t, `SELECT body FROM comments WHERE id = $1`, cid); b != "Setuju sekali." {
		t.Fatalf("author's edit: %q", b)
	}
	// a coordinator cannot remove someone else's comment: only its author or the project owner
	koor := signUp(t, "koor")
	owner.post(base+"/members/invite", url.Values{"email": {koor.Email}, "role": {"manager"}}).redirectsTo(base)
	kinv := scalar[int64](t, `SELECT id FROM project_invitations WHERE email = $1`, koor.Email)
	koor.post("/invitations/"+strconv.FormatInt(kinv, 10)+"/accept", nil).redirectsTo("/projects")
	koor.post(base+"/comments/"+strconv.FormatInt(cid, 10)+"/delete", nil)
	if scalar[bool](t, `SELECT deleted_at IS NOT NULL FROM comments WHERE id = $1`, cid) {
		t.Fatalf("a coordinator deleted someone else's comment")
	}
	owner.post(base+"/comments/"+strconv.FormatInt(cid, 10)+"/delete", nil)
	if !scalar[bool](t, `SELECT deleted_at IS NOT NULL FROM comments WHERE id = $1`, cid) {
		t.Fatalf("comment not soft-deleted")
	}

	// the Diskusi tab finds the topic by a word in a comment
	visitor(t).get(base + "/discussions?q=berkas").status(http.StatusOK).contains("Istilah file")
	visitor(t).get(base + "/discussions?q=tidakada").status(http.StatusOK).lacks("Istilah file")

	// visitors cannot comment
	r := visitor(t).hx(base+"/discussions/topic/"+key+"/comments", url.Values{"body": {"spam"}})
	if r.Status == http.StatusOK {
		t.Fatalf("a visitor could comment")
	}
}

// Wiki pages: create, edit, refuse a save over someone else's newer version
func TestWiki(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "hana")
	slug := owner.newProject("Wiki")
	base := "/projects/" + slug
	owner.post(base+"/wiki/new", url.Values{"title": {"Panduan Gaya"}, "body": {"# Halo\n\nGunakan *berkas*."}}).
		redirectsTo(base + "/wiki/panduan-gaya")
	visitor(t).get(base + "/wiki/panduan-gaya").status(http.StatusOK).contains("<em>berkas</em>")

	edit := owner.get(base + "/wiki/panduan-gaya/edit").status(http.StatusOK)
	v := formValue(t, edit.Body, "version")
	owner.post(base+"/wiki/panduan-gaya/edit", url.Values{"title": {"Panduan Gaya"}, "slug": {"panduan-gaya"}, "body": {"Versi dua"}, "version": {v}}).
		redirectsTo(base + "/wiki/panduan-gaya")
	// the same old version again: refused, nothing overwritten
	owner.post(base+"/wiki/panduan-gaya/edit", url.Values{"title": {"Panduan Gaya"}, "slug": {"panduan-gaya"}, "body": {"Versi lama"}, "version": {v}}).
		status(http.StatusConflict)
	if b := scalar[string](t, `SELECT body FROM wiki_pages WHERE slug = 'panduan-gaya'`); b != "Versi dua" {
		t.Fatalf("wiki body = %q", b)
	}
	if n := scalar[int](t, `SELECT count(*) FROM wiki_edits e JOIN wiki_pages p ON p.id = e.page_id WHERE p.slug = 'panduan-gaya'`); n != 2 {
		t.Fatalf("wiki edits = %d, want 2", n)
	}
	// non-members cannot create pages
	stranger := signUp(t, "iwan")
	r := stranger.post(base+"/wiki/new", url.Values{"title": {"Spam"}, "body": {"x"}})
	if r.Status == http.StatusSeeOther {
		t.Fatalf("a non-member created a wiki page")
	}
}

// Konteks items that belong to no module: members create and edit them, a stale save is refused and keeps the text
func TestGeneralContexts(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "kirana")
	slug := owner.newProject("Konteks Umum")
	base := "/projects/" + slug
	visitor(t).get(base + "/contexts").status(http.StatusOK).lacks("Konteks baru")
	owner.get(base + "/contexts").status(http.StatusOK).contains("Konteks baru")

	// a title is required
	owner.post(base+"/contexts", url.Values{"title": {"  "}, "note": {"Catatan tanpa judul"}}).
		status(http.StatusOK).contains("Judul wajib diisi").contains("Catatan tanpa judul")
	r := owner.post(base+"/contexts", url.Values{"title": {"Gaya bahasa"}, "note": {"Pakai kalimat aktif dan sapaan Anda."}})
	r.status(http.StatusSeeOther)
	key := scalar[string](t, `SELECT substr(x.module, 2) FROM module_contexts x JOIN projects p ON p.id = x.project_id WHERE p.slug = $1 AND x.module LIKE '~%'`, slug)
	if !strings.Contains(r.Header.Get("Location"), "#ctx-"+key) {
		t.Fatalf("redirect = %q", r.Header.Get("Location"))
	}
	visitor(t).get(base + "/contexts").status(http.StatusOK).contains("Gaya bahasa").contains("Pakai kalimat aktif")
	v := formValue(t, owner.get(base+"/contexts").Body, "version")

	owner.post(base+"/contexts/"+key, url.Values{"title": {"Gaya bahasa"}, "note": {"Versi dua"}, "version": {v}}).status(http.StatusSeeOther)
	// the same old version again: refused, nothing overwritten, the text is kept in the form
	owner.post(base+"/contexts/"+key, url.Values{"title": {"Gaya bahasa"}, "note": {"Versi lama saya"}, "version": {v}}).
		status(http.StatusConflict).contains("Versi lama saya").contains("Versi dua")
	if n := scalar[string](t, `SELECT note FROM module_contexts WHERE module = $1`, "~"+key); n != "Versi dua" {
		t.Fatalf("note = %q", n)
	}
	// non-members cannot add or change items; module notes cannot be changed through this route
	stranger := signUp(t, "lukas")
	if stranger.post(base+"/contexts", url.Values{"title": {"Spam"}, "note": {"x"}}).Status == http.StatusSeeOther {
		t.Fatalf("a non-member created a context")
	}
	stranger.post(base+"/contexts/"+key, url.Values{"title": {"Spam"}, "note": {"x"}, "version": {"2"}}).lacks("Context saved")
	if n := scalar[string](t, `SELECT note FROM module_contexts WHERE module = $1`, "~"+key); n != "Versi dua" {
		t.Fatalf("note after stranger = %q", n)
	}
	// emptying the note hides the item
	v = formValue(t, owner.get(base+"/contexts").Body, "version")
	owner.post(base+"/contexts/"+key, url.Values{"title": {"Gaya bahasa"}, "note": {""}, "version": {v}}).status(http.StatusSeeOther)
	owner.get(base + "/contexts").status(http.StatusOK).lacks("Gaya bahasa")
}

// formValue reads the value of a named input from a page
func formValue(t *testing.T, body, name string) string {
	t.Helper()
	i := strings.Index(body, `name="`+name+`"`)
	if i < 0 {
		t.Fatalf("no input %q on the page", name)
	}
	tag := body[strings.LastIndex(body[:i], "<"):]
	tag = tag[:strings.Index(tag, ">")]
	j := strings.Index(tag, `value="`)
	if j < 0 {
		t.Fatalf("input %q has no value", name)
	}
	v := tag[j+7:]
	return v[:strings.Index(v, `"`)]
}

// The profile page saves everything at once (only when every field is valid); the description and links
// show on the public page
func TestProfilePage(t *testing.T) {
	skipWithoutDB(t)
	u := signUp(t, "prof")
	form := url.Values{"username": {u.Username}, "name": {"Prof Baru"}, "lang": {"id"},
		"translator_name": {"Prof Penerjemah"}, "translator_email": {""}, "language_team": {""},
		"bio": {"Penerjemah GNOME."}, "link_website": {"blog.example.org"}, "link_github": {"@prof-gh"},
		"link_x": {"https://twitter.com/prof_x"}, "link_linkedin": {"linkedin.com/in/prof-li"},
		"link_mastodon": {"@prof@mastodon.social"}, "badge_emails": {"on"}}
	u.get("/profile").status(http.StatusOK).contains(`id="profile-form"`, `name="link_github"`)
	u.post("/profile", form).redirectsTo("/profile")
	page := visitor(t).get("/users/" + u.Username).status(http.StatusOK)
	page.contains("Penerjemah GNOME.", "https://blog.example.org", "https://github.com/prof-gh", "https://x.com/prof_x",
		"https://www.linkedin.com/in/prof-li", "https://mastodon.social/@prof")
	if n := scalar[string](t, `SELECT translator_name FROM users WHERE id = $1`, u.ID); n != "Prof Penerjemah" {
		t.Fatalf("translator name = %q", n)
	}

	// one bad link: nothing is saved
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("name", "Tidak Tersimpan")
	bad.Set("link_github", "https://gitlab.com/someone")
	u.post("/profile", bad).status(http.StatusOK).contains("GitHub")
	if n := scalar[string](t, `SELECT name FROM users WHERE id = $1`, u.ID); n != "Prof Baru" {
		t.Fatalf("an invalid form saved the name: %q", n)
	}
	// badge emails off through the same form
	form.Del("badge_emails")
	u.post("/profile", form).redirectsTo("/profile")
	if !scalar[bool](t, `SELECT badge_emails_off FROM users WHERE id = $1`, u.ID) {
		t.Fatal("badge emails still on")
	}
}
