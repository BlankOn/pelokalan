package web

import (
	"net/http"
	"strconv"

	"pelokalan/internal/catalog"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/pretranslate"
	"pelokalan/internal/web/middleware"
)

// Consistency check in the review editor ("Cek konsistensi", people who review): the review's strings go through
// the glossary extraction and their terms are compared with the glossary and with each other
// (pretranslate.Service.CheckConsistency); the result shows in a panel below the table.

// consistencyView is the data of the consistency_panel partial
type consistencyView struct {
	Lang  string
	URL   string // the check's address (polled while it runs)
	State *pretranslate.ConsistencyState
	Error string
}

// reviewFile loads a review and its file for people who review it
func (h *Handler) reviewFile(w http.ResponseWriter, r *http.Request) (*models.Project, *models.ModuleReview, *models.Catalog, bool) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if !p.Role.CanReview() {
		h.fail(w, r, apperr.ErrForbidden)
		return nil, nil, nil, false
	}
	rv, err := h.reviews.Get(r.Context(), p, idParam(r, "rid"))
	if err != nil {
		h.fail(w, r, err)
		return nil, nil, nil, false
	}
	c, _, err := h.catalogService.Get(r.Context(), middleware.GetUser(r.Context()), rv.CatalogID)
	if err != nil {
		h.fail(w, r, err)
		return nil, nil, nil, false
	}
	return p, rv, c, true
}

// ReviewConsistency shows the file's last check: GET /projects/{id}/reviews/{rid}/consistency (HTML partial)
func (h *Handler) ReviewConsistency(w http.ResponseWriter, r *http.Request) {
	p, rv, c, ok := h.reviewFile(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.renderPartials(w, partial{"consistency_panel", consistencyView{Lang: middleware.GetLang(r.Context()),
		URL: p.Path() + "/reviews/" + strconv.FormatInt(rv.ID, 10) + "/consistency", State: h.ai.Consistency(c.ID)}})
}

// StartReviewConsistency starts a check of the review's strings: POST /projects/{id}/reviews/{rid}/consistency
// (HTML partial: the running panel, or the reason it could not start)
func (h *Handler) StartReviewConsistency(w http.ResponseWriter, r *http.Request) {
	p, rv, c, ok := h.reviewFile(w, r)
	if !ok {
		return
	}
	view := consistencyView{Lang: middleware.GetLang(r.Context()), URL: p.Path() + "/reviews/" + strconv.FormatInt(rv.ID, 10) + "/consistency"}
	listing, err := h.catalogService.Entries(r.Context(), c, catalog.EntryFilter{Only: h.reviewHashes(r, p, c)})
	if err == nil {
		err = h.ai.CheckConsistency(r.Context(), middleware.GetUser(r.Context()), p, c, listing.Entries)
	}
	if err != nil {
		view.Error = h.errorMessage(r, err)
	}
	view.State = h.ai.Consistency(c.ID)
	h.renderPartials(w, partial{"consistency_panel", view})
}
