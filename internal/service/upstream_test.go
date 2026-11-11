package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"pelokalan/internal/gnomel10n"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// upstreamRepo keeps versions, project files and stored upstream states in memory;
// other calls are not used by these tests
type upstreamRepo struct {
	repository.GlossaryRepository
	versions []*models.ProjectVersion
	files    []*models.ProjectFile
	states   map[string]*models.UpstreamRecord // release/lang/stem
}

func (r *upstreamRepo) ListVersions(context.Context, int64) ([]*models.ProjectVersion, error) {
	return r.versions, nil
}

func (r *upstreamRepo) ListProjectFiles(_ context.Context, _ int64, _ string, versionIDs []int64, limit, offset int) ([]*models.ProjectFile, int, error) {
	var out []*models.ProjectFile
	for _, f := range r.files {
		ok := len(versionIDs) == 0
		for _, v := range versionIDs {
			ok = ok || v == f.VersionID
		}
		if ok {
			cp := *f
			out = append(out, &cp)
		}
	}
	total := len(out)
	offset = min(offset, total)
	return out[offset:min(offset+limit, total)], total, nil
}

func (r *upstreamRepo) ProjectFileNames(context.Context, int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	for _, f := range r.files {
		out[f.VersionID] = append(out[f.VersionID], f.Filename)
	}
	return out, nil
}

func (r *upstreamRepo) UpstreamCandidates(context.Context) ([]models.UpstreamCandidate, error) {
	var out []models.UpstreamCandidate
	for _, f := range r.files {
		for _, v := range r.versions {
			if v.ID == f.VersionID {
				out = append(out, models.UpstreamCandidate{Filename: f.Filename, FileLanguage: f.Language,
					ProjectLanguage: "id", VersionName: v.Name, Version: v.Version, GlossaryURL: v.GlossaryURL})
			}
		}
	}
	return out, nil
}

func (r *upstreamRepo) UpstreamStates(_ context.Context, release, lang string) (map[string]*models.UpstreamRecord, error) {
	out := map[string]*models.UpstreamRecord{}
	for _, rec := range r.states {
		if rec.Release == release && rec.Language == lang {
			cp := *rec
			out[rec.Stem] = &cp
		}
	}
	return out, nil
}

func (r *upstreamRepo) SaveUpstreamState(_ context.Context, rec *models.UpstreamRecord) error {
	cp := *rec
	r.states[rec.Release+"/"+rec.Language+"/"+rec.Stem] = &cp
	return nil
}

// upstreamSite serves the API answers recorded in internal/gnomel10n/testdata
func upstreamSite(t *testing.T) (*httptest.Server, *int) {
	apiCalls := new(int)
	files := map[string]string{
		"/api/v1/releases/":                      "releases.json",
		"/api/v1/releases/gnome-51/languages/id": "release_gnome-51_id.json",
		"/api/v1/modules/gnome-maps/branches/gnome-51/domains/po/languages/id": "stats_gnome-maps_po_id.json",
		"/api/v1/modules/glib/branches/glib-2-90/domains/po/languages/id":      "stats_glib_po_id.json",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/POT/") {
			_, _ = w.Write([]byte("msgid \"\"\nmsgstr \"\"\n\"Language: id\\n\"\n"))
			return
		}
		*apiCalls++
		name, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile("../gnomel10n/testdata/" + name)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, apiCalls
}

