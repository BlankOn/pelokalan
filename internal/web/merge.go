package web

import (
	"net/http"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/review"
	"pelokalan/internal/web/middleware"
)

// UpstreamMergePlan shows what "Tarik upstream" would do to a file (nothing changes): GET /catalogs/{cid}/upstream-merge
// → JSON catalog.MergePlan
func (h *Handler) UpstreamMergePlan(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	plan, err := h.reviews.PlanUpstreamMerge(r.Context(), middleware.GetUser(r.Context()), p, c, h.serverConfig.MaxUploadMB<<20)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// UpstreamMerge merges a file with upstream as planned: POST /catalogs/{cid}/upstream-merge (version, c_<key> =
// mine|upstream for every conflict) → JSON {message}
func (h *Handler) UpstreamMerge(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	choices := map[string]string{}
	for k, v := range r.PostForm {
		if strings.HasPrefix(k, "c_") && len(v) > 0 {
			choices[strings.TrimPrefix(k, "c_")] = v[0]
		}
	}
	if c.OwnerID == user.ID && review.CanMergeUpstream(user, p, c) { // the owner's own work claims the module, as edits do
		if err := h.reservations.Claim(r.Context(), user, p, models.ModuleOf(c)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
			return
		}
	}
	plan, err := h.reviews.ApplyUpstreamMerge(r.Context(), user, p, c, h.serverConfig.MaxUploadMB<<20, r.PostForm.Get("version"), choices)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	lang := middleware.GetLang(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(lang, "flash.upstream_merged",
		plan.Updated+plan.Added, plan.Kept, len(plan.Conflicts))})
}
