package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/service"
)

// Git patches for module groups synced from a GitHub repository, one file (= one module) at a time, so
// each module goes on its own branch and pull request. Each repository file's ⋯ menu downloads its
// patch, or copies a command (PatchCommand) that fetches the repository branch, starts the module's own
// branch from it and applies the patch from a signed link (`/git/{token}.patch`, service/patchlink.go).

func (h *Handler) sendPatch(w http.ResponseWriter, r *http.Request, data []byte, filename string) {
	w.Header().Set("Content-Type", "text/x-patch; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// catalogPatch builds the owner's changes to one repository file
func (h *Handler) catalogPatch(r *http.Request, user *models.User, c *models.Catalog, p *models.Project) (*service.GitPatchResult, error) {
	return h.catalogService.GitPatch(r.Context(), user, p, []*models.Catalog{c}, strings.TrimRight(h.siteConfig.BaseURL, "/")+c.URL())
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

// PatchLink returns a signed link to the user's patch of one file, for curl
func (h *Handler) PatchLink(user *models.User, c *models.Catalog) string {
	return strings.TrimRight(h.siteConfig.BaseURL, "/") + "/git/" + h.authService.PatchLinkToken(user, "c", c.ID, time.Now()) + ".patch"
}

// PatchCommand is what a member runs in their clone of the repository: fetch the branch the file was
// synced from (by address, so it works whatever the remotes are called), start the module's own branch
// from it (-C: a later round starts again from the newest upstream; the patch holds all the member's
// changes anyway) and apply the patch
func PatchCommand(g *models.ModuleGroup, repoPath, link string) string {
	return "git fetch https://github.com/" + g.GitRepo + ".git " + g.GitBranch +
		" && git switch -C " + service.GitBranchName(repoPath) + " FETCH_HEAD" +
		" && curl -fsSL '" + link + "' | git am"
}

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
	c, p, err := h.catalogService.Get(r.Context(), user, link.CatalogID)
	if err == nil && !c.Owned {
		err = service.ErrForbidden
	}
	if err == nil {
		res, err = h.catalogPatch(r, user, c, p)
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
