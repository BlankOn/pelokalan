package web

import (
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
	Groups      []*models.ModuleGroup
	Group       string // selected module group id, or "all"
	Page        *service.GlossaryPage
	MoreQuery   string
	Modules     []repository.ModuleCount
}

// selectGroup resolves the "group" parameter: "all", a module group id, or
// (by default) the newest module group that has glossary data.
func selectGroup(param string, groups []*models.ModuleGroup) (int64, string) {
	if param == "all" {
		return 0, "all"
	}
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		for _, v := range groups {
			if v.ID == id {
				return id, param
			}
		}
	}
	for _, v := range groups {
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
	vals.Set("group", v.Group)
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
	groups, err := h.glossary.Groups(r.Context(), p)
	if err != nil {
		return glossaryView{}, err
	}
	s := glossarySearchFromRequest(r, languages)
	var selected string
	param := r.URL.Query().Get("group")
	if param == "" {
		param = r.URL.Query().Get("version") // links from before the rename
	}
	s.GroupID, selected = selectGroup(param, groups)
	v := glossaryView{Lang: middleware.GetLang(r.Context()), ProjectID: p.ID, ProjectPath: p.Path(), Search: s, Groups: groups, Group: selected}
	if len(languages) == 0 {
		v.Page = &service.GlossaryPage{}
		return v, nil
	}
	if v.Modules, err = h.glossary.Modules(r.Context(), p, s.Language, s.GroupID); err != nil {
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

func (h *Handler) renderGlossary(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg string) {
	imports, err := h.glossary.GroupImports(r.Context(), p)
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
	data.Error = errMsg
	data.Data["Project"] = p
	data.Data["Tab"] = "glossary"
	data.Data["Languages"] = languages
	data.Data["Results"] = results
	data.Data["Sync"] = glossarySyncStatus(results.Groups, imports)
	h.render(w, "glossary", data)
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

// syncStatus describes how fresh the newest module group is
type syncStatus struct {
	Label       string     // newest module group with glossary data, e.g. "gnome-51"
	HasFeed     bool       // that module group has a sync link
	LastFetched *time.Time // its last download check
	Failed      bool       // that check failed
	LastImport  *time.Time // its most recent import (upload or sync)
}

// glossarySyncStatus always speaks about the newest module group that has glossary data
// (groups come newest first), so an older module group's sync never hides that the
// current release is only updated by hand.
func glossarySyncStatus(groups []*models.ModuleGroup, imports []*models.ModuleGroupImport) syncStatus {
	var st syncStatus
	var latest *models.ModuleGroup
	for _, v := range groups {
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
		if imp.GroupID == latest.ID && (st.LastImport == nil || imp.CreatedAt.After(*st.LastImport)) {
			t := imp.CreatedAt
			st.LastImport = &t
		}
	}
	return st
}
