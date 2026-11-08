package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
)

// SaveCatalogContext stores the Konteks note of a PO file (owner; JSON)
func (h *Handler) SaveCatalogContext(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	n, err := h.contexts.Save(r.Context(), middleware.GetUser(r.Context()), c, r.FormValue("note"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	lang := middleware.GetLang(r.Context())
	res := map[string]any{"status": i18n.T(lang, "context.removed"), "has": false}
	if n != nil {
		res = map[string]any{"status": i18n.T(lang, "context.saved", timeAgo(lang, n.UpdatedAt)), "has": true}
	}
	_ = json.NewEncoder(w).Encode(res)
}

// ContextsPage lists the Konteks notes of all PO files of a project (public, like the files)
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
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.contexts") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "contexts"
	data.Data["Items"] = items
	data.Data["Query"] = q
	h.render(w, "contexts", data)
}
