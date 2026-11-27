package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
	"pelokalan/pkg/po"
)

// Read-only view of a project file ("Lihat" in Berkas proyek, module links in the glossary): the editor's
// view-only layout built straight from the stored file, with "Reservasi modul" (or "Ambil") when it is free.

// viewRow is one string of a viewed project file
type viewRow struct {
	Context, ID, Translation string
	Plural                   bool
	Status                   string // translated, fuzzy, untranslated
}

// ProjectFileView shows a project file read-only: GET /projects/{id}/project-files/{fid}/view (?q= filters)
func (h *Handler) ProjectFileView(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	f, data, err := h.glossary.ProjectFile(r.Context(), p, idParam(r, "fid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	cat, err := po.ParseBytes(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var rows []viewRow
	stats := models.CatalogStats{}
	for _, e := range cat.Entries {
		if e.Obsolete {
			continue
		}
		row := viewRow{Context: e.Context, ID: e.ID, Plural: e.IDPlural != "", Status: "untranslated"}
		if len(e.Translation) > 0 {
			row.Translation = e.Translation[0]
		}
		stats.Total++
		switch {
		case e.Fuzzy:
			row.Status = "fuzzy"
			stats.Fuzzy++
		case e.IsTranslated():
			row.Status = "translated"
			stats.Translated++
		default:
			stats.Untranslated++
		}
		if q != "" && !strings.Contains(strings.ToLower(e.ID+"\n"+strings.Join(e.Translation, "\n")), q) {
			continue
		}
		rows = append(rows, row)
	}
	data2 := h.page(r, "")
	data2.Title = f.Filename + " · " + p.Name
	data2.Data["Project"] = p
	data2.Data["File"] = f
	data2.Data["Rows"] = rows
	data2.Data["Stats"] = stats
	data2.Data["Query"] = r.URL.Query().Get("q")
	data2.Data["FullPage"] = true
	h.viewAction(r, p, f, data2.Data)
	h.render(w, "project_file", data2)
}

// viewAction decides the right side of a view-only toolbar for the module of a project file (nil = none):
// the member's own copy, "Reservasi modul" / "Ambil" when it is free, or who holds it
func (h *Handler) viewAction(r *http.Request, p *models.Project, f *models.ProjectFile, d map[string]any) {
	if f == nil || !p.Role.IsMember() {
		return
	}
	user := middleware.GetUser(r.Context())
	list := []*models.ProjectFile{f}
	_ = h.catalogService.MatchOwnFiles(r.Context(), user, p, list)
	d["ViewFile"] = f
	d["ViewModule"] = service.ModuleKey(f.Filename)
	d["ViewRes"] = h.reservationView(r, p)
	if f.Mine != nil {
		d["ViewMine"] = f.Mine
		return
	}
	if p.Reservations {
		if hold := h.reservations.Holder(r.Context(), p, service.ModuleKey(f.Filename)); hold != nil && (user == nil || hold.UserID != user.ID) {
			d["ViewHeld"] = hold
		}
	}
}

// ProjectFileByModule opens the project file of a glossary module: GET /projects/{id}/project-files/module/{name}
func (h *Handler) ProjectFileByModule(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	f, err := h.glossary.ProjectFileForModule(r.Context(), p, name)
	if err != nil || f == nil {
		h.fail(w, r, service.ErrNotFound)
		return
	}
	target := p.Path() + "/project-files/" + strconv.FormatInt(f.ID, 10) + "/view"
	if q := r.URL.Query().Get("q"); q != "" {
		target += "?q=" + url.QueryEscape(q)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
