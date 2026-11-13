package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"pelokalan/internal/glossary"
	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Projects lists all projects (public) and the user's pending invitations
func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	h.renderProjects(w, r, "", nil)
}

func (h *Handler) renderProjects(w http.ResponseWriter, r *http.Request, errMsg string, form *service.ProjectInput) {
	user := middleware.GetUser(r.Context())
	projects, err := h.projectService.ListAll(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var mine, others []*models.Project
	for _, p := range projects {
		if p.Role.IsMember() {
			mine = append(mine, p)
		} else {
			others = append(others, p)
		}
	}
	data := h.page(r, "title.projects")
	data.Error = errMsg
	data.Data["Mine"] = mine
	data.Data["Others"] = others
	if user != nil {
		invitations, err := h.projectService.InvitationsForUser(r.Context(), user)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		data.Data["Invitations"] = invitations
	}
	data.Data["ShowNew"] = user != nil && (r.URL.Query().Get("new") == "1" || form != nil)
	if form != nil {
		data.Data["Form"] = *form
	}
	h.render(w, "projects", data)
}

// CreateProject creates a project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	in := service.ProjectInput{
		Name:           r.FormValue("name"),
		Slug:           r.FormValue("slug"),
		Description:    r.FormValue("description"),
		SourceLanguage: r.FormValue("source_language"),
	}
	p, err := h.projectService.Create(r.Context(), user, in)
	if err != nil {
		h.renderProjects(w, r, h.errorMessage(r, err), &in)
		return
	}
	http.Redirect(w, r, p.Path()+"?flash=project_created", http.StatusSeeOther)
}

// loadProject loads a project for the request. Projects are public, so the
// viewer may be a visitor (empty Role); services check roles for changes.
//
// The {id} URL segment is the project slug ("gnome-id"); numeric ids still
// work and page views are redirected to the slug address.
func (h *Handler) loadProject(w http.ResponseWriter, r *http.Request) (*models.Project, bool) {
	user := middleware.GetUser(r.Context())
	param := chi.URLParam(r, "id")
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		p, err := h.projectService.GetPublic(r.Context(), user, id)
		if err != nil {
			h.fail(w, r, err)
			return nil, false
		}
		if r.Method == http.MethodGet && !isHTMX(r) {
			target := strings.Replace(r.URL.EscapedPath(), "/projects/"+param, p.Path(), 1)
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return nil, false
		}
		return p, true
	}
	p, err := h.projectService.GetPublicBySlug(r.Context(), user, param)
	if err != nil {
		h.fail(w, r, err)
		return nil, false
	}
	return p, true
}

// loadMemberProject loads a project the signed-in user belongs to
func (h *Handler) loadMemberProject(w http.ResponseWriter, r *http.Request) (*models.Project, bool) {
	p, ok := h.loadProject(w, r)
	if ok && !p.Role.IsMember() {
		h.fail(w, r, service.ErrForbidden)
		return nil, false
	}
	return p, ok
}

// ownerFiles groups another member's files on the PO files page
type ownerFiles struct {
	Username string
	Files    []*models.Catalog
}

// ProjectPage shows the project's PO files: the user's own first, then the
// other members' files (read-only)
func (h *Handler) ProjectPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	h.renderProject(w, r, p, "")
}

