package web

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"log"
	"mime"
	"net/http"
	"net/url"
	"pelokalan/internal/ai"
	"strconv"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/internal/service"
)

// loadCatalog resolves the file of the request, either by id (/catalogs/{cid})
// or by its canonical path (/projects/{id}/{username}/po/{filename}).
func (h *Handler) loadCatalog(w http.ResponseWriter, r *http.Request) (*models.Catalog, *models.Project, bool) {
	user := middleware.GetUser(r.Context())
	var c *models.Catalog
	var p *models.Project
	var err error
	if username := chi.URLParam(r, "username"); username != "" {
		filename := chi.URLParam(r, "filename")
		if u, e := url.PathUnescape(username); e == nil {
			username = u
		}
		if f, e := url.PathUnescape(filename); e == nil {
			filename = f
		}
		proj, ok := h.loadProject(w, r)
		if !ok {
			return nil, nil, false
		}
		c, p, err = h.catalogService.GetByPath(r.Context(), user, proj.ID, username, filename)
	} else {
		c, p, err = h.catalogService.Get(r.Context(), user, idParam(r, "cid"))
	}
	if err != nil {
		h.fail(w, r, err)
		return nil, nil, false
	}
	return c, p, true
}

// loadOwnCatalog is loadCatalog for changes: only the owner may write a file
func (h *Handler) loadOwnCatalog(w http.ResponseWriter, r *http.Request) (*models.Catalog, *models.Project, bool) {
	c, p, ok := h.loadCatalog(w, r)
	if ok && !c.Owned {
		h.fail(w, r, service.ErrForbidden)
		return nil, nil, false
	}
	return c, p, ok
}

// attachComments fills the discussion comment count of each entry
func (h *Handler) attachComments(r *http.Request, p *models.Project, c *models.Catalog, entries []*models.Entry) {
	if len(entries) == 0 {
		return
	}
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = service.EntrySubjectKey(c, e)
	}
	counts, err := h.discuss.Counts(r.Context(), p, models.SubjectPOEntry, keys)
	if err != nil {
		return
	}
	for i, e := range entries {
		e.Comments = counts[keys[i]]
	}
}

func filterFromRequest(r *http.Request) service.EntryFilter {
	q := r.URL.Query()
	// status may repeat (checkboxes) or be comma separated (?status=fuzzy,ai)
	var statuses []string
	seen := map[string]bool{}
	for _, v := range q["status"] {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" && !seen[s] {
				seen[s] = true
				statuses = append(statuses, s)
			}
		}
	}
	return service.EntryFilter{Statuses: statuses, Query: q.Get("q"), Order: q.Get("order")}
}

// editorView is the data of the editor partials
type editorView struct {
	Lang        string
	Catalog     *models.Catalog
	Project     *models.Project
	Entry       *models.Entry
	Suggestions []repository.Suggestion
	ReadOnly    bool
	Error       string
	Notice      string
	Draft       []string // the user's text when a save conflicted
	Thread      string   // URL of the entry's discussion component
	Glossary    *service.EntryGlossary
	OOB         bool
}

// listView is the data of the entry list partial
type listView struct {
	Lang     string
	Catalog  *models.Catalog
	Listing  *service.EntryListing
	Filter   service.EntryFilter
	Selected int64
}

