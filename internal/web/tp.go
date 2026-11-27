package web

import (
	"errors"
	"net/http"

	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/source"
	"pelokalan/internal/web/middleware"
)

// Translation Project (service/tp.go): the site takes translations by email only, from the translator's
// own address, so the file menu offers the file under the name the robot expects and an email to the
// robot with that subject; the translator attaches the file and sends it.

// DownloadTP returns the owner's file prepared for the Translation Project: GET /catalogs/{cid}/tp
func (h *Handler) DownloadTP(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	d, err := h.glossary.TPFor(r.Context(), p, c)
	if errors.Is(err, source.ErrNotTP) {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	pc, err := h.catalogService.Build(r.Context(), middleware.GetUser(r.Context()), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	name, body := source.TPSubmit(pc, d)
	sendFile(w, name, "text/x-gettext-translation; charset=utf-8", body)
}