func (h *Handler) renderProject(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg string) {
	catalogs, err := h.catalogService.ListAll(r.Context(), middleware.GetUser(r.Context()), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var template *models.Catalog
	for _, c := range catalogs {
		if c.Owned && c.IsTemplate {
			template = c
		}
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "project.tab_files") + " · " + p.Name
	data.Error = errMsg
	data.Data["Project"] = p
	data.Data["Template"] = template
	h.glossary.AttachUpstreamToCatalogs(r.Context(), p, catalogs)
	data.Data["MyFiles"] = h.fileList(r, p, catalogs, true, "", nil, nil, 0)
	data.Data["MemberFiles"] = h.fileList(r, p, catalogs, false, "", nil, nil, 0)
	data.Data["Tab"] = "files"
	if pf, total, err := h.glossary.ProjectFiles(r.Context(), p, "", nil, nil, 0); err == nil {
		_ = h.catalogService.MatchOwnFiles(r.Context(), middleware.GetUser(r.Context()), p, pf)
		view := projectFilesView{Lang: data.Lang, Project: p, Files: pf, Total: total,
			Next: len(pf), CanTake: p.Role.IsMember(), CanManage: p.Role.CanManage(),
			SyncHours: int(h.syncEvery / time.Hour), Upstream: h.glossary.HasUpstream(r.Context(), p)}
		view.HasFeed, view.LastSync = h.lastSync(r, p)
		view.Groups, _ = h.glossary.GroupsWithFiles(r.Context(), p)
		data.Data["ProjectFiles"] = view
	}
	if msg := r.URL.Query().Get("merged"); msg != "" {
		data.Flash = i18n.T(data.Lang, "flash.merged", r.URL.Query().Get("added"), r.URL.Query().Get("obsolete"))
		if n := r.URL.Query().Get("skipped"); n != "" && n != "0" {
			data.Flash += " " + i18n.T(data.Lang, "flash.merged_skipped", n)
		}
	}
	h.render(w, "project", data)
}

// UploadCatalog imports a PO/POT file into the project
func (h *Handler) UploadCatalog(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	name, body, err := h.readUpload(w, r)
	var page string
	if err == nil {
		page, err = h.glossary.UpstreamPageURL(r.FormValue("upstream_page"))
	}
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	c, res, err := h.catalogService.Upload(r.Context(), user, p, name, body, r.FormValue("language"))
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	if u := uploadedURL(r); u != "" && res == nil && c != nil {
		_ = h.catalogService.SetSourceURL(r.Context(), user, c, u)
	}
	if page != "" && res == nil && c != nil && !c.IsTemplate {
		h.setUpstreamPage(r, c, page)
	}
	if res != nil {
		http.Redirect(w, r, p.Path()+fmt.Sprintf("/files?merged=1&added=%d&obsolete=%d&skipped=%d", res.Added, res.Obsolete, res.Skipped), http.StatusSeeOther)
		return
	}
	if c.IsTemplate {
		http.Redirect(w, r, p.Path()+"/files?flash=template_uploaded", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, c.URL(), http.StatusSeeOther)
}

func (h *Handler) readUpload(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	limit := h.serverConfig.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(limit); err != nil {
		return "", nil, &service.UserError{Key: "err.upload_too_large", Args: []any{h.serverConfig.MaxUploadMB}}
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		// no file chosen: download it from the given URL instead
		if u := strings.TrimSpace(r.FormValue("url")); u != "" {
			return h.downloadPO(r, u)
		}
		return "", nil, &service.UserError{Key: "err.no_file"}
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", nil, err
	}
	if int64(len(body)) > limit {
		return "", nil, &service.UserError{Key: "err.upload_too_large", Args: []any{h.serverConfig.MaxUploadMB}}
	}
	return hdr.Filename, body, nil
}

// setUpstreamPage stores a file's page on the GNOME translation site and fetches its state in
// the background (one request), so the badge is there when the file list is next shown
func (h *Handler) setUpstreamPage(r *http.Request, c *models.Catalog, page string) {
	if err := h.catalogService.SetUpstreamPage(r.Context(), middleware.GetUser(r.Context()), c, page); err != nil || page == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h.glossary.CheckUpstreamPage(ctx, page)
	}()
}

// uploadedURL is the link readUpload downloaded from ("" when a file was chosen)
func uploadedURL(r *http.Request) string {
	if r.MultipartForm != nil && len(r.MultipartForm.File["file"]) > 0 {
		return ""
	}
	return strings.TrimSpace(r.FormValue("url"))
}

// CreateTranslation starts a new language from the template
func (h *Handler) CreateTranslation(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	lang := r.FormValue("language")
	if lang == "other" {
		lang = r.FormValue("language_other")
	}
	c, err := h.catalogService.CreateFromTemplate(r.Context(), middleware.GetUser(r.Context()), p, lang)
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	http.Redirect(w, r, c.URL(), http.StatusSeeOther)
}

// UpdateAllFromTemplate refreshes every translation from the template
func (h *Handler) UpdateAllFromTemplate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	res, err := h.catalogService.UpdateAllFromTemplate(r.Context(), middleware.GetUser(r.Context()), p)
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	http.Redirect(w, r, p.Path()+fmt.Sprintf("/files?merged=1&added=%d&obsolete=%d&skipped=%d", res.Added, res.Obsolete, res.Skipped), http.StatusSeeOther)
}

