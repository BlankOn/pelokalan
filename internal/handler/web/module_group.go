package web

import (
	"net/http"
	"net/url"
	"strconv"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Module groups ("Kelompok modul", e.g. gnome-51): the PO archives imported per language, by
// upload or from a sync link. They feed the glossary and the project files ("Berkas proyek").
// Managed by owners and managers in a popup on the Settings tab (section #module-groups).

// moduleGroupSource is one module group in the popup with its imports
type moduleGroupSource struct {
	Group   *models.ModuleGroup
	Imports []*models.ModuleGroupImport
	Terms   int
}

// moduleGroupsPath is where the popup lives; open picks its tab ("list" or "add")
func moduleGroupsPath(p *models.Project, q url.Values, open string) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set("groups", open)
	return p.Path() + "/settings?" + q.Encode() + "#module-groups"
}

// moduleGroupsData fills what the settings page needs for the popup. open reopens it on a tab
// ("list" or "add"); errMsg is shown inside it.
func (h *Handler) moduleGroupsData(r *http.Request, p *models.Project, data *PageData, errMsg, open string) {
	groups, err := h.glossary.Groups(r.Context(), p)
	if err != nil {
		return
	}
	imports, _ := h.glossary.GroupImports(r.Context(), p)
	byGroup := map[int64][]*models.ModuleGroupImport{}
	for _, imp := range imports {
		byGroup[imp.GroupID] = append(byGroup[imp.GroupID], imp)
	}
	var sources []moduleGroupSource
	for _, g := range groups {
		src := moduleGroupSource{Group: g, Imports: byGroup[g.ID]}
		for _, imp := range src.Imports {
			src.Terms += imp.TermCount
		}
		sources = append(sources, src)
	}
	data.Data["Sources"] = sources
	if open == "" {
		open = r.URL.Query().Get("groups")
	}
	if open != "list" && open != "add" {
		open = ""
	}
	data.Data["GroupsOpen"] = open
	if len(groups) > 0 {
		data.Data["LastName"] = groups[0].Name
	}
	data.Data["FormName"] = r.FormValue("name")
	data.Data["FormVersion"] = r.FormValue("version_name")
	data.Data["FormURL"] = r.FormValue("url")
	data.Data["FormMode"] = r.FormValue("source")
	if open != "" {
		data.Data["GroupsError"] = errMsg
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
}

// renderModuleGroups shows the settings page with the popup open on a tab and an error inside it
func (h *Handler) renderModuleGroups(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg, open string) {
	h.renderSettingsWith(w, r, p, "", "", func(data *PageData) { h.moduleGroupsData(r, p, data, errMsg, open) })
}

// AddModuleGroupSource adds an archive to a module group from the popup: an uploaded file or a
// sync link, under a group name and version
func (h *Handler) AddModuleGroupSource(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	limit := h.serverConfig.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(limit); err != nil {
		h.renderModuleGroups(w, r, p, i18n.T(middleware.GetLang(r.Context()), "err.upload_too_large", h.serverConfig.MaxUploadMB), "add")
		return
	}

	if r.FormValue("source") == "url" {
		g, out, err := h.glossary.SetGroupSync(r.Context(), user, p, service.GroupSyncInput{
			Name:     r.FormValue("name"),
			Version:  r.FormValue("version_name"),
			URL:      r.FormValue("url"),
			Language: r.FormValue("language"),
		})
		if err != nil {
			h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "add")
			return
		}
		h.syncRedirect(w, r, p, g, out)
		return
	}

	name, body, err := h.readUpload(w, r)
	if err == nil {
		var res *service.ModuleGroupImportResult
		res, err = h.glossary.ImportGroup(r.Context(), user, p, service.ModuleGroupImportInput{
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
	h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "add")
}

// ImportModuleGroup uploads an archive into a module group (without the popup's sync option)
func (h *Handler) ImportModuleGroup(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	name, body, err := h.readUpload(w, r)
	if err != nil {
		h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "add")
		return
	}
	res, err := h.glossary.ImportGroup(r.Context(), middleware.GetUser(r.Context()), p, service.ModuleGroupImportInput{
		Name:     r.FormValue("name"),
		Version:  r.FormValue("version_name"),
		Filename: name,
		Data:     body,
		Language: r.FormValue("language"),
	})
	if err != nil {
		h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "add")
		return
	}
	h.importRedirect(w, r, p, res)
}

func (h *Handler) importRedirect(w http.ResponseWriter, r *http.Request, p *models.Project, res *service.ModuleGroupImportResult) {
	terms, files := 0, 0
	for _, imp := range res.Imports {
		terms += imp.TermCount
		files += imp.FileCount
	}
	q := url.Values{"imported": {strconv.Itoa(terms)}, "files": {strconv.Itoa(files)},
		"skipped": {strconv.Itoa(len(res.Skipped))}, "label": {res.Group.Label()}}
	http.Redirect(w, r, moduleGroupsPath(p, q, "list"), http.StatusSeeOther)
}

func (h *Handler) syncRedirect(w http.ResponseWriter, r *http.Request, p *models.Project, g *models.ModuleGroup, out *service.FetchOutcome) {
	q := url.Values{"label": {g.Label()}}
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
	default:
		q.Set("fetch", "unchanged")
	}
	http.Redirect(w, r, moduleGroupsPath(p, q, "list"), http.StatusSeeOther)
}

// DeleteModuleGroup removes a module group with its imports
func (h *Handler) DeleteModuleGroup(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.DeleteGroup(r.Context(), p, idParam(r, "gid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, moduleGroupsPath(p, url.Values{"flash": {"version_deleted"}}, "list"), http.StatusSeeOther)
}

// DeleteModuleGroupImport removes one imported archive (one language) from a module group
func (h *Handler) DeleteModuleGroupImport(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.DeleteGroupImport(r.Context(), p, idParam(r, "iid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, moduleGroupsPath(p, url.Values{"flash": {"glossary_deleted"}}, "list"), http.StatusSeeOther)
}

// SetModuleGroupSync saves a module group's sync link and fetches it
func (h *Handler) SetModuleGroupSync(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	g, out, err := h.glossary.SetGroupSync(r.Context(), middleware.GetUser(r.Context()), p, service.GroupSyncInput{
		Name:     r.FormValue("name"),
		Version:  r.FormValue("version_name"),
		URL:      r.FormValue("url"),
		Language: r.FormValue("language"),
	})
	if err != nil {
		h.renderModuleGroups(w, r, p, h.errorMessage(r, err), "list")
		return
	}
	h.syncRedirect(w, r, p, g, out)
}

// FetchModuleGroup downloads a module group's sync link now
func (h *Handler) FetchModuleGroup(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	gid := idParam(r, "gid")
	out, err := h.glossary.FetchGroupNow(r.Context(), p, gid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	groups, _ := h.glossary.Groups(r.Context(), p)
	for _, g := range groups {
		if g.ID == gid {
			h.syncRedirect(w, r, p, g, out)
			return
		}
	}
	http.Redirect(w, r, moduleGroupsPath(p, nil, "list"), http.StatusSeeOther)
}

// RemoveModuleGroupSync stops the automatic downloads of a module group
func (h *Handler) RemoveModuleGroupSync(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.glossary.RemoveGroupSync(r.Context(), p, idParam(r, "gid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, moduleGroupsPath(p, url.Values{"flash": {"feed_removed"}}, "list"), http.StatusSeeOther)
}
