package gnomel10n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// testdata/page_gnome-51_id_ui.html: https://l10n.gnome.org/languages/id/gnome-51/ui/ as saved on 2026-11-11
func TestParseReleasePage(t *testing.T) {
	data, err := os.ReadFile("testdata/page_gnome-51_id_ui.html")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseReleasePage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 85 {
		t.Errorf("rows = %d, want 85", len(rows))
	}
	by := map[string]PageRow{}
	states := map[string]int{}
	for _, r := range rows {
		by[r.Entry().Stem()] = r
		states[r.State]++
		if r.Language != "id" {
			t.Errorf("%s: language %q", r.Module, r.Language)
		}
	}
	if states["Translated"] != 17 || states["Translating"] != 2 || states["Proofreading"] != 1 {
		t.Errorf("states = %v", states)
	}
	orca := by["orca.main"]
	if orca.State != "Translating" || orca.Translated != 1491 || orca.Fuzzy != 151 || orca.Untranslated != 236 ||
		!orca.Changed.Equal(time.Date(2026, 8, 28, 6, 6, 47, 653260000, time.UTC)) {
		t.Errorf("orca = %+v", orca)
	}
	if gtk := by["gtk.gtk-4-24"]; gtk.State != "Proofreading" || gtk.Domain != "po" {
		t.Errorf("gtk = %+v", gtk)
	}
	// blank counts are 0, no badge = no activity
	if calls := by["calls.gnome-51"]; calls.State != "None" || calls.Translated != 122 || calls.Fuzzy != 0 || !calls.Changed.IsZero() {
		t.Errorf("calls = %+v", calls)
	}
	if loc := by["locations.main"]; loc.Module != "gweather-locations" || loc.Domain != "locations" {
		t.Errorf("locations = %+v", loc)
	}
	if calc := by["gnome-calculator.gnome-51"]; calc.Translated != 791 || calc.Untranslated != 0 {
		t.Errorf("gnome-calculator = %+v", calc)
	}
}

func TestParseReleasePageLayout(t *testing.T) {
	for _, page := range []string{"", "<html><body><table><tr><td>nothing</td></tr></table></body></html>",
		`<tr><td><a href="/vertimus/a/b/po/id/">a</a></td></tr>`} {
		if _, err := ParseReleasePage([]byte(page)); err != ErrLayout {
			t.Errorf("%q: %v", page, err)
		}
	}
	rows, err := ParseReleasePage([]byte(`<tr><td><a href="/vertimus/x/main/po/id/">x</a></td>
		<td><pre class="statistics_workflow"><span class="translated"> 1,234</span><span class="fuzzy"> 5</span><span class="untranslated"> </span></pre></td>
		<td><span class="badge text-bg-x">To  Review</span></td></tr>
		<tr><td><a href="/vertimus/y/main/po/id/">y</a></td><td><pre class="statistics_workflow"></pre></td><td><span class="badge">Rejected</span></td></tr>`))
	if err != nil || len(rows) != 2 || rows[0].State != "ToReview" || rows[0].Translated != 1234 || rows[0].Fuzzy != 5 || rows[1].State != "Rejected" {
		t.Errorf("rows %+v %v", rows, err)
	}
}

func TestReleasePageRequest(t *testing.T) {
	data, _ := os.ReadFile("testdata/page_gnome-51_id_ui.html")
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + " " + r.Header.Get("Accept-Language")
		if r.URL.Path == "/languages/id/gnome-51/doc/" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	c := New(srv.URL, true, "test")
	rows, err := c.ReleasePage(context.Background(), "gnome-51", "id", "ui")
	if err != nil || len(rows) != 85 || asked != "/languages/id/gnome-51/ui/ en" {
		t.Errorf("ui page: %d rows, %v, asked %q", len(rows), err, asked)
	}
	if c.Remaining() != DefaultBudget-1 {
		t.Errorf("budget not counted: %d", c.Remaining())
	}
	if _, err := c.ReleasePage(context.Background(), "gnome-51", "id", "doc"); err != ErrBudget || c.Remaining() != 0 {
		t.Errorf("throttled page: %v, remaining %d", err, c.Remaining())
	}
}