// CatalogRedirect sends old /catalogs/{cid} page links to the canonical file URL
func (h *Handler) CatalogRedirect(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	// a deleted file lives at its id address (read-only): show it here
	if c.IsDeleted() {
		switch {
		case strings.HasSuffix(r.URL.Path, "/history"):
			h.History(w, r)
		case strings.HasSuffix(r.URL.Path, "/raw"):
			h.RawEditor(w, r)
		default:
			h.Editor(w, r)
		}
		return
	}
	target := c.URL()
	if strings.HasSuffix(r.URL.Path, "/history") {
		target += "/history"
	} else if strings.HasSuffix(r.URL.Path, "/raw") {
		target += "/raw"
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// Editor shows the translation editor
func (h *Handler) Editor(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	f := filterFromRequest(r)
	listing, err := h.catalogService.Entries(r.Context(), c, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "")
	name := c.Filename
	if !c.IsTemplate {
		name = c.LanguageName()
	}
	data.Title = name + " · " + p.Name + " · " + h.siteConfig.Name
	data.Data["Project"] = p
	data.Data["Catalog"] = c
	data.Data["Filter"] = f
	data.Data["List"] = listView{Lang: data.Lang, Catalog: c, Listing: listing, Filter: f}
	data.Data["Status"] = map[string]any{"Lang": data.Lang, "Listing": listing, "Catalog": c}
	data.Data["ReadOnly"] = c.IsTemplate || !c.Owned
	data.Data["Owned"] = c.Owned
	if n, err := h.contexts.ForCatalog(r.Context(), c); err == nil {
		data.Data["FileContext"] = n
	}
	if !c.IsTemplate {
		// the module's Konteks, shared by every member's file of this module
		data.Data["ContextModule"] = service.ModuleOf(c)
		data.Data["CanEditContext"] = p.Role.IsMember()
		data.Data["ContextEdits"] = []models.ContextEdit{} // the history partial needs a list, also when empty
		if n, err := h.contexts.ForModule(r.Context(), p.ID, service.ModuleOf(c)); err == nil && n != nil {
			data.Data["ContextVersion"] = n.Version
			if edits, err := h.contexts.Edits(r.Context(), n); err == nil && edits != nil {
				data.Data["ContextEdits"] = edits
			}
		}
	}
	data.Data["FullPage"] = true
	data.Data["Props"] = h.catalogService.Properties(c)
	h.catalogService.EnsureBaseline(r.Context(), c)
	h.attachComments(r, p, c, listing.Entries)
	// deep link to one entry: ?entry=<key hash>
	if want := r.URL.Query().Get("entry"); want != "" {
		if all, err := h.catalogService.Entries(r.Context(), c, service.EntryFilter{}); err == nil {
			for _, e := range all.Entries {
				if e.KeyHash() == want {
					data.Data["InitialEntry"] = e.ID
					break
				}
			}
		}
	}
	if c.Owned {
		data.Data["Uncommitted"] = h.commitDirtyCount(r, c)
	}
	if !c.IsTemplate && c.Owned {
		h.editorAIData(r, p, c, data)
		if tmpl, err := h.catalogService.Template(r.Context(), p, c.OwnerID); err == nil && tmpl != nil {
			data.Data["Template"] = true
		}
		if up, err := h.glossary.UpstreamFor(r.Context(), p, c); err == nil && up != nil {
			data.Data["Upstream"] = up
		}
	}
	if n := r.URL.Query().Get("pretranslated"); n != "" {
		data.Flash = i18n.T(data.Lang, "flash.pretranslated", n)
	}
	if r.URL.Query().Get("merged") != "" {
		data.Flash = i18n.T(data.Lang, "flash.merged", r.URL.Query().Get("added"), r.URL.Query().Get("obsolete"))
	}
	h.render(w, "editor", data)
}

// EntryList renders the filtered entry list (HTMX)
func (h *Handler) EntryList(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	f := filterFromRequest(r)
	listing, err := h.catalogService.Entries(r.Context(), c, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	lang := middleware.GetLang(r.Context())
	h.attachComments(r, p, c, listing.Entries)
	selected, _ := strconv.ParseInt(r.URL.Query().Get("selected"), 10, 64)
	h.renderPartials(w,
		partial{"entry_list", listView{Lang: lang, Catalog: c, Listing: listing, Filter: f, Selected: selected}},
		partial{"statusbar", map[string]any{"Lang": lang, "Listing": listing, "Catalog": c, "OOB": true}},
	)
}

func (h *Handler) editorData(r *http.Request, c *models.Catalog, p *models.Project, e *models.Entry) editorView {
	user := middleware.GetUser(r.Context())
	v := editorView{
		Lang:     middleware.GetLang(r.Context()),
		Catalog:  c,
		Project:  p,
		Entry:    e,
		ReadOnly: c.IsTemplate || !c.Owned,
		Thread:   p.Path() + "/discussions/" + models.SubjectPOEntry + "/" + service.EntrySubjectKey(c, e),
	}
	if !c.IsTemplate && c.Language != "" {
		if g, err := h.glossary.ForEntry(r.Context(), p.ID, c.Language, e.MsgID, e.MsgIDPlural); err == nil {
			if len(g.Terms) > 0 {
				words := h.contextWords(r, p, c)
				for i := range g.Terms {
					g.Terms[i].Overridden = ai.Mentioned(g.Terms[i].Term, words)
				}
			}
			v.Glossary = g
		} else {
			log.Printf("glossary terms for entry %d: %v", e.ID, err)
		}
	}
	if c.Owned && user != nil {
		if sugg, err := h.catalogService.Suggestions(r.Context(), user, c, e); err == nil {
			v.Suggestions = sugg
		}
	}
	return v
}

// EntryEditor renders the editor pane for one entry (HTMX)
func (h *Handler) EntryEditor(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	e, err := h.catalogService.Entry(r.Context(), c, idParam(r, "eid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v := h.editorData(r, c, p, e)
	sv := v
	sv.OOB = true
	h.renderPartials(w, partial{"editor", v}, partial{"sidebar", sv})
}

// SaveEntry stores a translation and returns the editor for the next entry (HTMX)
func (h *Handler) SaveEntry(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	user := middleware.GetUser(r.Context())
	lang := middleware.GetLang(r.Context())
	eid := idParam(r, "eid")

	version, _ := strconv.Atoi(r.FormValue("version"))
	forms := make([]string, 0, c.NPlurals)
	for i := 0; ; i++ {
		v, ok := r.Form["msgstr"+strconv.Itoa(i)]
		if !ok {
			break
		}
		forms = append(forms, v[0])
	}
	in := service.EntryInput{
		Version:     version,
		Translation: forms,
		Fuzzy:       r.FormValue("fuzzy") == "1",
		Comment:     r.FormValue("comment"),
	}

	saved, err := h.catalogService.SaveEntry(r.Context(), user, c, eid, in)
	if err != nil {
		e, getErr := h.catalogService.Entry(r.Context(), c, eid)
		if getErr != nil {
			h.fail(w, r, err)
			return
		}
		v := h.editorData(r, c, p, e)
		v.Error = h.errorMessage(r, err)
		if errors.Is(err, service.ErrEntryConflict) {
			v.Draft = forms
		}
		sv := v
		sv.OOB = true
		h.renderPartials(w,
			partial{"editor", v},
			partial{"sidebar", sv},
			partial{"entry_row", map[string]any{"E": e, "Lang": lang, "OOB": true}},
		)
		return
	}

	stats, _ := h.catalogService.Entries(r.Context(), c, service.EntryFilter{})
	parts := []partial{
		{"entry_row", map[string]any{"E": saved, "Lang": lang, "OOB": true}},
		{"statusbar", map[string]any{"Lang": lang, "Listing": stats, "Catalog": c, "OOB": true}},
	}
	// the Commit button counts changes since the last version, including needs-work flags
	n := h.commitDirtyCount(r, c)
	parts = append(parts,
		partial{"commit_badge", map[string]any{"N": n, "OOB": true}},
		partial{"commit_status", map[string]any{"N": n, "Lang": lang, "OOB": true}})

	next := saved
	if nid, _ := strconv.ParseInt(r.FormValue("next_id"), 10, 64); nid > 0 && nid != saved.ID {
		if ne, err := h.catalogService.Entry(r.Context(), c, nid); err == nil {
			next = ne
		}
	}
	v := h.editorData(r, c, p, next)
	if next == saved {
		v.Notice = i18n.T(lang, "editor.saved")
	}
	sv := v
	sv.OOB = true
	h.renderPartials(w, append([]partial{{"editor", v}, {"sidebar", sv}}, parts...)...)
}

// Download returns the catalog as a PO/POT file
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	pc, err := h.catalogService.Build(r.Context(), middleware.GetUser(r.Context()), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	sendFile(w, c.Filename, "text/x-gettext-translation; charset=utf-8", pc.Bytes())
}

// DownloadMO returns the compiled catalog
func (h *Handler) DownloadMO(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	if c.IsTemplate {
		h.fail(w, r, service.ErrForbidden)
		return
	}
	pc, err := h.catalogService.Build(r.Context(), middleware.GetUser(r.Context()), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	sendFile(w, service.MOFilename(c), "application/x-gettext-translation", pc.CompileMO())
}

func sendFile(w http.ResponseWriter, name, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = bytes.NewReader(body).WriteTo(w)
}

// UpdateFromTemplate merges the template into the catalog
func (h *Handler) UpdateFromTemplate(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	res, err := h.catalogService.UpdateFromTemplate(r.Context(), p, c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	redirect(w, r, c.URL()+fmt.Sprintf("?merged=1&added=%d&obsolete=%d", res.Added, res.Obsolete))
}

// PreTranslate fills untranslated entries from the translation memory
func (h *Handler) PreTranslate(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	n, err := h.catalogService.PreTranslate(r.Context(), middleware.GetUser(r.Context()), p, c, r.FormValue("fuzzy") == "1")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	redirect(w, r, c.URL()+fmt.Sprintf("?pretranslated=%d", n))
}

// ReplaceCatalog imports a new version of the translation file
func (h *Handler) ReplaceCatalog(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	name, body, err := h.readUpload(w, r)
	u := uploadedURL(r)
	if u != "" {
		name = "" // downloaded again: keep the file's name (links often end in a bare "id.po")
	}
	if err == nil {
		err = h.catalogService.Replace(r.Context(), user, p, c, name, body)
	}
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	if u != "" && u != c.SourceURL {
		_ = h.catalogService.SetSourceURL(r.Context(), user, c, u)
	}
	http.Redirect(w, r, c.URL()+"?flash=replaced", http.StatusSeeOther)
}

// DeleteCatalog removes a file
func (h *Handler) DeleteCatalog(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	if err := h.catalogService.Delete(r.Context(), middleware.GetUser(r.Context()), c); err != nil {
		h.fail(w, r, err)
		return
	}
	redirect(w, r, p.Path()+"/files?flash=file_deleted")
}

// RawEditor shows the catalog as PO text for direct editing
func (h *Handler) RawEditor(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	pc, err := h.catalogService.Build(r.Context(), middleware.GetUser(r.Context()), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderRaw(w, r, c, p, string(pc.Bytes()), "")
}

func (h *Handler) renderRaw(w http.ResponseWriter, r *http.Request, c *models.Catalog, p *models.Project, text, errMsg string) {
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "raw.title") + " · " + c.Filename
	data.Error = errMsg
	data.Data["FullPage"] = true
	data.Data["Catalog"] = c
	data.Data["Project"] = p
	data.Data["Text"] = text
	data.Data["UpdatedAt"] = c.UpdatedAt.UnixNano()
	data.Data["ReadOnly"] = !c.Owned
	h.render(w, "raw", data)
}

// SaveRaw stores edited PO text
func (h *Handler) SaveRaw(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.serverConfig.MaxUploadMB<<20)
	text := r.FormValue("content")
	updatedAt, _ := strconv.ParseInt(r.FormValue("updated_at"), 10, 64)
	if err := h.catalogService.SaveRaw(r.Context(), middleware.GetUser(r.Context()), c, text, updatedAt); err != nil {
		h.renderRaw(w, r, c, p, text, h.errorMessage(r, err))
		return
	}
	// "Simpan dulu" in the leave popup continues to where the user was going
	http.Redirect(w, r, safeNext(r.FormValue("next"), c.URL()+"?flash=raw_saved"), http.StatusSeeOther)
}

// UpdateProperties saves the catalog header values
func (h *Handler) UpdateProperties(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	err := h.catalogService.UpdateProperties(r.Context(), middleware.GetUser(r.Context()), c, service.CatalogProperties{
		ProjectVersion: r.FormValue("project_version"),
		ReportBugsTo:   r.FormValue("report_bugs_to"),
		LanguageTeam:   r.FormValue("language_team"),
		Contributors:   r.FormValue("contributors"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.URL()+"?flash=properties_saved", http.StatusSeeOther)
}

// contextWords are the words of the project's AI context and the file's Konteks; glossary
// terms containing one of them are decided by the context (same rule as AI pre-translation)
func (h *Handler) contextWords(r *http.Request, p *models.Project, c *models.Catalog) map[string]bool {
	text := ""
	if st, err := h.ai.Settings(r.Context(), p); err == nil && st != nil {
		text = st.ExtraContext
	}
	if n, err := h.contexts.ForCatalog(r.Context(), c); err == nil && n != nil {
		text += "\n" + n.Note
	}
	return ai.ContextWords(text)
}