// MembersPage shows members and invitations
func (h *Handler) MembersPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	h.renderMembers(w, r, p, "", "")
}

func (h *Handler) renderMembers(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg, email string) {
	members, err := h.projectService.Members(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// pending invitations reveal email addresses: managers only
	var invitations []*models.Invitation
	if p.Role.CanManage() {
		if invitations, err = h.projectService.PendingInvitations(r.Context(), p); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.members") + " · " + p.Name
	data.Error = errMsg
	data.Data["Project"] = p
	data.Data["Members"] = members
	data.Data["Invitations"] = invitations
	data.Data["InviteEmail"] = email
	if p.Role.CanManage() {
		links, err := h.projectService.InviteLinks(r.Context(), p)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		data.Data["InviteLinks"] = links
		data.Data["LinkDays"] = service.InviteLinkDays
		data.Data["LinkUses"] = service.InviteLinkUses
		data.Data["SiteURL"] = strings.TrimRight(h.siteConfig.BaseURL, "/")
		data.Data["NewLink"] = r.URL.Query().Get("link")
	}
	data.Data["Tab"] = "members"
	h.render(w, "members", data)
}

// Invite sends an invitation
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	_, err := h.projectService.Invite(r.Context(), user, p, r.FormValue("email"),
		models.ProjectRole(r.FormValue("role")), middleware.GetLang(r.Context()))
	if err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), r.FormValue("email"))
		return
	}
	http.Redirect(w, r, p.Path()+"/members?flash=invited", http.StatusSeeOther)
}

// CancelInvitation withdraws an invitation
func (h *Handler) CancelInvitation(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.projectService.CancelInvitation(r.Context(), p, idParam(r, "iid")); err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/members?flash=invitation_cancelled", http.StatusSeeOther)
}

// ChangeMemberRole updates a member's role
func (h *Handler) ChangeMemberRole(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	err := h.projectService.ChangeRole(r.Context(), p, idParam(r, "uid"), models.ProjectRole(r.FormValue("role")))
	if err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/members?flash=role_changed", http.StatusSeeOther)
}

// RemoveMember removes a member or lets a member leave
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	uid := idParam(r, "uid")
	if err := h.projectService.RemoveMember(r.Context(), user, p, uid); err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), "")
		return
	}
	if uid == user.ID {
		http.Redirect(w, r, "/projects?flash=left_project", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, p.Path()+"/members?flash=member_removed", http.StatusSeeOther)
}

// SettingsPage shows project settings
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	// every member manages their email notifications here; the project settings below
	// them are for owners and managers only
	h.renderSettings(w, r, p, "", "")
}

