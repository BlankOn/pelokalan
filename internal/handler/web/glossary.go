package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/internal/service"
)

// glossaryView is the data of the glossary result partials
type glossaryView struct {
	Lang        string
	ProjectID   int64
	ProjectPath string
	Search      service.GlossarySearch
	Versions    []*models.ProjectVersion
	Version     string // selected version id, or "all"
	Page        *service.GlossaryPage
	MoreQuery   string
	Modules     []repository.ModuleCount
}

// selectVersion resolves the "version" parameter: "all", a version id, or
// (by default) the newest version that has glossary data.
func selectVersion(param string, versions []*models.ProjectVersion) (int64, string) {
	if param == "all" {
		return 0, "all"
	}
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		for _, v := range versions {
			if v.ID == id {
				return id, param
			}
		}
	}
	for _, v := range versions {
		if v.ImportCount > 0 {
			return v.ID, strconv.FormatInt(v.ID, 10)
		}
	}
	return 0, "all"
}

func glossarySearchFromRequest(r *http.Request, languages []string) service.GlossarySearch {
	q := r.URL.Query()
	s := service.GlossarySearch{
		Language:     q.Get("lang"),
		Query:        q.Get("q"),
		TermsOnly:    true,
		Inconsistent: q.Get("inconsistent") == "1",
		Module:       q.Get("module"),
		Order:        q.Get("order"),
		Dir:          q.Get("dir"),
	}
	// most diverse (least consistent) terms first unless another sort is chosen
	if s.Order == "" {
		s.Order = "variants"
	}
	// "f" marks a submitted filter form, where an unchecked box sends nothing
	if q.Get("f") == "1" {
		s.TermsOnly = q.Get("terms") == "1"
	}
	s.Offset, _ = strconv.Atoi(q.Get("offset"))
	valid := false
	for _, l := range languages {
		if l == s.Language {
			valid = true
		}
	}
	if !valid && len(languages) > 0 {
		s.Language = languages[0]
	}
	return s
}

func (v glossaryView) moreQuery() string {
	vals := url.Values{}
	vals.Set("f", "1")
	vals.Set("lang", v.Search.Language)
	vals.Set("version", v.Version)
	vals.Set("q", v.Search.Query)
	vals.Set("order", v.Search.Order)
	vals.Set("dir", v.Search.Dir)
	if v.Search.Module != "" {
		vals.Set("module", v.Search.Module)
	}
	vals.Set("offset", strconv.Itoa(v.Page.NextOffset))
	if v.Search.TermsOnly {
		vals.Set("terms", "1")
	}
	if v.Search.Inconsistent {
		vals.Set("inconsistent", "1")
	}
	return vals.Encode()
}

func (h *Handler) glossaryResults(r *http.Request, p *models.Project, languages []string) (glossaryView, error) {
	versions, err := h.glossary.Versions(r.Context(), p)
	if err != nil {
		return glossaryView{}, err
	}
	s := glossarySearchFromRequest(r, languages)
	var selected string
	s.VersionID, selected = selectVersion(r.URL.Query().Get("version"), versions)
	v := glossaryView{Lang: middleware.GetLang(r.Context()), ProjectID: p.ID, ProjectPath: p.Path(), Search: s, Versions: versions, Version: selected}
	if len(languages) == 0 {
		v.Page = &service.GlossaryPage{}
		return v, nil
	}
	if v.Modules, err = h.glossary.Modules(r.Context(), p, s.Language, s.VersionID); err != nil {
		return v, err
	}
	page, err := h.glossary.Search(r.Context(), p, s)
	if err != nil {
		return v, err
	}
	keys := make([]string, len(page.Groups))
	for i, g := range page.Groups {
		keys[i] = g.Hash
	}
	if counts, err := h.discuss.Counts(r.Context(), p, models.SubjectGlossaryTerm, keys); err == nil {
		for _, g := range page.Groups {
			g.Comments = counts[g.Hash]
		}
	}
	v.Page = page
	v.MoreQuery = v.moreQuery()
	return v, nil
}

// GlossaryPage shows the project glossary
func (h *Handler) GlossaryPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	h.renderGlossary(w, r, p, "")
}

// glossarySource is one version in the manage dialog with its imports
type glossarySource struct {
	Version *models.ProjectVersion
	Imports []*models.GlossaryImport
	Terms   int
}

func (h *Handler) renderGlossary(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg string) {
	h.renderGlossaryManage(w, r, p, errMsg, "")
}

