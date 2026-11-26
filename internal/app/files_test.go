package app_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// targz packs files (name → content) into a .tar.gz archive with a top folder, like a release download
func targz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "ui/" + name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// addMember invites u by email with a role and accepts the invitation
func addMember(t *testing.T, owner *user, slug string, u *user, role string) {
	t.Helper()
	base := "/projects/" + slug
	owner.post(base+"/members/invite", url.Values{"email": {u.Email}, "role": {role}}).redirectsTo(base)
	inv := scalar[int64](t, `SELECT id FROM project_invitations WHERE email = $1 AND project_id = $2`, u.Email, projectID(t, slug))
	u.post("/invitations/"+strconv.FormatInt(inv, 10)+"/accept", nil).redirectsTo("/projects")
}

// Module group archive → glossary terms and project files → members take files; module reservations
func TestModuleGroupsAndReservations(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "joko")
	slug := owner.newProject("Gnome")
	base := "/projects/" + slug
	pid := projectID(t, slug)
	kiki := signUp(t, "kiki")
	addMember(t, owner, slug, kiki, "translator")

	archive := targz(t, map[string][]byte{
		"calc.main.id.po": poFile("calc", "Open File", "Buka Berkas", "Close", "Tutup", "Add", ""),
		"edit.main.id.po": poFile("edit", "Open File", "Buka Berkas", "Undo", "Urungkan"),
	})
	owner.upload(base+"/module-groups/sources", url.Values{"source": {"file"}, "name": {"gnome"}, "version_name": {"51"}, "language": {"id"}},
		"ui.tar.gz", archive).redirectsTo(base + "/settings?")
	if n := scalar[int](t, `SELECT count(*) FROM project_files WHERE project_id = $1`, pid); n != 2 {
		t.Fatalf("project files = %d, want 2", n)
	}

	// the glossary is public and groups "Open File" from both files
	visitor(t).get(base+"/glossary?q=open").status(http.StatusOK).contains("Open File", "Buka Berkas")
	visitor(t).get(base+"/files").status(http.StatusOK).contains("calc.main.id.po", "edit.main.id.po")

	// a project file opens read-only ("Lihat"); glossary module links open it at the term
	calcID := scalar[int64](t, `SELECT id FROM project_files WHERE project_id = $1 AND filename = 'calc.main.id.po'`, pid)
	view := fmt.Sprintf("%s/project-files/%d/view", base, calcID)
	visitor(t).get(base + "/files").status(http.StatusOK).contains(view)
	visitor(t).get(view).status(http.StatusOK).contains("view-mode", "Buka Berkas", "Close", "Hanya lihat").lacks("/take")
	visitor(t).get(view + "?q=tutup").status(http.StatusOK).contains("Close").lacks("Open File")
	kiki.get(view).status(http.StatusOK).contains("/project-files/"+strconv.FormatInt(calcID, 10)+"/take", "Ambil")
	visitor(t).get(base + "/project-files/module/calc.main?q=Open").redirectsTo(view + "?q=Open")
	visitor(t).get(base + "/glossary?q=open").status(http.StatusOK).contains("/project-files/module/calc.main?q=Open")

	// turn reservations on (owner)
	owner.post(base+"/settings", url.Values{"name": {"Gnome"}, "slug": {slug}, "source_language": {"en"}, "access": {"private"},
		"reservations": {"on"}, "reservation_days": {"30"}}).redirectsTo(base)
	if !scalar[bool](t, `SELECT reservations FROM projects WHERE id = $1`, pid) {
		t.Fatalf("reservations not switched on")
	}

	// "Reservasi modul" asks first (the UI is in Indonesian by default), saying that it reserves the module and how it is released
	kiki.get(base+"/files").status(http.StatusOK).contains(`data-confirm-ok="Reservasi modul"`, "Reservasi modul calc untuk dikerjakan?", "setelah 30 hari tanpa simpanan")

	// with reservations on, the read-only view offers "Reservasi modul" while the module is free
	kiki.get(view).status(http.StatusOK).contains(`data-confirm-ok="Reservasi modul"`)

	// kiki takes calc: a copy in her files, and the module is hers
	calc := scalar[int64](t, `SELECT id FROM project_files WHERE project_id = $1 AND filename = 'calc.main.id.po'`, pid)
	kiki.post(base+"/project-files/"+strconv.FormatInt(calc, 10)+"/take", nil).redirectsTo(base + "/" + kiki.Username + "/po/calc.main.id.po")
	if h := scalar[int64](t, `SELECT user_id FROM module_reservations WHERE project_id = $1 AND module = 'calc' AND released_at IS NULL`, pid); h != kiki.ID {
		t.Fatalf("calc held by %d, want kiki", h)
	}

	// sorting the project files (calc has 3 strings, edit 2; calc is reserved)
	order := func(sort string) (calcFirst bool) {
		body := owner.get(base + "/project-files?s=" + sort).status(http.StatusOK).Body
		c, e := strings.Index(body, "calc.main.id.po"), strings.Index(body, "edit.main.id.po")
		if c < 0 || e < 0 {
			t.Fatalf("sort %s: a file is missing", sort)
		}
		return c < e
	}
	for sort, want := range map[string]bool{"most": true, "fewest": false, "name": true, "reserved": true, "free": false,
		"complete": false, "incomplete": true} {
		if order(sort) != want {
			t.Errorf("sort %s: calc first = %v, want %v", sort, !want, want)
		}
	}
	owner.get(base+"/files").status(http.StatusOK).contains(`name="s"`, "Direservasi dulu", "String terbanyak")

	// the owner (a manager) gets a Lepas button where kiki's reservation shows; kiki just sees who holds it
	owner.get(base+"/files").status(http.StatusOK).contains("res-release", "Lepas reservasi @"+kiki.Username)
	owner.get(base + "/member-files").status(http.StatusOK).contains("res-release")
	kiki.get(base + "/files").status(http.StatusOK).lacks("res-release")

	// once held, the view shows who holds it; the holder gets "Buka berkas Anda"
	owner.get(view).status(http.StatusOK).contains("Direservasi oleh @" + kiki.Username).lacks("/take")
	kiki.get(view).status(http.StatusOK).contains("Buka berkas Anda")

	// the owner cannot take it while kiki holds it, and sees who does
	owner.post(base+"/project-files/"+strconv.FormatInt(calc, 10)+"/take", nil).status(http.StatusOK).contains("@" + kiki.Username)
	if n := scalar[int](t, `SELECT count(*) FROM catalogs WHERE project_id = $1 AND created_by = $2`, pid, owner.ID); n != 0 {
		t.Fatalf("the owner got a copy of a held module")
	}

	// kiki translates; saving works for the holder
	kc := scalar[int64](t, `SELECT id FROM catalogs WHERE project_id = $1 AND created_by = $2`, pid, kiki.ID)
	eid, ver := entry(t, kc, "Add")
	kiki.post("/catalogs/"+strconv.FormatInt(kc, 10)+"/entries/"+strconv.FormatInt(eid, 10), url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {"Tambah"}}).status(http.StatusOK)
	if msgstr(t, eid) != "Tambah" {
		t.Fatalf("holder's save failed")
	}

	// the owner (a manager) releases it; kiki is emailed, then the owner may take it
	owner.post(base+"/reservations/release", url.Values{"module": {"calc"}}).redirectsTo(base)
	mails.waitFor(t, kiki.Email, "calc")
	owner.post(base+"/project-files/"+strconv.FormatInt(calc, 10)+"/take", nil).redirectsTo(base + "/" + owner.Username + "/po/calc.main.id.po")

	// now kiki's copy is locked: her save is refused and the editor opens read-only
	_, ver = entry(t, kc, "Add")
	kiki.post("/catalogs/"+strconv.FormatInt(kc, 10)+"/entries/"+strconv.FormatInt(eid, 10), url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {"Tambahkan"}})
	if msgstr(t, eid) != "Tambah" {
		t.Fatalf("a save went through on a module someone else holds")
	}
	kiki.get(base + "/" + kiki.Username + "/po/calc.main.id.po").status(http.StatusOK).contains("editor-hold")

	// in Berkas Anda the holder's own reservation has a blue lock and the release item
	owner.get(base+"/my-files").status(http.StatusOK).contains("Lepas reservasi tugas", "Direservasi oleh Anda", `res-lock-wrap res-lock-mine`)
	// kiki's copy of the module the owner holds has the red lock and "Lihat"
	kiki.get(base + "/my-files").status(http.StatusOK).contains(`class="res-lock-wrap" title="Direservasi oleh @` + owner.Username + `"`).lacks("res-lock-mine")

	// the red lock after the file name names the holder
	visitor(t).get(base + "/files").status(http.StatusOK).contains(`class="res-lock-wrap" title="Direservasi oleh @` + owner.Username + `"`)

	// Settings lists the held module for managers
	owner.get(base + "/settings").status(http.StatusOK).contains("Reservasi modul untuk dikerjakan").lacks("held-modules")

	// the reservation filter on Berkas proyek
	visitor(t).get(base + "/project-files?rv=free").status(http.StatusOK).contains("edit.main.id.po").lacks("calc.main.id.po")

	// the owner releases calc; their copy then offers "Reservasi & Terjemahkan", which asks first,
	// reserves the module and opens the editor
	owner.post(base+"/reservations/release", url.Values{"module": {"calc"}}).redirectsTo(base)
	mine := owner.get(base + "/my-files").status(http.StatusOK)
	mine.contains("Reservasi &amp; Terjemahkan", "Reservasi modul calc untuk dikerjakan?").lacks("res-lock-wrap")
	editor := base + "/" + owner.Username + "/po/calc.main.id.po"
	owner.post(base+"/reservations", url.Values{"module": {"calc"}, "next": {editor}}).redirectsTo(editor)
	if h := scalar[int64](t, `SELECT user_id FROM module_reservations WHERE project_id = $1 AND module = 'calc' AND released_at IS NULL`, pid); h != owner.ID {
		t.Fatalf("calc held by %d after Reservasi & Terjemahkan, want the owner", h)
	}
	owner.get(base + "/my-files").status(http.StatusOK).lacks("Reservasi &amp; Terjemahkan").contains("res-lock-mine")
}

