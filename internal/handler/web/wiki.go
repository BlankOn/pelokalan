package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// wikiPageData fills the data shared by the wiki view and editor
func (h *Handler) wikiPageData(r *http.Request, p *models.Project, tree *service.WikiTree, current *models.WikiPage) PageData {
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.wiki") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "wiki"
	data.Data["Tree"] = tree
	data.Data["Current"] = current
	data.Data["CanEdit"] = p.Role.IsMember()
	return data
}

// WikiIndex shows the first top-level page, or an empty wiki
func (h *Handler) WikiIndex(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	tree, err := h.wiki.Tree(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if first := tree.First(); first != nil {
		http.Redirect(w, r, p.Path()+"/wiki/"+first.Slug, http.StatusSeeOther)
		return
	}
	h.render(w, "wiki", h.wikiPageData(r, p, tree, nil))
}

// wikiRedirect sends an old page address to the page's current one; it reports whether it did
func (h *Handler) wikiRedirect(w http.ResponseWriter, r *http.Request, p *models.Project, err error, suffix string) bool {
	if !errors.Is(err, service.ErrNotFound) {
		return false
	}
	cur, rerr := h.wiki.Redirect(r.Context(), p, chi.URLParam(r, "slug"))
	if rerr != nil {
		return false
	}
	http.Redirect(w, r, p.Path()+"/wiki/"+cur+suffix, http.StatusMovedPermanently)
	return true
}

// WikiPage shows one page
func (h *Handler) WikiPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	pg, err := h.wiki.Page(r.Context(), p, chi.URLParam(r, "slug"))
	if err != nil {
		if !h.wikiRedirect(w, r, p, err, "") {
			h.fail(w, r, err)
		}
		return
	}
	tree, err := h.wiki.Tree(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.wikiPageData(r, p, tree, pg)
	data.Title = pg.Title + " · " + data.Title
	data.Data["Page"] = pg
	data.Data["Crumbs"] = tree.Breadcrumbs(pg)
	if node := tree.ByID[pg.ID]; node != nil {
		data.Data["Depth"] = node.Depth
		data.Data["Subpages"] = node.Children
	}
	data.Data["MaxDepth"] = models.WikiMaxDepth
	if edits, err := h.wiki.Edits(r.Context(), pg); err == nil {
		data.Data["Edits"] = edits
	}
	data.Data["CanDelete"] = service.CanDeleteWikiPage(middleware.GetUser(r.Context()), p, pg)
	h.render(w, "wiki", data)
}

// renderWikiEdit shows the editor for a new (page nil) or existing page
func (h *Handler) renderWikiEdit(w http.ResponseWriter, r *http.Request, p *models.Project, pg *models.WikiPage, in service.WikiInput, errMsg string) {
	tree, err := h.wiki.Tree(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.wikiPageData(r, p, tree, pg)
	data.Error = errMsg
	if pg == nil {
		data.Title = i18n.T(data.Lang, "wiki.new_title") + " · " + data.Title
	} else {
		data.Title = i18n.T(data.Lang, "wiki.edit_title", pg.Title) + " · " + data.Title
	}
	data.Data["Page"] = pg
	data.Data["Input"] = in
	data.Data["Parents"] = tree.ParentOptions(pg)
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	h.render(w, "wiki_edit", data)
}

// renderWikiConflict shows the editor with the user's unsaved text and how to resolve
// a conflict: copy the text, reload to get the latest version, then edit again.
func (h *Handler) renderWikiConflict(w http.ResponseWriter, r *http.Request, p *models.Project, cur *models.WikiPage, in service.WikiInput) {
	tree, err := h.wiki.Tree(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.wikiPageData(r, p, tree, cur)
	data.Title = i18n.T(data.Lang, "wiki.edit_title", cur.Title) + " · " + data.Title
	data.Data["Page"] = cur
	data.Data["Input"] = in
	data.Data["Conflict"] = true
	w.WriteHeader(http.StatusConflict)
	h.render(w, "wiki_edit", data)
}

func wikiInput(r *http.Request) service.WikiInput {
	parent, _ := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	pos, _ := strconv.Atoi(r.FormValue("position"))
	ver, _ := strconv.ParseInt(r.FormValue("version"), 10, 64)
	return service.WikiInput{Title: r.FormValue("title"), Slug: r.FormValue("slug"), Body: r.FormValue("body"),
		ParentID: parent, Position: pos, Version: ver}
}

// WikiNew shows the editor for a new page (members); ?parent= preselects the parent
func (h *Handler) WikiNew(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
	h.renderWikiEdit(w, r, p, nil, service.WikiInput{ParentID: parent}, "")
}

// WikiCreate saves a new page
func (h *Handler) WikiCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	in := wikiInput(r)
	pg, err := h.wiki.Create(r.Context(), middleware.GetUser(r.Context()), p, in)
	if err != nil {
		if _, ok := service.AsUserError(err); ok {
			h.renderWikiEdit(w, r, p, nil, in, h.errorMessage(r, err))
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/wiki/"+pg.Slug+"?flash=wiki_saved", http.StatusSeeOther)
}

// WikiEdit shows the editor for an existing page
func (h *Handler) WikiEdit(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	pg, err := h.wiki.Page(r.Context(), p, chi.URLParam(r, "slug"))
	if err != nil {
		if !h.wikiRedirect(w, r, p, err, "/edit") {
			h.fail(w, r, err)
		}
		return
	}
	in := service.WikiInput{Title: pg.Title, Slug: pg.Slug, Body: pg.Body, Version: pg.Version()}
	if pg.ParentID != nil {
		in.ParentID = *pg.ParentID
	}
	h.renderWikiEdit(w, r, p, pg, in, "")
}

// WikiSave stores changes to a page; on a conflict the editor comes back with the user's text
func (h *Handler) WikiSave(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	in := wikiInput(r)
	pg, err := h.wiki.Update(r.Context(), middleware.GetUser(r.Context()), p, chi.URLParam(r, "slug"), in)
	if err != nil {
		if _, ok := service.AsUserError(err); ok {
			cur, perr := h.wiki.Page(r.Context(), p, chi.URLParam(r, "slug"))
			if perr != nil {
				h.fail(w, r, perr)
				return
			}
			if err == service.ErrWikiConflict {
				// keep the old version: saving again is refused until the user reloads,
				// copies their text into the latest version and saves that
				h.renderWikiConflict(w, r, p, cur, in)
				return
			}
			h.renderWikiEdit(w, r, p, cur, in, h.errorMessage(r, err))
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/wiki/"+pg.Slug+"?flash=wiki_saved", http.StatusSeeOther)
}

// WikiDelete removes a page
func (h *Handler) WikiDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.wiki.Delete(r.Context(), middleware.GetUser(r.Context()), p, chi.URLParam(r, "slug")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/wiki?flash=wiki_deleted", http.StatusSeeOther)
}

// WikiPreview renders Markdown exactly as a saved page would look
func (h *Handler) WikiPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadMemberProject(w, r); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body := r.FormValue("body")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.TrimSpace(body) == "" {
		fmt.Fprintf(w, `<p class="muted">%s</p>`, i18n.T(middleware.GetLang(r.Context()), "wiki.preview_empty"))
		return
	}
	_, _ = w.Write([]byte(wikiHTML(body)))
}

type wikiUploadResult struct {
	Error string           `json:"error,omitempty"`
	Files []wikiUploadFile `json:"files,omitempty"`
}

type wikiUploadFile struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
}

// wikiFileURL is the address an uploaded file is served at
func wikiFileURL(p *models.Project, f *models.WikiFile) string {
	return fmt.Sprintf("%s/wiki/files/%d/%s", p.Path(), f.ID, url.PathEscape(f.Filename))
}

// WikiUpload stores images and returns Markdown to insert (JSON)
func (h *Handler) WikiUpload(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fail := func(err error) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(wikiUploadResult{Error: h.errorMessage(r, err)})
	}
	uploads, err := readAttachments(w, r)
	if err != nil {
		fail(err)
		return
	}
	files, err := h.wiki.Upload(r.Context(), middleware.GetUser(r.Context()), p, uploads)
	if err != nil {
		fail(err)
		return
	}
	res := wikiUploadResult{}
	for _, f := range files {
		u := wikiFileURL(p, f)
		clean := strings.NewReplacer("[", "", "]", "", "\n", " ")
		md := "![" + clean.Replace(strings.TrimSuffix(f.Filename, filepath.Ext(f.Filename))) + "](" + u + ")"
		if !f.IsImage() {
			// other files become a download link: "📎 notes.pdf (1.2 MB)"
			md = "[📎 " + clean.Replace(f.Filename) + " (" + humanSize(f.Size) + ")](" + u + ")"
		}
		res.Files = append(res.Files, wikiUploadFile{Name: f.Filename, URL: u, Markdown: md})
	}
	_ = json.NewEncoder(w).Encode(res)
}

// WikiFile serves an uploaded wiki image (public, like the wiki)
func (h *Handler) WikiFile(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	f, data, err := h.wiki.File(r.Context(), p, idParam(r, "fid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	hdr.Set("ETag", `"`+f.BlobKey+`"`)
	disposition, ctype := "attachment", "application/octet-stream"
	if f.IsImage() {
		disposition, ctype = "inline", f.ContentType
	}
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Filename}))
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Header.Get("If-None-Match") == `"`+f.BlobKey+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(data)
}

// WikiMove moves a page in the tree (drag and drop in the sidebar); JSON response
func (h *Handler) WikiMove(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	target, _ := strconv.ParseInt(r.FormValue("target_id"), 10, 64)
	err := h.wiki.Move(r.Context(), middleware.GetUser(r.Context()), p, chi.URLParam(r, "slug"), target, r.FormValue("mode"))
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// humanSize writes a byte count like "820 B", "14 KB" or "1.2 MB"
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return strconv.FormatInt((n+512)/1024, 10) + " KB"
	default:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB"
	}
}