// UpdateSettings saves project settings
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	in := service.ProjectInput{
		Name:           r.FormValue("name"),
		Slug:           r.FormValue("slug"),
		Description:    r.FormValue("description"),
		SourceLanguage: r.FormValue("source_language"),
		Language:       r.FormValue("language"),
		Open:           r.FormValue("access") == "open",
	}
	if err := h.projectService.Update(r.Context(), p, in); err != nil {
		h.renderSettings(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/settings?flash=settings_saved", http.StatusSeeOther)
}

// JoinProject adds the signed-in user to an open project (or accepts their pending invitation)
// and returns to the page the button was on
func (h *Handler) JoinProject(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.projectService.JoinOpen(r.Context(), middleware.GetUser(r.Context()), p); err != nil {
		h.fail(w, r, err)
		return
	}
	next := p.Path()
	if u, err := url.Parse(safeNext(r.FormValue("next"), p.Path())); err == nil {
		q := u.Query()
		q.Set("flash", "joined")
		u.RawQuery = q.Encode()
		next = u.String()
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// DeleteProject removes a project
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if r.FormValue("confirm_name") != p.Name {
		h.renderSettings(w, r, p, i18n.T(middleware.GetLang(r.Context()), "err.confirm_name"), "")
		return
	}
	if err := h.projectService.Delete(r.Context(), p); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/projects?flash=project_deleted", http.StatusSeeOther)
}

// InvitationsPage lists invitations addressed to the user
func (h *Handler) InvitationsPage(w http.ResponseWriter, r *http.Request) {
	invitations, err := h.projectService.InvitationsForUser(r.Context(), middleware.GetUser(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "title.invitations")
	data.Data["Invitations"] = invitations
	h.render(w, "invitations", data)
}

// AcceptInvitation joins a project
func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	inv, err := h.projectService.AcceptInvitation(r.Context(), middleware.GetUser(r.Context()), idParam(r, "iid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(inv.ProjectID, 10)+"?flash=joined", http.StatusSeeOther)
}

// DeclineInvitation rejects an invitation
func (h *Handler) DeclineInvitation(w http.ResponseWriter, r *http.Request) {
	if err := h.projectService.DeclineInvitation(r.Context(), middleware.GetUser(r.Context()), idParam(r, "iid")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/invitations?flash=declined", http.StatusSeeOther)
}

// CreateInviteLink makes a shareable join link (owners and managers)
func (h *Handler) CreateInviteLink(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.FormValue("days"))
	uses, _ := strconv.Atoi(r.FormValue("max_uses"))
	l, err := h.projectService.CreateInviteLink(r.Context(), middleware.GetUser(r.Context()), p,
		models.ProjectRole(r.FormValue("role")), days, uses)
	if err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/members?flash=link_created&link=%d#invite-links", p.Path(), l.ID), http.StatusSeeOther)
}

// DeleteInviteLink revokes a join link
func (h *Handler) DeleteInviteLink(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if err := h.projectService.DeleteInviteLink(r.Context(), p, idParam(r, "lid")); err != nil {
		h.renderMembers(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/members?flash=link_deleted#invite-links", http.StatusSeeOther)
}

// JoinPage shows what a join link offers; visitors are asked to sign in or register first
func (h *Handler) JoinPage(w http.ResponseWriter, r *http.Request) {
	h.renderJoin(w, r, "")
}

func (h *Handler) renderJoin(w http.ResponseWriter, r *http.Request, errMsg string) {
	token := chi.URLParam(r, "token")
	l, p, err := h.projectService.InviteLink(r.Context(), middleware.GetUser(r.Context()), token)
	data := h.page(r, "title.join")
	data.Error = errMsg
	if err != nil {
		if !errors.Is(err, service.ErrInviteLinkInvalid) {
			h.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		data.Data["Invalid"] = true
		h.render(w, "join", data)
		return
	}
	data.Title = i18n.T(data.Lang, "title.join") + " · " + p.Name
	data.Data["Link"] = l
	data.Data["Project"] = p
	data.Data["Next"] = l.Path()
	h.render(w, "join", data)
}

// Join adds the signed-in user to the project of a join link
func (h *Handler) Join(w http.ResponseWriter, r *http.Request) {
	p, err := h.projectService.JoinByInviteLink(r.Context(), middleware.GetUser(r.Context()), chi.URLParam(r, "token"))
	if err != nil {
		if _, ok := service.AsUserError(err); ok {
			h.renderJoin(w, r, h.errorMessage(r, err))
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"?flash=joined", http.StatusSeeOther)
}

// downloadPO fetches a PO/POT file from a public URL (private and local addresses are
// refused) with the same size limit as uploads
func (h *Handler) downloadPO(r *http.Request, rawURL string) (string, []byte, error) {
	if h.poFetcher == nil {
		return "", nil, &service.UserError{Key: "err.no_file"}
	}
	if _, err := glossary.ValidateURL(rawURL); err != nil {
		return "", nil, &service.UserError{Key: "err.po_url"}
	}
	res, err := h.poFetcher.Fetch(r.Context(), rawURL, "", "")
	if errors.Is(err, glossary.ErrTooLarge) {
		return "", nil, &service.UserError{Key: "err.upload_too_large", Args: []any{h.serverConfig.MaxUploadMB}}
	}
	if err != nil {
		log.Printf("PO download %s: %v", rawURL, err)
		switch msg := err.Error(); {
		case strings.Contains(msg, "address not allowed"):
			return "", nil, &service.UserError{Key: "err.po_private"}
		case strings.HasPrefix(msg, "HTTP "):
			return "", nil, &service.UserError{Key: "err.po_download", Args: []any{msg}}
		default:
			return "", nil, &service.UserError{Key: "err.po_unreachable"}
		}
	}
	return res.Filename, res.Data, nil
}

// projectFilesView is a page of the project's source PO files
type projectFilesView struct {
	Lang      string
	Project   *models.Project
	Files     []*models.ProjectFile
	Total     int
	Next      int // offset of the next page
	CanTake   bool
	Query     string                      // search words (kept by "Muat lagi")
	Searched  bool                        // a search result: say so when nothing matches
	CanManage bool                        // may start a sync
	HasFeed   bool                        // some module group has a sync link
	LastSync  *time.Time                  // newest download check of those module groups
	SyncHours int                         // automatic sync interval
	Groups    []*models.ModuleGroupOption // filter options
	Upstream  bool                        // some module group is a release upstream: show the state filter
}

// ProjectFilesMore returns the next page of project files (htmx "Muat lagi")
func (h *Handler) ProjectFilesMore(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := r.URL.Query().Get("q")
	var groupIDs []int64
	groupIDs = groupParams(r)
	pf, total, err := h.glossary.ProjectFiles(r.Context(), p, q, groupIDs, models.UpstreamFilterSet(r.URL.Query()["u"]), offset)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	_ = h.catalogService.MatchOwnFiles(r.Context(), middleware.GetUser(r.Context()), p, pf)
	h.renderPartials(w, partial{"project_file_rows", projectFilesView{Lang: middleware.GetLang(r.Context()), Project: p,
		Files: pf, Total: total, Next: offset + len(pf), CanTake: p.Role.IsMember(), Query: q, Searched: true, CanManage: p.Role.CanManage()}})
}

// ProjectFileDownload sends a source PO file as it came from the glossary source
func (h *Handler) ProjectFileDownload(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	f, data, err := h.glossary.ProjectFile(r.Context(), p, idParam(r, "fid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-gettext-translation; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Filename}))
	_, _ = w.Write(data)
}

// ProjectFileTake copies a source PO file into the member's own files and opens it
func (h *Handler) ProjectFileTake(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	f, data, err := h.glossary.ProjectFile(r.Context(), p, idParam(r, "fid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	user := middleware.GetUser(r.Context())
	label := f.GroupLabel
	// the current PO file on the GNOME translation site when it has one (newer than the release
	// archive); otherwise, or when the site cannot be reached, the copy saved at the last sync
	if latest, from, err := h.glossary.UpstreamPO(r.Context(), p, f, h.serverConfig.MaxUploadMB<<20); err == nil {
		data, label = latest, from
	}
	// "Perbarui": replace the member's own file of this module (confirmed in the browser)
	if into, _ := strconv.ParseInt(r.FormValue("into"), 10, 64); into > 0 {
		c, cp, err := h.catalogService.Get(r.Context(), user, into)
		if err == nil && cp.ID != p.ID {
			err = service.ErrNotFound
		}
		if err == nil {
			err = h.catalogService.UpdateFromUpstream(r.Context(), user, c, label, data)
		}
		if err != nil {
			h.renderProject(w, r, p, h.errorMessage(r, err))
			return
		}
		http.Redirect(w, r, c.URL()+"?flash=upstream_updated", http.StatusSeeOther)
		return
	}
	c, res, err := h.catalogService.Upload(r.Context(), user, p, f.Filename, data, f.Language)
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	if res != nil { // merged into the member's existing file of that language
		http.Redirect(w, r, p.Path()+fmt.Sprintf("/files?merged=1&added=%d&obsolete=%d&skipped=%d", res.Added, res.Obsolete, res.Skipped), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, c.URL(), http.StatusSeeOther)
}

// ProjectFilesSync downloads the glossary sources again (in the background), refreshing project files
func (h *Handler) ProjectFilesSync(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	err := h.glossary.SyncAll(r.Context(), p)
	if r.Header.Get("X-Sync") != "" { // app.js waits for the result (button spins)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": h.errorMessage(r, err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	if err != nil {
		h.renderProject(w, r, p, h.errorMessage(r, err))
		return
	}
	http.Redirect(w, r, p.Path()+"/files?flash=sync_started", http.StatusSeeOther)
}

// ProjectFilesSyncStatus reports the running Sync (JSON, polled by app.js) with a message when done
func (h *Handler) ProjectFilesSyncStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	lang := middleware.GetLang(r.Context())
	st := h.glossary.SyncState(p)
	resp := map[string]any{"running": false}
	if _, last := h.lastSync(r, p); last != nil {
		resp["last"] = last.Format(time.RFC3339)
		resp["last_ago"] = timeAgo(lang, *last)
	}
	if st != nil {
		resp["running"] = st.Running
		resp["changed"] = len(st.Changed)
		switch {
		case st.Running:
			resp["message"] = i18n.T(lang, "files.sync_progress", st.Done, st.Groups)
		case len(st.Failed) > 0:
			resp["error"] = i18n.T(lang, "files.sync_failed", strings.Join(st.Failed, "; "))
			if len(st.Changed) > 0 {
				resp["message"] = i18n.T(lang, "files.sync_updated", strings.Join(st.Changed, ", "))
			}
		case len(st.Changed) > 0:
			resp["message"] = i18n.T(lang, "files.sync_updated", strings.Join(st.Changed, ", "))
		} // nothing changed: no message (the refreshed rows and sync time say enough)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// lastSync reports whether any module group has a sync link and when one was last checked
func (h *Handler) lastSync(r *http.Request, p *models.Project) (bool, *time.Time) {
	groups, err := h.glossary.Groups(r.Context(), p)
	if err != nil {
		return false, nil
	}
	has := false
	var last *time.Time
	for _, v := range groups {
		if !v.HasFeed() {
			continue
		}
		has = true
		if v.LastFetchedAt != nil && (last == nil || v.LastFetchedAt.After(*last)) {
			last = v.LastFetchedAt
		}
	}
	return has, last
}

// FileListPageSize is how many of the user's / other members' files a page shows
const FileListPageSize = 5

// fileListView is a page of "Berkas Anda" (Mine) or "Berkas anggota lain"
type fileListView struct {
	Lang        string
	Project     *models.Project
	Mine        bool
	Files       []*models.Catalog
	Total       int // matching the search and filter
	All         int // without search and filter
	Next        int // offset of the next page
	CanManage   bool
	HasTemplate bool
	Groups      []*models.ModuleGroupOption // module group filter options
	Upstream    bool                        // some module group is a release upstream: show the state filter
	Searched    bool
}

// fileList filters a project's PO files: the user's own (mine) or the other members'.
// q: every word must appear in the file name, language or owner; groups: module groups
// (module groups whose project files contain the file's module), 0 = no group;
// states: upstream states (models.UpstreamFilters, nil = all). all must carry their
// upstream state (GlossaryService.AttachUpstreamToCatalogs).
func (h *Handler) fileList(r *http.Request, p *models.Project, all []*models.Catalog, mine bool, q string, groups []int64, states map[string]bool, offset int) fileListView {
	v := fileListView{Lang: middleware.GetLang(r.Context()), Project: p, Mine: mine, CanManage: p.Role.IsMember(),
		Searched: q != "" || len(groups) > 0 || len(states) > 0, Upstream: h.glossary.HasUpstream(r.Context(), p)}
	for _, c := range all { // a file with its own upstream page has a state too
		if c.UpstreamPage != "" {
			v.Upstream = true
			break
		}
	}
	v.Groups, _ = h.glossary.GroupsWithFiles(r.Context(), p)
	moduleGroups, _ := h.glossary.ModuleGroups(r.Context(), p)
	want := map[int64]bool{}
	for _, g := range groups {
		want[g] = true
	}
	words := strings.Fields(strings.ToLower(q))
	var list []*models.Catalog
	for _, c := range all {
		if mine {
			if !c.Owned {
				continue
			}
			if c.IsTemplate {
				v.HasTemplate = true
				continue
			}
		} else if c.Owned {
			continue
		}
		v.All++
		hay := strings.ToLower(c.Filename + " " + c.Language + " " + c.LanguageName() + " @" + c.OwnerUsername)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok && len(want) > 0 {
			in := moduleGroups[service.ModuleOf(c)]
			ok = len(in) == 0 && want[0]
			for _, g := range in {
				if want[g] {
					ok = true
				}
			}
		}
		if ok && len(states) > 0 {
			ok = states[c.UpstreamClass()]
		}
		if ok {
			list = append(list, c)
		}
	}
	// most recently edited first (entry saves bump catalogs.updated_at), so the order is stable
	// between pages and the file someone just worked on is on top. The user's own files whose
	// upstream state needs them come first: requested rework, proofreading, translating.
	sort.SliceStable(list, func(i, j int) bool {
		if mine {
			if a, b := ownFileRank(list[i]), ownFileRank(list[j]); a != b {
				return a < b
			}
		}
		if !list[i].UpdatedAt.Equal(list[j].UpdatedAt) {
			return list[i].UpdatedAt.After(list[j].UpdatedAt)
		}
		return list[i].ID > list[j].ID
	})
	v.Total = len(list)
	if offset < 0 || offset > len(list) {
		offset = 0
	}
	end := min(offset+FileListPageSize, len(list))
	v.Files = list[offset:end]
	v.Next = end
	return v
}

// ownFileRanks orders "Berkas Anda" by upstream state first; other states follow (ownFileRank 3)
var ownFileRanks = map[string]int{"to_review": 0, "proofreading": 1, "translating": 2}

func ownFileRank(c *models.Catalog) int {
	if r, ok := ownFileRanks[c.UpstreamClass()]; ok {
		return r
	}
	return len(ownFileRanks)
}

func groupParams(r *http.Request) []int64 {
	var out []int64
	for _, g := range r.URL.Query()["g"] {
		if id, err := strconv.ParseInt(g, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// MyFiles / MemberFiles return a page of rows for "Muat lagi", search and filter (htmx)
func (h *Handler) MyFiles(w http.ResponseWriter, r *http.Request)     { h.fileRows(w, r, true) }
func (h *Handler) MemberFiles(w http.ResponseWriter, r *http.Request) { h.fileRows(w, r, false) }

func (h *Handler) fileRows(w http.ResponseWriter, r *http.Request, mine bool) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	all, err := h.catalogService.ListAll(r.Context(), middleware.GetUser(r.Context()), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	h.glossary.AttachUpstreamToCatalogs(r.Context(), p, all)
	h.renderPartials(w, partial{"file_rows", h.fileList(r, p, all, mine, strings.TrimSpace(r.URL.Query().Get("q")), groupParams(r),
		models.UpstreamFilterSet(r.URL.Query()["u"]), offset)})
}

// SaveNotifications stores the member's email notification choices for the project
func (h *Handler) SaveNotifications(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	on := map[string]bool{}
	for _, k := range models.NotifyKinds {
		on[k] = r.FormValue("n_"+k) == "1"
	}
	if err := h.discuss.SaveNotificationSettings(r.Context(), middleware.GetUser(r.Context()), p, r.FormValue("muted") == "1", on); err != nil {
		h.renderSettings(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/settings?flash=notifications_saved#notifications", http.StatusSeeOther)
}

// ProjectHome is the project's landing page: Penerjemahan (/projects/{id}/files).
// The query (e.g. ?flash=) is kept.
func (h *Handler) ProjectHome(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	target := p.Path() + "/files"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusFound)
}
