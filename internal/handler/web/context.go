package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// contextEditJSON is one history line for the editor's Konteks dialog
type contextEditJSON struct {
	Username string `json:"username"`
	At       string `json:"at"`  // RFC 3339, shown in the reader's time zone
	Ago      string `json:"ago"` // tooltip
}

func (h *Handler) contextEditsJSON(r *http.Request, n *models.ModuleContext) []contextEditJSON {
	lang := middleware.GetLang(r.Context())
	edits, _ := h.contexts.Edits(r.Context(), n)
	out := make([]contextEditJSON, 0, len(edits))
	for _, e := range edits {
		out = append(out, contextEditJSON{Username: e.Username, At: e.At.Format("2006-01-02T15:04:05Z07:00"), Ago: timeAgo(lang, e.At)})
	}
	return out
}

// SaveCatalogContext stores the Konteks of a PO file's module (members; JSON). The form
// carries the version the dialog was opened with; if someone saved in between, nothing is
// stored and the latest text comes back with conflict = true.
func (h *Handler) SaveCatalogContext(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	lang := middleware.GetLang(r.Context())
	w.Header().Set("Content-Type", "application/json")
	version, _ := strconv.ParseInt(r.FormValue("version"), 10, 64)
	n, err := h.contexts.Save(r.Context(), middleware.GetUser(r.Context()), p, c, r.FormValue("note"), version)
	if errors.Is(err, service.ErrContextConflict) {
		w.WriteHeader(http.StatusConflict)
		res := map[string]any{"conflict": true, "error": i18n.T(lang, "context.conflict_title")}
		if n != nil {
			res["note"] = n.Note
			res["version"] = n.Version
			res["help"] = i18n.T(lang, "context.conflict_help", orDash(n.UpdaterName), timeAgo(lang, n.UpdatedAt))
			res["edits"] = h.contextEditsJSON(r, n)
		}
		_ = json.NewEncoder(w).Encode(res)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	res := map[string]any{"version": n.Version, "has": strings.TrimSpace(n.Note) != "",
		"edits": h.contextEditsJSON(r, n)}
	if res["has"] == true {
		res["status"] = i18n.T(lang, "context.saved", timeAgo(lang, n.UpdatedAt))
	} else {
		res["status"] = i18n.T(lang, "context.removed")
	}
	_ = json.NewEncoder(w).Encode(res)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ContextsPage lists the Konteks notes of a project's modules (public, like the files)
func (h *Handler) ContextsPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := h.contexts.List(r.Context(), p, q)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// the files of each module, so readers can open one
	if files, err := h.catalogService.ListAll(r.Context(), middleware.GetUser(r.Context()), p); err == nil {
		byModule := map[string][]*models.Catalog{}
		for _, c := range files {
			if !c.IsTemplate {
				byModule[service.ModuleOf(c)] = append(byModule[service.ModuleOf(c)], c)
			}
		}
		for _, it := range items {
			it.Files = byModule[it.Module]
		}
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.contexts") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "contexts"
	data.Data["Items"] = items
	data.Data["Query"] = q
	h.render(w, "contexts", data)
}
