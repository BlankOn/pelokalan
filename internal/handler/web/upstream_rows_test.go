package web

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/internal/service"
)

// Project file rows show the upstream state; one Take/Update button per row fetches the latest file
func TestProjectFileRowsUpstream(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(funcMap()).ParseFiles(
		"../../templates/partials/project_card.html", "../../templates/partials/progress.html"))
	up := &models.UpstreamStats{State: "Translating", Translated: 10, Fuzzy: 2, Untranslated: 3,
		PageURL: "https://l10n.gnome.org/vertimus/glib/main/po/id/", POFileURL: "https://l10n.gnome.org/POT/glib.main/glib.main.id.po"}
	files := []*models.ProjectFile{
		{ID: 7, Filename: "glib.main.id.po", Language: "id", Total: 15, Translated: 9, VersionLabel: "gnome-51", UpstreamInfo: models.UpstreamInfo{Upstream: up}},
		{ID: 8, Filename: "gtk.main.id.po", Language: "id", Total: 5, VersionLabel: "gnome-51",
			UpstreamInfo: models.UpstreamInfo{Upstream: &models.UpstreamStats{State: "Odd", Translated: 5, PageURL: "https://l10n.gnome.org/x"}},
			Mine:         &models.Catalog{ID: 3, Filename: "gtk.main.id.po"}},
		{ID: 9, Filename: "vte.main.id.po", Language: "id", Total: 5, VersionLabel: "gnome-51",
			UpstreamInfo: models.UpstreamInfo{UpstreamLink: "https://l10n.gnome.org/vertimus/vte/main/po/id/"}},
	}
	view := projectFilesView{Lang: "en", Project: &models.Project{Slug: "gnome-id"}, Files: files, Total: 3, Next: 3, CanTake: true}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "project_file_rows", view); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`href="https://l10n.gnome.org/vertimus/glib/main/po/id/"`,
		">Translating<", "66% · 10/15", "3 untranslated",
		">Odd<", ">Take<", ">Update<", `class="up-badge up-translating"`, `class="up-badge up-other"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// no state yet: a grey label linking to the site's page, to check by hand
	if !strings.Contains(out, `class="up-badge up-unknown" href="https://l10n.gnome.org/vertimus/vte/main/po/id/"`) ||
		!strings.Contains(out, "No status yet") {
		t.Error("missing the unknown-state label for vte")
	}
	// the state is a badge under the file name; the progress column no longer repeats it
	if strings.Contains(out, "pf-upstream") {
		t.Error("upstream line still in the progress column")
	}
	// no separate "latest version" button any more: Take/Update try the site first themselves
	if strings.Contains(out, `value="upstream"`) {
		t.Error("separate upstream button still rendered")
	}
	if n := strings.Count(out, `/take"`); n != 3 {
		t.Errorf("%d take forms, want one per row (3)", n)
	}
}

// "Berkas Anda" rows carry the same badge; the file filter offers the upstream states
func TestFileRowsUpstream(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(funcMap()).ParseFiles(
		"../../templates/partials/project_card.html", "../../templates/partials/progress.html"))
	files := []*models.Catalog{
		{ID: 3, Filename: "glib.main.id.po", Language: "id", UpstreamInfo: models.UpstreamInfo{UpstreamChecked: true,
			Upstream: &models.UpstreamStats{State: "Proofread", Translated: 4, PageURL: "https://l10n.gnome.org/y"}}},
		{ID: 4, Filename: "mine.id.po", Language: "id"},
	}
	view := fileListView{Lang: "id", Project: &models.Project{Slug: "gnome-id"}, Mine: true, Files: files, Total: 2, Next: 2}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "file_rows", view); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, `class="up-badge up-proofread"`) || strings.Count(out, "up-badge") != 1 {
		t.Errorf("badges:\n%s", out)
	}
	buf.Reset()
	if err := tmpl.ExecuteTemplate(&buf, "file_filter", map[string]any{"Lang": "en", "ID": "my", "URL": "/x", "Upstream": true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`name="u" value="translating"`, `name="u" value="unknown"`, `name="u" value="missing"`, "Not checked yet", "All upstream states"} {
		if !strings.Contains(out, want) {
			t.Errorf("filter missing %q", want)
		}
	}
	buf.Reset()
	_ = tmpl.ExecuteTemplate(&buf, "file_filter", map[string]any{"Lang": "en", "ID": "my", "URL": "/x"})
	if strings.Contains(buf.String(), `name="u"`) {
		t.Error("upstream filter shown for a project without upstream")
	}
}

// emptyGlossaryRepo has no versions or project files
type emptyGlossaryRepo struct{ repository.GlossaryRepository }

func (emptyGlossaryRepo) ProjectFileVersions(context.Context, int64) ([]*models.ProjectFileVersion, error) {
	return nil, nil
}
func (emptyGlossaryRepo) ProjectFileNames(context.Context, int64) (map[int64][]string, error) {
	return nil, nil
}

// "Berkas Anda": requested rework, proofreading, translating first, then by last edit;
// the same order on every page and with filters. Other members' files: last edit only.
func TestOwnFilesUpstreamOrder(t *testing.T) {
	h := &Handler{glossary: service.NewGlossaryService(emptyGlossaryRepo{}, nil)}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	file := func(id int64, state string, hoursAgo int, owned bool) *models.Catalog {
		c := &models.Catalog{ID: id, Filename: fmt.Sprintf("f%d.id.po", id), Language: "id", Owned: owned,
			UpdatedAt: base.Add(-time.Duration(hoursAgo) * time.Hour)}
		if state != "" {
			c.UpstreamInfo = models.UpstreamInfo{UpstreamChecked: true, Upstream: &models.UpstreamStats{State: state}}
		}
		return c
	}
	var all []*models.Catalog
	for _, owned := range []bool{true, false} {
		off := int64(0)
		if !owned {
			off = 100
		}
		all = append(all,
			file(off+1, "None", 1, owned),
			file(off+2, "Translating", 9, owned),
			file(off+3, "ToReview", 20, owned),
			file(off+4, "", 2, owned), // not checked yet
			file(off+5, "Proofreading", 5, owned),
			file(off+6, "Translating", 3, owned),
			file(off+7, "Committed", 0, owned),
			file(off+8, "ToReview", 4, owned),
		)
	}
	r := httptest.NewRequest("GET", "/", nil)
	p := &models.Project{ID: 1, Slug: "x"}
	var got []int64
	for offset := 0; offset < 8; offset += FileListPageSize {
		for _, c := range h.fileList(r, p, all, true, "", nil, nil, offset).Files {
			got = append(got, c.ID)
		}
	}
	if want := []int64{8, 3, 5, 6, 2, 7, 1, 4}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("own files order %v, want %v", got, want)
	}
	// with a filter the order holds
	v := h.fileList(r, p, all, true, "", nil, map[string]bool{"translating": true, "unknown": true}, 0)
	got = got[:0]
	for _, c := range v.Files {
		got = append(got, c.ID)
	}
	if fmt.Sprint(got) != "[6 2 4]" {
		t.Errorf("filtered order %v", got)
	}
	got = got[:0]
	for _, c := range h.fileList(r, p, all, false, "", nil, nil, 0).Files {
		got = append(got, c.ID)
	}
	if fmt.Sprint(got) != "[107 101 104 106 108]" {
		t.Errorf("member files order %v", got)
	}
}
