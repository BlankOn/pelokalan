package web

import (
	"net/http"
	"net/url"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/service"
)

// Module reservations (service/reservation.go): a project setting; members hold a module while working on
// it. Changes to a member's file go through loadEditableCatalog, which claims a free module and refuses a
// module someone else holds; the editor shows such a file read-only with a banner (applyLock). Lists show
// who holds what (attachReservations) and can be filtered by it (rv=mine|other|free).

// SetReservations turns on module reservations
func (h *Handler) SetReservations(s *service.ReservationService) { h.reservations = s }

// loadEditableCatalog is loadOwnCatalog for changes to the translations: the module must not be held by
// another member, and a free one becomes the user's
func (h *Handler) loadEditableCatalog(w http.ResponseWriter, r *http.Request) (*models.Catalog, *models.Project, bool) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok || c.IsTemplate {
		return c, p, ok
	}
	if err := h.reservations.Claim(r.Context(), middleware.GetUser(r.Context()), p, service.ModuleOf(c)); err != nil {
		h.fail(w, r, err)
		return nil, nil, false
	}
	return c, p, true
}

// applyLock looks up who holds the file's module; when it is another member, the owner's file opens
// read-only (Owned false) like any other member's file
func (h *Handler) applyLock(r *http.Request, p *models.Project, c *models.Catalog) *models.ModuleReservation {
	if c.IsTemplate {
		return nil
	}
	hold := h.reservations.Holder(r.Context(), p, service.ModuleOf(c))
	c.Reservation = hold
	if hold != nil && c.Owned && hold.UserID != c.OwnerID {
		c.Owned = false
	}
	return hold
}

// attachReservations fills Reservation of the files and project files from the project's held modules
func (h *Handler) attachReservations(r *http.Request, p *models.Project, catalogs []*models.Catalog, files []*models.ProjectFile) map[string]*models.ModuleReservation {
	held := h.reservations.Map(r.Context(), p)
	for _, c := range catalogs {
		if !c.IsTemplate {
			c.Reservation = held[service.ModuleOf(c)]
		}
	}
	for _, f := range files {
		f.Reservation = held[service.ModuleKey(f.Filename)]
	}
	return held
}

// reservationFilter keeps files by reservation (rv: mine, other, free; none = all)
func reservationFilter(q url.Values, userID int64) func(*models.ModuleReservation) bool {
	want := map[string]bool{}
	for _, v := range q["rv"] {
		want[v] = true
	}
	if len(want) == 0 {
		return nil
	}
	return func(res *models.ModuleReservation) bool {
		switch {
		case res == nil:
			return want["free"]
		case res.UserID == userID:
			return want["mine"]
		default:
			return want["other"]
		}
	}
}

// reservationBack is where the reserve/release buttons return to
// inPage reports whether the page sent the form in the background (app.js `data-ajax`): it wants JSON, not a new page
func inPage(r *http.Request) bool { return r.Header.Get("X-Requested-With") == "fetch" }

// finish ends a form action: JSON with a message for the page, else a redirect to back (with the message as a flash)
func (h *Handler) finish(w http.ResponseWriter, r *http.Request, back, flash string) {
	if inPage(r) {
		writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(middleware.GetLang(r.Context()), "flash."+flash)})
		return
	}
	http.Redirect(w, r, back+flashSep(back)+"flash="+flash, http.StatusSeeOther)
}

// failAction reports a refused form action: JSON for the page, else the usual error page
func (h *Handler) failAction(w http.ResponseWriter, r *http.Request, err error) {
	if inPage(r) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	h.fail(w, r, err)
}

func reservationBack(r *http.Request, p *models.Project) string {
	return safeNext(r.FormValue("next"), p.Path()+"/files")
}

// ReserveModule holds a module for the user: POST /projects/{id}/reservations (module)
func (h *Handler) ReserveModule(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	module := strings.TrimSpace(r.FormValue("module"))
	if err := h.reservations.Claim(r.Context(), middleware.GetUser(r.Context()), p, module); err != nil {
		h.failAction(w, r, err)
		return
	}
	h.finish(w, r, reservationBack(r, p), "module_reserved")
}

// ReleaseModule ends a reservation (the holder, or an owner/manager): POST /projects/{id}/reservations/release
func (h *Handler) ReleaseModule(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.reservations.Release(r.Context(), middleware.GetUser(r.Context()), p, strings.TrimSpace(r.FormValue("module"))); err != nil {
		h.failAction(w, r, err)
		return
	}
	h.finish(w, r, reservationBack(r, p), "module_released")
}
