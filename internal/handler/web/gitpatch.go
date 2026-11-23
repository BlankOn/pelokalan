package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Git patches for module groups synced from a GitHub repository: a member downloads their own changes
// as one `git am` patch (all their repository files, or one file) and applies it in a local clone.
// Besides the download, the page offers a signed link (`/git/{token}.patch`, service/patchlink.go) for
// `curl -fsSL <link> | git am` ("Salin perintah git patch"), so nothing has to be saved and found again.

func (h *Handler) sendPatch(w http.ResponseWriter, r *http.Request, data []byte, filename string) {
	w.Header().Set("Content-Type", "text/x-patch; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// projectPatch builds the member's changes to all their repository files in p
func (h *Handler) projectPatch(r *http.Request, user *models.User, p *models.Project) (*service.GitPatchResult, error) {
	all, err := h.catalogService.ListAll(r.Context(), user, p)
	if err != nil {
		return nil, err
	}
	var mine []*models.Catalog
	for _, c := range all {
		if c.Owned && c.RepoPath != "" {
			mine = append(mine, c)
		}
	}
	return h.catalogService.GitPatch(r.Context(), user, p, mine, strings.TrimRight(h.siteConfig.BaseURL, "/")+p.Path())
}

// catalogPatch builds the owner's changes to one repository file
func (h *Handler) catalogPatch(r *http.Request, user *models.User, c *models.Catalog, p *models.Project) (*service.GitPatchResult, error) {
	return h.catalogService.GitPatch(r.Context(), user, p, []*models.Catalog{c}, strings.TrimRight(h.siteConfig.BaseURL, "/")+c.URL())
}

// ProjectGitPatch sends the member's changes to all their repository files: GET /projects/{id}/git/patch
func (h *Handler) ProjectGitPatch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	res, err := h.projectPatch(r, middleware.GetUser(r.Context()), p)
	h.patchResponse(w, r, p, res, err)
}

// CatalogGitPatch sends the owner's changes to one repository file: GET /catalogs/{cid}/patch
func (h *Handler) CatalogGitPatch(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	res, err := h.catalogPatch(r, middleware.GetUser(r.Context()), c, p)
	h.patchResponse(w, r, p, res, err)
}

func (h *Handler) patchResponse(w http.ResponseWriter, r *http.Request, p *models.Project, res *service.GitPatchResult, err error) {
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

// PatchLink returns a signed link to the user's patch of p (catalog nil) or of one file, for curl
func (h *Handler) PatchLink(user *models.User, p *models.Project, c *models.Catalog) string {
	if user == nil {
		return ""
	}
	kind, id := "p", p.ID
	if c != nil {
		kind, id = "c", c.ID
	}
	return strings.TrimRight(h.siteConfig.BaseURL, "/") + "/git/" + h.authService.PatchLinkToken(user, kind, id, time.Now()) + ".patch"
}

// PatchCommand is the terminal command that applies the patch behind a patch link
func PatchCommand(link string) string { return "curl -fsSL '" + link + "' | git am" }

// GitPatchByLink serves a patch to a terminal: GET /git/{token}.patch (public route; the token is the
// credential). Failures are plain text with a status curl -f reports: 403 = the link expired or no longer
// works, 404 = nothing to send yet.
func (h *Handler) GitPatchByLink(w http.ResponseWriter, r *http.Request) {
	plain := func(status int, msg string) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(msg + "\n"))
	}
	lang := middleware.GetLang(r.Context())
	link, err := h.authService.ParsePatchLink(r.Context(), strings.TrimSuffix(chi.URLParam(r, "token"), ".patch"), time.Now())
	if err != nil {
		plain(http.StatusForbidden, i18n.T(lang, "git.link_invalid"))
		return
	}
	user := link.User
	if user.Lang != "" {
		lang = user.Lang
	}
	var res *service.GitPatchResult
	if link.CatalogID != 0 {
		var c *models.Catalog
		var p *models.Project
		c, p, err = h.catalogService.Get(r.Context(), user, link.CatalogID)
		if err == nil && !c.Owned {
			err = service.ErrForbidden
		}
		if err == nil {
			res, err = h.catalogPatch(r, user, c, p)
		}
	} else {
		var p *models.Project
		p, err = h.projectService.GetPublic(r.Context(), user, link.ProjectID)
		if err == nil && !p.Role.IsMember() {
			err = service.ErrForbidden
		}
		if err == nil {
			res, err = h.projectPatch(r, user, p)
		}
	}
	switch {
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrForbidden):
		plain(http.StatusForbidden, i18n.T(lang, "git.link_invalid"))
	case err != nil:
		plain(http.StatusInternalServerError, h.errorMessage(r, err))
	case res.Files == 0:
		plain(http.StatusNotFound, i18n.T(lang, "flash.git_no_changes"))
	default:
		h.sendPatch(w, r, res.Data, res.Filename)
	}
}