// Deleting a PO file is a soft delete: it leaves the lists but opens read-only
func TestSoftDelete(t *testing.T) {
	skipWithoutDB(t)
	lia := signUp(t, "lia")
	slug := lia.newProject("Hapus")
	base := "/projects/" + slug
	lia.upload(base+"/upload", url.Values{"language": {"id"}}, "hapus.main.id.po", poFile("hapus", "Yes", "Ya")).
		redirectsTo(base + "/" + lia.Username + "/po/hapus.main.id.po")
	cid := scalar[int64](t, `SELECT id FROM catalogs WHERE created_by = $1`, lia.ID)
	c := "/catalogs/" + strconv.FormatInt(cid, 10)

	lia.post(c+"/delete", nil).redirectsTo(base)
	if !scalar[bool](t, `SELECT deleted_at IS NOT NULL FROM catalogs WHERE id = $1`, cid) {
		t.Fatalf("not soft-deleted")
	}
	lia.get(base + "/my-files").status(http.StatusOK).lacks("hapus.main.id.po")
	lia.get(c).status(http.StatusOK).contains("Ya")

	// no more changes
	eid, ver := entry(t, cid, "Yes")
	lia.post(c+"/entries/"+strconv.FormatInt(eid, 10), url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {"Iya"}})
	if msgstr(t, eid) != "Ya" {
		t.Fatalf("a deleted file was changed")
	}
	// the same name can be uploaded again
	lia.upload(base+"/upload", url.Values{"language": {"id"}}, "hapus.main.id.po", poFile("hapus", "Yes", "")).
		redirectsTo(base + "/" + lia.Username + "/po/hapus.main.id.po")
}

