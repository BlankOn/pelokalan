package web

import (
	"net/http"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/models"
)

// Git patches for module groups synced from a GitHub repository: a member downloads their own changes
// as one `git am` patch (all their repository files, or one file) and applies it in a local clone.
// The guide popup on the Penerjemahan page shows the commands.

func (h *Handler) sendPatch(w http.ResponseWriter, r *http.Request, data []byte, filename string) {
	w.Header().Set("Content-Type", "text/x-patch; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// ProjectGitPatch sends the member's changes to all their repository files: GET /projects/{id}/git/patch
func (h *Handler) ProjectGitPatch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	all, err := h.catalogService.ListAll(r.Context(), user, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var mine []*models.Catalog
	for _, c := range all {
		if c.Owned && c.RepoPath != "" {
			mine = append(mine, c)
		}
	}
	res, err := h.catalogService.GitPatch(r.Context(), user, p, mine, strings.TrimRight(h.siteConfig.BaseURL, "/")+p.Path())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if res.Files == 0 {
		http.Redirect(w, r, p.Path()+"/files?flash=git_no_changes", http.StatusSeeOther)
		return
	}
	h.sendPatch(w, r, res.Data, res.Filename)
}

// CatalogGitPatch sends the owner's changes to one repository file: GET /catalogs/{cid}/patch
func (h *Handler) CatalogGitPatch(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	res, err := h.catalogService.GitPatch(r.Context(), middleware.GetUser(r.Context()), p, []*models.Catalog{c},
		strings.TrimRight(h.siteConfig.BaseURL, "/")+c.URL())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if res.Files == 0 {
		http.Redirect(w, r, p.Path()+"/files?flash=git_no_changes", http.StatusSeeOther)
		return
	}
	h.sendPatch(w, r, res.Data, res.Filename)
}
