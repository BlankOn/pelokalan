package gnomel10n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixtures recorded from https://l10n.gnome.org/api/v1/ on 2026-11-11 (testdata/)
var fixtures = map[string]string{
	"/api/v1/releases/":                      "releases.json",
	"/api/v1/releases/gnome-51/languages/id": "release_gnome-51_id.json",
	"/api/v1/modules/gnome-maps/branches/gnome-51/domains/po/languages/id":            "stats_gnome-maps_po_id.json",
	"/api/v1/modules/orca/branches/main/domains/po/languages/id":                      "stats_orca_po_id.json",
	"/api/v1/modules/glib/branches/glib-2-90/domains/po/languages/id":                 "stats_glib_po_id.json",
	"/api/v1/modules/gweather-locations/branches/main/domains/locations/languages/id": "stats_gweather-locations_locations_id.json",
	"/api/v1/modules/gnome-desktop/branches/master/domains/desktop-gpl/languages/id":  "stats_gnome-desktop_desktop-gpl_id.json",
	"/api/v1/modules/gnome-boxes/branches/gnome-50/domains/help/languages/id":         "stats_gnome-boxes_help_id.json",
}

type fakeSite struct {
	mu       sync.Mutex
	hits     map[string]int
	throttle bool
}

func (f *fakeSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits[r.URL.Path]++
	throttle := f.throttle
	f.mu.Unlock()
	if throttle {
		w.WriteHeader(http.StatusTooManyRequests)
		// recorded answer of the site when throttled
		_, _ = w.Write([]byte(`{"detail":"Request was throttled. Expected available in 2781 seconds."}`))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/POT/") {
		_, _ = w.Write([]byte("msgid \"\"\nmsgstr \"\"\n"))
		return
	}
	name, ok := fixtures[r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"No Language matches the given query."}`))
		return
	}
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (f *fakeSite) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func newTest(t *testing.T) (*Client, *fakeSite, *time.Time) {
	t.Helper()
	site := &fakeSite{hits: map[string]int{}}
	srv := httptest.NewServer(site)
	t.Cleanup(srv.Close)
	c := New(srv.URL, true, "test")
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, site, &now
}

func TestStem(t *testing.T) {
	cases := map[Entry]string{
		{Module: "gnome-maps", Branch: "gnome-51", Domain: "po"}:            "gnome-maps.gnome-51",
		{Module: "gnome-boxes", Branch: "gnome-50", Domain: "help"}:         "gnome-boxes-help.gnome-50",
		{Module: "gweather-locations", Branch: "main", Domain: "locations"}: "locations.main",
		{Module: "LocalSearch", Branch: "main", Domain: "po"}:               "localsearch.main",
	}
	for e, want := range cases {
		if got := e.Stem(); got != want {
			t.Errorf("%+v: stem %q, want %q", e, got, want)
		}
	}
	if got := FileStem("LocalSearch.main.id.po", "id"); got != "localsearch.main" {
		t.Errorf("FileStem = %q", got)
	}
	if got := FileStem("gtk.gtk-4-24.pt_BR.po", "pt-BR"); got != "gtk.gtk-4-24" {
		t.Errorf("FileStem pt-BR = %q", got)
	}
	if got := FileStem("id.po", "id"); got != "" {
		t.Errorf("FileStem of a bare language file = %q", got)
	}
}

func TestReleaseFromURL(t *testing.T) {
	c := New("", false, "")
	if got := c.ReleaseFromURL("https://l10n.gnome.org/languages/id/gnome-51/ui.tar.gz"); got != "gnome-51" {
		t.Errorf("got %q", got)
	}
	if got := c.ReleaseFromURL("https://example.org/languages/id/gnome-51/ui.tar.gz"); got != "" {
		t.Errorf("other host: got %q", got)
	}
	s := &Stats{Module: "gnome-maps", Branch: "gnome-51", Domain: "po", Language: "id"}
	if got := c.PageURL(s); got != "https://l10n.gnome.org/vertimus/gnome-maps/gnome-51/po/id/" {
		t.Errorf("PageURL = %q", got)
	}
}

func TestReleaseListing(t *testing.T) {
	c, _, _ := newTest(t)
	rel, err := c.Releases(context.Background())
	if err != nil || !rel["gnome-51"] || rel["gnome-99"] {
		t.Fatalf("releases: %v %v", rel, err)
	}
	entries, err := c.Release(context.Background(), "gnome-51", "id")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 113 {
		t.Errorf("entries = %d, want 113", len(entries))
	}
	domains := map[string]int{}
	for _, e := range entries {
		domains[e.Domain]++
	}
	if domains["po"] != 83 || domains["help"] != 23 || domains["locations"] != 1 {
		t.Errorf("domains = %v", domains)
	}
	if _, err := c.Release(context.Background(), "gnome-51", "xx"); err != ErrNotFound {
		t.Errorf("unknown language: %v", err)
	}
}

func TestLookup(t *testing.T) {
	c, site, now := newTest(t)
	files := []string{"gnome-maps.gnome-51.id.po", "glib.glib-2-90.id.po", "locations.main.id.po",
		"desktop-gpl.master.id.po", "gnome-boxes-help.gnome-50.id.po", "not-in-release.main.id.po", "orca.main.id.po"}
	got := c.Lookup(context.Background(), "gnome-51", "id", files)
	if len(got) != 6 {
		t.Fatalf("found %d, want 5: %v", len(got), got)
	}
	maps := got["gnome-maps.gnome-51.id.po"]
	if maps.State != "None" || maps.Translated != 535 || maps.Total() != 535 ||
		maps.POFile != "/POT/gnome-maps.gnome-51/gnome-maps.gnome-51.id.po" {
		t.Errorf("gnome-maps = %+v", maps)
	}
	if orca := got["orca.main.id.po"]; orca.State != "Translating" {
		t.Errorf("orca = %+v", orca)
	}
	glib := got["glib.glib-2-90.id.po"]
	if glib.Translated != 1247 || glib.Fuzzy != 15 || glib.Untranslated != 9 {
		t.Errorf("glib = %+v", glib)
	}
	if got["locations.main.id.po"].Module != "gweather-locations" {
		t.Errorf("locations = %+v", got["locations.main.id.po"])
	}

	// cached: no second request
	c.Lookup(context.Background(), "gnome-51", "id", files)
	if n := site.count("/api/v1/modules/glib/branches/glib-2-90/domains/po/languages/id"); n != 1 {
		t.Errorf("glib fetched %d times", n)
	}
	// stale after an hour: fetched again, the release list stays cached
	*now = now.Add(StatsTTL + time.Minute)
	c.Lookup(context.Background(), "gnome-51", "id", files)
	if n := site.count("/api/v1/modules/glib/branches/glib-2-90/domains/po/languages/id"); n != 2 {
		t.Errorf("after an hour glib fetched %d times", n)
	}
	if n := site.count("/api/v1/releases/gnome-51/languages/id"); n != 1 {
		t.Errorf("release list fetched %d times", n)
	}
}

func TestBudgetAndThrottle(t *testing.T) {
	c, site, now := newTest(t)
	c.SetBudget(2)
	files := []string{"gnome-maps.gnome-51.id.po", "glib.glib-2-90.id.po", "locations.main.id.po"}
	got := c.Lookup(context.Background(), "gnome-51", "id", files)
	if len(got) != 1 { // one request for the list, one for a module
		t.Errorf("with a budget of 2 found %d", len(got))
	}
	// the site throttles: stale answers keep being served and we wait as asked
	c.SetBudget(100)
	*now = now.Add(2 * StatsTTL)
	site.mu.Lock()
	site.throttle = true
	site.mu.Unlock()
	got = c.Lookup(context.Background(), "gnome-51", "id", files)
	if len(got) != 1 {
		t.Errorf("stale answers while throttled: %d", len(got))
	}
	c.mu.Lock()
	wait := c.blockedUntil.Sub(*now)
	c.mu.Unlock()
	if wait != 2781*time.Second {
		t.Errorf("blocked for %v", wait)
	}
	if c.Remaining() != 0 {
		t.Errorf("remaining while blocked: %d", c.Remaining())
	}
	*now = now.Add(2781 * time.Second)
	if c.Remaining() == 0 {
		t.Errorf("remaining after the wait: %d", c.Remaining())
	}
}

func TestDownload(t *testing.T) {
	c, _, _ := newTest(t)
	data, err := c.Download(context.Background(), "/POT/gnome-maps.gnome-51/gnome-maps.gnome-51.id.po", 1<<20)
	if err != nil || !strings.Contains(string(data), "msgid") {
		t.Fatalf("download: %q %v", data, err)
	}
	if _, err := c.Download(context.Background(), "/POT/x.po", 5); err == nil {
		t.Error("size limit not applied")
	}
	if _, err := c.Download(context.Background(), "http://evil/x", 5); err == nil {
		t.Error("absolute address accepted")
	}
}

// A page address entered by hand is read into its entry; StatsMaxAge skips a cached answer
// older than asked (the Sync button), Stats keeps it for an hour
func TestPageURLAndMaxAge(t *testing.T) {
	c, site, now := newTest(t)
	e, lang, ok := c.ParsePageURL(c.Base() + "/vertimus/glib/glib-2-90/po/id/")
	path := "/api/v1/modules/glib/branches/glib-2-90/domains/po/languages/id"
	if !ok || lang != "id" || e.Stem() != "glib.glib-2-90" || e.StatsPath != path {
		t.Fatalf("parsed %+v %q %v", e, lang, ok)
	}
	for _, bad := range []string{"https://elsewhere.test/vertimus/glib/glib-2-90/po/id/", c.Base() + "/vertimus/glib/po/id/", c.Base() + "/module/glib/"} {
		if _, _, ok := c.ParsePageURL(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := c.Stats(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	_, _ = c.Stats(context.Background(), e)
	if n := site.count(path); n != 1 {
		t.Errorf("cached stats fetched %d times", n)
	}
	_, _ = c.StatsMaxAge(context.Background(), e, time.Minute)
	if n := site.count(path); n != 2 {
		t.Errorf("a 10 minute old answer with max age 1 minute: fetched %d times, want 2", n)
	}
}