// Public profile with the person's work, and the badge share page with its image
func TestProfileAndBadges(t *testing.T) {
	skipWithoutDB(t)
	mira := signUp(t, "mira")
	slug := mira.newProject("Profil")
	base := "/projects/" + slug
	mira.upload(base+"/upload", url.Values{"language": {"id"}}, "profil.main.id.po", poFile("profil", "One", "", "Two", "")).
		redirectsTo(base + "/")
	cid := scalar[int64](t, `SELECT id FROM catalogs WHERE created_by = $1`, mira.ID)
	for _, pair := range [][2]string{{"One", "Satu"}, {"Two", "Dua"}} {
		eid, ver := entry(t, cid, pair[0])
		mira.post("/catalogs/"+strconv.FormatInt(cid, 10)+"/entries/"+strconv.FormatInt(eid, 10),
			url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {pair[1]}}).status(http.StatusOK)
	}

	// improving a translated string is work too: its words count ("Kata dikerjakan" = every string worked on);
	// it was an AI draft, so the badge details mark the module as AI-assisted
	if _, err := env.DB.Exec(`UPDATE entries SET origin = 'ai' WHERE catalog_id = $1 AND msgid = 'One'`, cid); err != nil {
		t.Fatal(err)
	}
	eid, ver := entry(t, cid, "One")
	mira.post("/catalogs/"+strconv.FormatInt(cid, 10)+"/entries/"+strconv.FormatInt(eid, 10),
		url.Values{"version": {strconv.Itoa(ver)}, "msgstr0": {"Satu saja"}}).status(http.StatusOK)

	page := visitor(t).get("/users/" + mira.Username).status(http.StatusOK)
	page.contains("@"+mira.Username, "Profil", "<strong>3</strong><span>Kata dikerjakan</span>",
		"<strong>2</strong><span>String diterjemahkan dari kosong</span>").lacks(mira.Email)
	// two strings typed by hand, one AI draft reviewed: progress towards "Racikan Tangan" and "Mitra AI"
	for name, progress := range map[string]string{"Racikan Tangan": "2 / 50", "Mitra AI": "1 / 25"} {
		i := strings.Index(page.Body, name)
		if i < 0 || !strings.Contains(page.Body[i:min(len(page.Body), i+1500)], progress) {
			t.Errorf("badge %q does not show %s", name, progress)
		}
	}
	visitor(t).get("/users/nobody-here").status(http.StatusNotFound)

	// a fully translated file earns the bronze "completed" badge: share page and PNG
	share := visitor(t).get("/users/" + mira.Username + "/badges/completed/1").status(http.StatusOK)
	share.contains(`property="og:image"`, "Dibantu AI")
	img := visitor(t).get("/users/" + mira.Username + "/badges/completed/1.png").status(http.StatusOK)
	if img.Header.Get("Content-Type") != "image/png" || len(img.Body) < 1000 {
		t.Fatalf("badge image: %s, %d bytes", img.Header.Get("Content-Type"), len(img.Body))
	}
	// a tier not earned goes back to the profile
	visitor(t).get("/users/" + mira.Username + "/badges/completed/2").redirectsTo("/users/" + mira.Username)

	// badge emails: one message for new tiers, never twice
	if _, err := env.Activity.AwardBadges(t.Context(), time.UTC, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	n := len(mails.to(mira.Email))
	if _, err := env.Activity.AwardBadges(t.Context(), time.UTC, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if len(mails.to(mira.Email)) != n {
		t.Fatalf("badge email sent again")
	}
	if scalar[int](t, `SELECT count(*) FROM badge_awards WHERE user_id = $1`, mira.ID) == 0 {
		t.Fatalf("no badge recorded")
	}
}