// renderGlossaryManage renders the glossary page; manage opens the manage
// dialog on a tab ("list" or "add").
func (h *Handler) renderGlossaryManage(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg, manage string) {
	imports, err := h.glossary.Imports(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	languages, err := h.glossary.Languages(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	results, err := h.glossaryResults(r, p, languages)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.glossary") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "glossary"
	data.Data["Languages"] = languages
	data.Data["Results"] = results
	data.Data["Sync"] = glossarySyncStatus(results.Versions, imports)

	if p.Role.CanManage() {
		byVersion := map[int64][]*models.GlossaryImport{}
		for _, imp := range imports {
			byVersion[imp.VersionID] = append(byVersion[imp.VersionID], imp)
		}
		var sources []glossarySource
		for _, v := range results.Versions {
			src := glossarySource{Version: v, Imports: byVersion[v.ID]}
			for _, imp := range src.Imports {
				src.Terms += imp.TermCount
			}
			sources = append(sources, src)
		}
		data.Data["Sources"] = sources
		if manage == "" {
			manage = r.URL.Query().Get("manage")
		}
		if manage != "list" && manage != "add" {
			manage = ""
		}
		data.Data["Manage"] = manage
		if len(results.Versions) > 0 {
			data.Data["LastName"] = results.Versions[0].Name
		}
		data.Data["FormName"] = r.FormValue("name")
		data.Data["FormVersion"] = r.FormValue("version_name")
		data.Data["FormURL"] = r.FormValue("url")
		data.Data["FormMode"] = r.FormValue("source")
	}
	// errors from the manage dialog are shown inside it
	if data.Data["Manage"] != nil && data.Data["Manage"] != "" {
		data.Data["ManageError"] = errMsg
	} else {
		data.Error = errMsg
	}

	q := r.URL.Query()
	switch q.Get("fetch") {
	case "failed":
		data.Error = i18n.T(data.Lang, "flash.feed_failed", q.Get("label"))
	case "unchanged":
		data.Flash = i18n.T(data.Lang, "flash.feed_unchanged", q.Get("label"))
	}
	if q.Get("imported") != "" {
		data.Flash = i18n.T(data.Lang, "flash.glossary_imported", q.Get("label"), q.Get("imported"), q.Get("files"))
		if s := q.Get("skipped"); s != "" && s != "0" {
			data.Flash += " " + i18n.T(data.Lang, "flash.glossary_skipped", s)
		}
	}
	h.render(w, "glossary", data)
}

// GlossaryAddSource adds a glossary source from the manage dialog: an uploaded
// file or a download URL, under a release name and version.
func (h *Handler) GlossaryAddSource(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	limit := h.serverConfig.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(limit); err != nil {
		h.renderGlossaryManage(w, r, p, i18n.T(middleware.GetLang(r.Context()), "err.upload_too_large", h.serverConfig.MaxUploadMB), "add")
		return
	}

	if r.FormValue("source") == "url" {
		v, out, err := h.glossary.SetFeed(r.Context(), user, p, service.FeedInput{
			Name:     r.FormValue("name"),
			Version:  r.FormValue("version_name"),
			URL:      r.FormValue("url"),
			Language: r.FormValue("language"),
		})
		if err != nil {
			h.renderGlossaryManage(w, r, p, h.errorMessage(r, err), "add")
			return
		}
		h.feedRedirect(w, r, p, v, out)
		return
	}

	name, body, err := h.readUpload(w, r)
	if err == nil {
		var res *service.GlossaryImportResult
		res, err = h.glossary.Import(r.Context(), user, p, service.GlossaryImportInput{
			Name:     r.FormValue("name"),
			Version:  r.FormValue("version_name"),
			Filename: name,
			Data:     body,
			Language: r.FormValue("language"),
		})
		if err == nil {
			h.importRedirect(w, r, p, res)
			return
		}
	}
	h.renderGlossaryManage(w, r, p, h.errorMessage(r, err), "add")
}

func (h *Handler) importRedirect(w http.ResponseWriter, r *http.Request, p *models.Project, res *service.GlossaryImportResult) {
	terms, files := 0, 0
	for _, imp := range res.Imports {
		terms += imp.TermCount
		files += imp.FileCount
	}
	http.Redirect(w, r, p.Path()+fmt.Sprintf("/glossary?imported=%d&files=%d&skipped=%d&lang=%s&version=%d&label=%s",
		terms, files, len(res.Skipped), url.QueryEscape(res.Imports[0].Language), res.Version.ID,
		url.QueryEscape(res.Version.Label())), http.StatusSeeOther)
}

// GlossaryDeleteVersion removes a version from the glossary
func (h *Handler) GlossaryDeleteVersion(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.DeleteVersion(r.Context(), p, idParam(r, "vid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/glossary?flash=version_deleted&manage=list", http.StatusSeeOther)
}

// GlossarySearch renders search results (HTMX)
func (h *Handler) GlossarySearch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	languages, err := h.glossary.Languages(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v, err := h.glossaryResults(r, p, languages)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if v.Search.Offset > 0 {
		h.renderPartials(w, partial{"glossary_page", v})
		return
	}
	h.renderPartials(w, partial{"glossary_results", v})
}

// GlossaryImport uploads translations into the glossary
func (h *Handler) GlossaryImport(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	name, body, err := h.readUpload(w, r)
	if err != nil {
		h.renderGlossary(w, r, p, h.errorMessage(r, err))
		return
	}
	res, err := h.glossary.Import(r.Context(), middleware.GetUser(r.Context()), p, service.GlossaryImportInput{
		Name:     r.FormValue("name"),
		Version:  r.FormValue("version_name"),
		Filename: name,
		Data:     body,
		Language: r.FormValue("language"),
	})
	if err != nil {
		h.renderGlossary(w, r, p, h.errorMessage(r, err))
		return
	}
	h.importRedirect(w, r, p, res)
}

// GlossaryDeleteImport removes an upload from the glossary
func (h *Handler) GlossaryDeleteImport(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.DeleteImport(r.Context(), p, idParam(r, "gid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/glossary?flash=glossary_deleted&manage=list", http.StatusSeeOther)
}

func (h *Handler) feedRedirect(w http.ResponseWriter, r *http.Request, p *models.Project, v *models.ProjectVersion, out *service.FetchOutcome) {
	q := url.Values{"version": {strconv.FormatInt(v.ID, 10)}, "label": {v.Label()}}
	switch {
	case out.Err != nil:
		q.Set("fetch", "failed")
	case out.Changed:
		q.Set("fetch", "updated")
		terms, files := 0, 0
		for _, imp := range out.Result.Imports {
			terms += imp.TermCount
			files += imp.FileCount
		}
		q.Set("imported", strconv.Itoa(terms))
		q.Set("files", strconv.Itoa(files))
		q.Set("lang", out.Result.Imports[0].Language)
	default:
		q.Set("fetch", "unchanged")
	}
	q.Set("manage", "list")
	http.Redirect(w, r, p.Path()+fmt.Sprintf("/glossary?%s", q.Encode()), http.StatusSeeOther)
}

// GlossarySetFeed saves a download URL for a version and fetches it
func (h *Handler) GlossarySetFeed(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	v, out, err := h.glossary.SetFeed(r.Context(), middleware.GetUser(r.Context()), p, service.FeedInput{
		Name:     r.FormValue("name"),
		Version:  r.FormValue("version_name"),
		URL:      r.FormValue("url"),
		Language: r.FormValue("language"),
	})
	if err != nil {
		h.renderGlossaryManage(w, r, p, h.errorMessage(r, err), "list")
		return
	}
	h.feedRedirect(w, r, p, v, out)
}

// GlossaryFetchNow downloads a version's source immediately
func (h *Handler) GlossaryFetchNow(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	vid := idParam(r, "vid")
	out, err := h.glossary.FetchNow(r.Context(), p, vid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	versions, _ := h.glossary.Versions(r.Context(), p)
	for _, v := range versions {
		if v.ID == vid {
			h.feedRedirect(w, r, p, v, out)
			return
		}
	}
	http.Redirect(w, r, p.Path()+"/glossary", http.StatusSeeOther)
}

// GlossaryRemoveFeed stops automatic downloads for a version
func (h *Handler) GlossaryRemoveFeed(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.RemoveFeed(r.Context(), p, idParam(r, "vid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/glossary?flash=feed_removed&manage=list", http.StatusSeeOther)
}

// syncStatus describes how fresh the newest glossary version is
type syncStatus struct {
	Label       string     // newest version with glossary data, e.g. "gnome-51"
	HasFeed     bool       // that version has a sync link
	LastFetched *time.Time // its last download check
	Failed      bool       // that check failed
	LastImport  *time.Time // its most recent import (upload or sync)
}

// glossarySyncStatus always speaks about the newest version that has glossary data
// (versions come newest first), so an older version's sync never hides that the
// current release is only updated by hand.
func glossarySyncStatus(versions []*models.ProjectVersion, imports []*models.GlossaryImport) syncStatus {
	var st syncStatus
	var latest *models.ProjectVersion
	for _, v := range versions {
		if v.ImportCount > 0 {
			latest = v
			break
		}
	}
	if latest == nil {
		return st
	}
	st.Label = latest.Label()
	if latest.HasFeed() {
		st.HasFeed = true
		st.LastFetched, st.Failed = latest.LastFetchedAt, latest.LastFetchError != ""
	}
	for _, imp := range imports {
		if imp.VersionID == latest.ID && (st.LastImport == nil || imp.CreatedAt.After(*st.LastImport)) {
			t := imp.CreatedAt
			st.LastImport = &t
		}
	}
	return st
}