func TestUpstreamWarmAndFilter(t *testing.T) {
	srv, calls := upstreamSite(t)
	ctx := context.Background()
	repo := &upstreamRepo{states: map[string]*models.UpstreamRecord{}, versions: []*models.ProjectVersion{
		{ID: 1, Name: "gnome", Version: "51", GlossaryURL: srv.URL + "/languages/id/gnome-51/ui.tar.gz"},
		{ID: 2, Name: "gnome", Version: "51"}, // no sync link: the label names a release
		{ID: 3, Name: "kde", Version: "6"},    // not a release upstream
	}, files: []*models.ProjectFile{
		{ID: 11, VersionID: 1, Filename: "gnome-maps.gnome-51.id.po", Language: "id"},
		{ID: 12, VersionID: 2, Filename: "glib.glib-2-90.id.po", Language: "id"},
		{ID: 13, VersionID: 3, Filename: "konsole.id.po", Language: "id"},
		{ID: 14, VersionID: 1, Filename: "gone.main.id.po", Language: "id"},
	}}
	s := NewGlossaryService(repo, nil)
	client := gnomel10n.New(srv.URL, true, "test")
	s.SetUpstream(client)
	p := &models.Project{ID: 1, Language: "id"}
	classes := func(states map[string]bool) map[int64]string {
		files, _, err := s.ProjectFiles(ctx, p, "", nil, states, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]string{}
		for _, f := range files {
			out[f.ID] = f.UpstreamClass()
		}
		return out
	}

	// pages never ask the site for states: before warming everything is unknown
	if got := classes(nil); got[11] != "unknown" || got[12] != "unknown" || got[13] != "unknown" {
		t.Errorf("before warming: %v", got)
	}
	before := *calls
	if !s.HasUpstream(ctx, p) || *calls != before {
		t.Errorf("HasUpstream: requests %d", *calls-before)
	}

	s.WarmUpstream(ctx)
	got := classes(nil)
	want := map[int64]string{11: "none", 12: "none", 13: "unknown", 14: "missing"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("file %d: %q, want %q", id, got[id], w)
		}
	}
	files, total, _ := s.ProjectFiles(ctx, p, "", nil, map[string]bool{"missing": true, "unknown": true}, 0)
	if total != 2 || len(files) != 2 {
		t.Errorf("filter missing+unknown: %d files", total)
	}
	if _, total, _ := s.ProjectFiles(ctx, p, "", []int64{1}, map[string]bool{"none": true}, 0); total != 1 {
		t.Errorf("filter none in gnome-51 (sync link): %d", total)
	}
	maps := repo.states["gnome-51/id/gnome-maps.gnome-51"]
	if maps == nil || !maps.Listed || maps.Translated != 535 || maps.POFile == "" {
		t.Errorf("stored gnome-maps = %+v", maps)
	}

	// fresh states are not asked again
	before = *calls
	s.WarmUpstream(ctx)
	if *calls != before {
		t.Errorf("second round sent %d requests", *calls-before)
	}

	// members' files: same name, else same module
	cats := []*models.Catalog{
		{ID: 1, Filename: "glib.glib-2-90.id.po", Language: "id"},
		{ID: 2, Filename: "gnome-maps.id.po", Language: "id"},
		{ID: 3, Filename: "other.id.po", Language: "id"},
		{ID: 4, Filename: "gnome-maps.pot", IsTemplate: true},
	}
	s.AttachUpstreamToCatalogs(ctx, p, cats)
	if cats[0].UpstreamClass() != "none" || cats[1].Upstream == nil || cats[1].Upstream.Translated != 535 ||
		cats[2].UpstreamClass() != "unknown" || cats[3].Upstream != nil {
		t.Errorf("catalogs: %+v %+v %+v", cats[0].UpstreamInfo, cats[1].UpstreamInfo, cats[2].UpstreamInfo)
	}

	// "Ambil" uses the stored po_file: no API request
	before = *calls
	data, label, err := s.UpstreamPO(ctx, p, repo.files[0], 1<<20)
	if err != nil || !strings.Contains(string(data), "Language: id") || !strings.HasSuffix(label, " gnome-maps/gnome-51") || *calls != before {
		t.Errorf("UpstreamPO = %q %q %v (requests %d)", data, label, err, *calls-before)
	}
	if _, _, err := s.UpstreamPO(ctx, p, repo.files[3], 1<<20); err == nil {
		t.Error("UpstreamPO of a file the release does not list")
	}
}

// The warmer leaves a reserve of the hourly budget and continues in the next round
func TestUpstreamWarmBudget(t *testing.T) {
	srv, calls := upstreamSite(t)
	ctx := context.Background()
	repo := &upstreamRepo{states: map[string]*models.UpstreamRecord{}, versions: []*models.ProjectVersion{
		{ID: 1, Name: "gnome", Version: "51", GlossaryURL: srv.URL + "/languages/id/gnome-51/ui.tar.gz"},
	}, files: []*models.ProjectFile{
		{ID: 11, VersionID: 1, Filename: "gnome-maps.gnome-51.id.po", Language: "id"},
		{ID: 12, VersionID: 1, Filename: "glib.glib-2-90.id.po", Language: "id"},
	}}
	s := NewGlossaryService(repo, nil)
	client := gnomel10n.New(srv.URL, true, "test")
	client.SetBudget(upstreamReserve + 3) // the release page (not served here), the release list and one module
	s.SetUpstream(client)
	s.WarmUpstream(ctx)
	if len(repo.states) != 1 || *calls != 3 {
		t.Errorf("first round: %d states, %d requests", len(repo.states), *calls)
	}
	client.SetBudget(upstreamReserve + 5) // the page again and the other module (the list is cached)
	s.WarmUpstream(ctx)
	if len(repo.states) != 2 {
		t.Errorf("second round: %d states", len(repo.states))
	}
}

