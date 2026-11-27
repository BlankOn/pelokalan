package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/review"
	"pelokalan/internal/web/middleware"
)

// Full GitHub integration (service/review.go, service/github.go): managers check the GitHub App's access on a
// module group and turn the integration on; members ask for a review of a file; owners and managers approve
// (Pelokalan commits the file to the branch) or send it back with a note, or send a file right away.

// SetReviews turns on reviews of modules for repositories Pelokalan writes to
func (h *Handler) SetReviews(s *review.Service) { h.reviews = s }

// CheckModuleGroupGitHub asks GitHub about the app's access: POST /projects/{id}/module-groups/{gid}/github/check
func (h *Handler) CheckModuleGroupGitHub(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if _, err := h.glossary.CheckGitAccess(r.Context(), p, idParam(r, "gid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, moduleGroupsPath(p, url.Values{"flash": {"github_checked"}}, "list"), http.StatusSeeOther)
}

// SetModuleGroupDirect turns full integration on or off: POST /projects/{id}/module-groups/{gid}/github/direct (on=1|0)
func (h *Handler) SetModuleGroupDirect(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	on := r.FormValue("on") == "1"
	if err := h.glossary.SetGitDirect(r.Context(), p, idParam(r, "gid"), on); err != nil {
		h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "list")
		return
	}
	flash := "github_direct_off"
	if on {
		flash = "github_direct_on"
	}
	http.Redirect(w, r, moduleGroupsPath(p, url.Values{"flash": {flash}}, "list"), http.StatusSeeOther)
}

// RequestReview asks for a review of the owner's file: POST /catalogs/{cid}/review (note)
func (h *Handler) RequestReview(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	if _, err := h.reviews.Request(r.Context(), middleware.GetUser(r.Context()), p, c, r.FormValue("note")); err != nil {
		h.failAction(w, r, err) // the popup stays on the page (JSON) and refreshes the file rows
		return
	}
	h.finish(w, r, safeNext(r.FormValue("next"), p.Path()+"/files"), "review_requested")
}

// PushCatalog sends a file to the repository right away (owners and managers): POST /catalogs/{cid}/push
func (h *Handler) PushCatalog(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	if _, err := h.reviews.PushNow(r.Context(), middleware.GetUser(r.Context()), p, c); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/files?flash=review_pushed", http.StatusSeeOther)
}

func flashSep(next string) string {
	if strings.Contains(safeNext(next, "/x"), "?") {
		return "&"
	}
	return "?"
}

type diffLine struct {
	Kind string // add, del, hunk, ""
	Text string
}

// ReviewPage is the review editor of a request: GET /projects/{id}/reviews/{rid}
func (h *Handler) ReviewPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	rv, err := h.reviews.Get(r.Context(), p, idParam(r, "rid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	user := middleware.GetUser(r.Context())
	c, _, err := h.catalogService.Get(r.Context(), user, rv.CatalogID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// the translator fixes things in the full editor: their own file opens there, on the strings with open
	// review comments when there are any
	if user != nil && c.OwnerID == user.ID && c.Owned {
		target := c.URL()
		if h.reviewComments(r, p, c, nil) > 0 {
			target += "?status=review"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	h.editorPage(w, r, c, p, rv)
}

// viewData is the view-only layout of another member's file: the review editor's look without decisions or
// discussion; repository files also get the diff of the member's changes against the last synced file
func (h *Handler) viewData(r *http.Request, p *models.Project, c *models.Catalog, data *PageData) {
	data.Data["ViewOnly"] = true
	data.Data["ReadOnly"] = true
	data.Data["Owned"] = false
	if pf, err := h.glossary.UpstreamFor(r.Context(), p, c); err == nil && pf != nil { // reserve/take the module from here
		h.viewAction(r, p, pf, data.Data)
	}
	if c.RepoPath == "" {
		return
	}
	if d, err := h.reviews.Changes(r.Context(), p, c); err == nil && d != nil {
		data.Data["Diff"] = diffLines(d.Hunks)
		data.Data["Changed"] = d.Changed
	}
}

// reviewData adds what the review editor needs: the request, the decision rights and the diff
func (h *Handler) reviewData(r *http.Request, p *models.Project, c *models.Catalog, rv *models.ModuleReview, data *PageData) {
	user := middleware.GetUser(r.Context())
	data.Title = i18n.T(data.Lang, "review.title", rv.RepoPath) + " · " + p.Name
	data.Data["Review"] = rv
	data.Data["ReadOnly"] = true
	data.Data["Owned"] = false
	if rv.Status == models.ReviewRequested {
		rv.OpenComments = h.reviewComments(r, p, c, nil)
	}
	data.Data["CanDecide"] = rv.Status == models.ReviewRequested && p.Role.CanManage() && user != nil && rv.RequestedBy != user.ID
	data.Data["CanMerge"] = rv.Status == models.ReviewRequested && review.CanMergeUpstream(user, p, c) // "Tarik upstream"
	data.Data["CanWithdraw"] = rv.Status == models.ReviewRequested && user != nil && rv.RequestedBy == user.ID
	if rv.Status != models.ReviewRequested {
		return
	}
	d, err := h.reviews.Changes(r.Context(), p, c)
	if err != nil || d == nil {
		return
	}
	data.Data["Diff"] = diffLines(d.Hunks)
	data.Data["Changed"] = d.Changed
}

// diffLines splits unified diff hunks into lines classed for display
func diffLines(hunks string) []diffLine {
	var lines []diffLine
	for _, l := range strings.Split(strings.TrimRight(hunks, "\n"), "\n") {
		kind := ""
		switch {
		case strings.HasPrefix(l, "@@"):
			kind = "hunk"
		case strings.HasPrefix(l, "+"):
			kind = "add"
		case strings.HasPrefix(l, "-"):
			kind = "del"
		}
		lines = append(lines, diffLine{kind, l})
	}
	return lines
}

// ApproveReview commits a requested file: POST /projects/{id}/reviews/{rid}/approve
func (h *Handler) ApproveReview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	rv, err := h.reviews.Approve(r.Context(), middleware.GetUser(r.Context()), p, idParam(r, "rid"))
	if err != nil {
		if rv != nil { // the commit failed: the request stays open and shows the error
			http.Redirect(w, r, p.Path()+"/reviews/"+strconv.FormatInt(idParam(r, "rid"), 10)+"?flash=review_failed", http.StatusSeeOther)
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/reviews/"+strconv.FormatInt(rv.ID, 10)+"?flash=review_pushed", http.StatusSeeOther)
}

// RejectReview sends a request back with a note: POST /projects/{id}/reviews/{rid}/reject (note)
func (h *Handler) RejectReview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	id := idParam(r, "rid")
	if err := h.reviews.Reject(r.Context(), middleware.GetUser(r.Context()), p, id, r.FormValue("note")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/reviews/"+strconv.FormatInt(id, 10)+"?flash=review_rejected", http.StatusSeeOther)
}

// WithdrawReview takes the user's own request back: POST /projects/{id}/reviews/{rid}/withdraw
func (h *Handler) WithdrawReview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.reviews.Withdraw(r.Context(), middleware.GetUser(r.Context()), p, idParam(r, "rid")); err != nil {
		h.failAction(w, r, err)
		return
	}
	h.finish(w, r, p.Path()+"/files", "review_withdrawn")
}

// reviewHashes are the entries a review of the file sends
func (h *Handler) reviewHashes(r *http.Request, p *models.Project, c *models.Catalog) map[string]bool {
	return h.reviews.ChangedHashes(r.Context(), c)
}