func TestUpstreamStateKeys(t *testing.T) {
	keys := models.UpstreamStateKeys()
	for _, f := range models.UpstreamFilters {
		keys = append(keys, "upstream.state_"+f)
	}
	for _, k := range keys {
		for _, lang := range []string{"id", "en"} {
			if i18n.T(lang, k) == k {
				t.Errorf("%s missing in %s", k, lang)
			}
		}
	}
	u := &models.UpstreamStats{State: "Translating"}
	if u.StateKey() != "upstream.state_translating" {
		t.Errorf("Translating → %q", u.StateKey())
	}
	if (&models.UpstreamStats{State: "Rejected?"}).StateKey() != "" {
		t.Error("unknown state got a key")
	}
	if (&models.UpstreamStats{State: "ToCommit"}).StateKey() != "upstream.state_to_commit" {
		t.Error("ToCommit")
	}
}

// The release page fills every state with one request; the API's po_file is kept and
// files the page does not list are stored as not upstream
func TestUpstreamWarmFromPage(t *testing.T) {
	page, err := os.ReadFile("../gnomel10n/testdata/page_gnome-51_id_ui.html")
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path == "/languages/id/gnome-51/ui/" {
			_, _ = w.Write(page)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	repo := &upstreamRepo{states: map[string]*models.UpstreamRecord{
		"gnome-51/id/orca.main": {Release: "gnome-51", Language: "id", Stem: "orca.main", Listed: true,
			State: "None", POFile: "/POT/orca.main/orca.main.id.po", CheckedAt: time.Now().Add(-3 * time.Hour)},
	}, versions: []*models.ProjectVersion{
		{ID: 1, Name: "gnome", Version: "51", GlossaryURL: srv.URL + "/languages/id/gnome-51/ui.tar.gz"},
	}, files: []*models.ProjectFile{
		{ID: 1, VersionID: 1, Filename: "orca.main.id.po", Language: "id"},
		{ID: 2, VersionID: 1, Filename: "gtk.gtk-4-24.id.po", Language: "id"},
		{ID: 3, VersionID: 1, Filename: "gone.main.id.po", Language: "id"},
	}}
	s := NewGlossaryService(repo, nil)
	s.SetUpstream(gnomel10n.New(srv.URL, true, "test"))
	s.WarmUpstream(ctx)
	if len(asked) != 1 {
		t.Errorf("requests %v, want only the ui page", asked)
	}
	if len(repo.states) != 86 { // 85 page rows + the missing file
		t.Errorf("stored %d states", len(repo.states))
	}
	orca := repo.states["gnome-51/id/orca.main"]
	if orca.State != "Translating" || orca.Translated != 1491 || orca.POFile != "/POT/orca.main/orca.main.id.po" {
		t.Errorf("orca = %+v", orca)
	}
	p := &models.Project{ID: 1, Language: "id"}
	files, _, _ := s.ProjectFiles(ctx, p, "", nil, nil, 0)
	got := map[int64]string{}
	for _, f := range files {
		got[f.ID] = f.UpstreamClass()
	}
	if got[1] != "translating" || got[2] != "proofreading" || got[3] != "missing" {
		t.Errorf("classes %v", got)
	}
	// fresh now: no request in the next round
	s.WarmUpstream(ctx)
	if len(asked) != 1 {
		t.Errorf("second round asked %v", asked[1:])
	}
	// a documentation file needs the doc page too; it fails here, so the API is the fallback
	repo.states["gnome-51/id/orca.main"].CheckedAt = time.Now().Add(-time.Hour) // still fresh, but rewritten from the page
	repo.files = append(repo.files, &models.ProjectFile{ID: 4, VersionID: 1, Filename: "gnome-boxes-help.gnome-50.id.po", Language: "id"})
	asked = nil
	s.WarmUpstream(ctx)
	if len(asked) < 2 || asked[0] != "/languages/id/gnome-51/ui/" || asked[1] != "/languages/id/gnome-51/doc/" ||
		!strings.HasPrefix(asked[len(asked)-1], "/api/v1/") {
		t.Errorf("doc fallback asked %v", asked)
	}
	// the ui page was still stored
	if repo.states["gnome-51/id/orca.main"].CheckedAt.Before(time.Now().Add(-time.Minute)) {
		t.Error("ui rows not stored when the doc page failed")
	}
}

func TestGuessUpstreamPage(t *testing.T) {
	s := &GlossaryService{}
	s.SetUpstream(gnomel10n.New("https://l10n.gnome.org", false, "test"))
	cases := map[string]string{
		"gnome-calculator.gnome-51": "https://l10n.gnome.org/vertimus/gnome-calculator/gnome-51/po/id/",
		"gnome-boxes-help.main":     "https://l10n.gnome.org/vertimus/gnome-boxes/main/help/id/",
		"nodot":                     "",
	}
	for stem, want := range cases {
		if got := s.guessPageURL(upstreamKey{release: "gnome-51", lang: "id", stem: stem}); got != want {
			t.Errorf("%s: %q, want %q", stem, got, want)
		}
	}
}
