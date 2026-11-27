package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"pelokalan/internal/account"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/gz"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/platform/mailer"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/project"
	"pelokalan/internal/repository"
	"pelokalan/internal/reservation"
	"pelokalan/internal/source"
	"pelokalan/pkg/githubapp"
	"pelokalan/pkg/gitpatch"
	"pelokalan/pkg/po"
)

// Reviews of modules for repositories Pelokalan writes to (full GitHub integration). A member asks for a
// review of their file; an owner or manager other than them approves it, and Pelokalan commits the file's
// changes to the module group's branch through the GitHub App (author: the translator, Reviewed-by: the
// reviewer), or sends it back with a note. Owners and managers can also send a file straight away (PushNow).
// What is sent is what GitPatch would send, applied to the branch's current file, minus strings Pelokalan
// already committed, so newer repository translations are never overwritten by old ones.

// MaxReviewNote is the longest note on a request or a rejection
const MaxReviewNote = 2000

// ReviewService handles review requests and commits to repositories
type ReviewService struct {
	repo         repository.ReviewRepository
	catalogs     *CatalogService
	glossary     *source.Service
	reservations *reservation.Service
	projects     project.Repository
	users        account.UserRepository
	mailer       mailer.Mailer
	site         config.SiteConfig
	discuss      *DiscussionService // review comments (nil = none)
}

// SetDiscussions connects review comments: comments by reviewers on a file waiting for review stay open
// until resolved, and approval waits for them
func (s *ReviewService) SetDiscussions(d *DiscussionService) {
	s.discuss = d
	d.SetReviews(s.OpenFor)
}

// OpenFor is the open review a comment by user on the file belongs to: the file waits for review and the
// user is an owner, coordinator or reviewer who did not ask for it (nil = a plain comment)
func (s *ReviewService) OpenFor(ctx context.Context, p *models.Project, catalogID int64, user *models.User) *int64 {
	if s == nil || !p.Role.CanReview() {
		return nil
	}
	latest, err := s.repo.Latest(ctx, []int64{catalogID})
	if err != nil {
		return nil
	}
	if r := latest[catalogID]; r != nil && r.Status == models.ReviewRequested && r.RequestedBy != user.ID {
		return &r.ID
	}
	return nil
}

// openComments counts a file's open review comments
func (s *ReviewService) openComments(ctx context.Context, p *models.Project, catalogID int64) int {
	if s.discuss == nil {
		return 0
	}
	n := 0
	for _, c := range s.discuss.OpenReviewComments(ctx, p, catalogID) {
		n += c
	}
	return n
}

// NewReviewService creates the review service
func NewReviewService(repo repository.ReviewRepository, catalogs *CatalogService, glossary *source.Service,
	reservations *reservation.Service, projects project.Repository, users account.UserRepository,
	mailer mailer.Mailer, site config.SiteConfig) *ReviewService {
	return &ReviewService{repo: repo, catalogs: catalogs, glossary: glossary, reservations: reservations,
		projects: projects, users: users, mailer: mailer, site: site}
}

// DirectGroup is the module group a member's file is committed to (nil = its repository is not written by Pelokalan)
func (s *ReviewService) DirectGroup(ctx context.Context, p *models.Project, c *models.Catalog) *models.ModuleGroup {
	if s == nil || c == nil || c.RepoPath == "" || c.IsTemplate || s.glossary.GitHubApp() == nil {
		return nil
	}
	if g := s.glossary.GitGroupFor(ctx, p, c.RepoPath); g != nil && g.CanPush() {
		return g
	}
	return nil
}

// Attach fills Review and CanPush of the files
func (s *ReviewService) Attach(ctx context.Context, p *models.Project, catalogs []*models.Catalog) {
	if s == nil || s.glossary.GitHubApp() == nil {
		return
	}
	var ids []int64
	for _, c := range catalogs {
		if c.RepoPath != "" {
			c.CanPush = s.DirectGroup(ctx, p, c) != nil
			ids = append(ids, c.ID)
		}
	}
	latest, err := s.repo.Latest(ctx, ids)
	if err != nil {
		log.Printf("reviews: latest: %v", err)
		return
	}
	for _, c := range catalogs {
		c.Review = latest[c.ID]
		if c.Review != nil && c.Review.Status == models.ReviewRequested {
			c.Review.OpenComments = s.openComments(ctx, p, c.ID)
		}
	}
}

// Open lists the project's requests waiting for review (owners, coordinators and reviewers)
func (s *ReviewService) Open(ctx context.Context, p *models.Project) ([]*models.ModuleReview, error) {
	if s == nil || !p.Role.CanReview() {
		return nil, nil
	}
	list, err := s.repo.Open(ctx, p.ID)
	for _, r := range list {
		r.OpenComments = s.openComments(ctx, p, r.CatalogID)
	}
	return list, err
}

// Get returns a review of the project
func (s *ReviewService) Get(ctx context.Context, p *models.Project, id int64) (*models.ModuleReview, error) {
	r, err := s.repo.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) || err == nil && r.ProjectID != p.ID {
		return nil, apperr.ErrNotFound
	}
	return r, err
}

// lastSent is the repository file as Pelokalan last committed it for the catalog (nil = never)
func (s *ReviewService) lastSent(ctx context.Context, catalogID int64) []byte {
	data, err := s.repo.LastPushed(ctx, catalogID)
	if err != nil || data == nil {
		return nil
	}
	out, err := gz.Decompress(data)
	if err != nil {
		return nil
	}
	return out
}

// Changes is what a review would send, against the repository file as last synced (for the review page)
func (s *ReviewService) Changes(ctx context.Context, p *models.Project, c *models.Catalog) (*gitpatch.FileDiff, error) {
	pf, _, err := s.glossary.ProjectFileByRepoPath(ctx, p.ID, c.RepoPath)
	if err != nil {
		return nil, err
	}
	base, err := gz.Decompress(pf.Data)
	if err != nil {
		return nil, err
	}
	want, err := s.catalogs.ChangedTargets(ctx, c, s.lastSent(ctx, c.ID))
	if err != nil {
		return nil, err
	}
	var credit *gitpatch.Credit
	if owner, err := s.users.GetByID(ctx, c.OwnerID); err == nil {
		credit = TranslatorCredit(owner)
	}
	return gitpatch.DiffCredit(c.RepoPath, base, want, credit), nil
}

// ChangedHashes are the key hashes of the entries a review of the file would send (the review editor lists them)
func (s *ReviewService) ChangedHashes(ctx context.Context, c *models.Catalog) map[string]bool {
	out := map[string]bool{}
	want, err := s.catalogs.ChangedTargets(ctx, c, s.lastSent(ctx, c.ID))
	if err != nil {
		return out
	}
	for k := range want {
		out[models.KeyHashOf(k)] = true
	}
	return out
}

func cleanNote(note string) string {
	note = strings.TrimSpace(strings.ReplaceAll(note, "\r\n", "\n"))
	if r := []rune(note); len(r) > MaxReviewNote {
		note = string(r[:MaxReviewNote])
	}
	return note
}

// Request asks for a review of the user's file
func (s *ReviewService) Request(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, note string) (*models.ModuleReview, error) {
	if !c.WritableBy(user.ID) {
		return nil, apperr.ErrForbidden
	}
	g := s.DirectGroup(ctx, p, c)
	if g == nil {
		return nil, apperr.New("err.review_unavailable")
	}
	d, err := s.Changes(ctx, p, c)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, apperr.New("err.review_nothing")
	}
	gid := g.ID
	r := &models.ModuleReview{ProjectID: p.ID, CatalogID: c.ID, GroupID: &gid, Module: models.ModuleOf(c), RepoPath: c.RepoPath,
		Status: models.ReviewRequested, RequestedBy: user.ID, Note: cleanNote(note), Entries: d.Changed}
	if err := s.repo.Create(ctx, r); errors.Is(err, store.ErrConflict) {
		return nil, apperr.New("err.review_open")
	} else if err != nil {
		return nil, err
	}
	go s.notifyReviewers(context.Background(), p, user, c, r)
	return r, nil
}

// Withdraw takes the user's own open request back
func (s *ReviewService) Withdraw(ctx context.Context, user *models.User, p *models.Project, id int64) error {
	r, err := s.Get(ctx, p, id)
	if err != nil {
		return err
	}
	if r.RequestedBy != user.ID {
		return apperr.ErrForbidden
	}
	if r.Status != models.ReviewRequested {
		return apperr.New("err.review_closed")
	}
	r.Status = models.ReviewWithdrawn
	return s.repo.Update(ctx, r)
}

// Reject sends a request back to the translator with a note (owners and managers, not the translator)
func (s *ReviewService) Reject(ctx context.Context, reviewer *models.User, p *models.Project, id int64, note string) error {
	r, err := s.reviewable(ctx, reviewer, p, id)
	if err != nil {
		return err
	}
	if note = cleanNote(note); note == "" {
		return apperr.New("err.review_note")
	}
	now := time.Now()
	r.Status, r.ReviewedBy, r.ReviewedAt, r.ReviewNote = models.ReviewRejected, &reviewer.ID, &now, note
	if err := s.repo.Update(ctx, r); err != nil {
		return err
	}
	go s.notifyTranslator(context.Background(), p, r, reviewer, "rejected")
	return nil
}

func (s *ReviewService) reviewable(ctx context.Context, reviewer *models.User, p *models.Project, id int64) (*models.ModuleReview, error) {
	if !p.Role.CanManage() {
		return nil, apperr.ErrForbidden
	}
	r, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if r.RequestedBy == reviewer.ID {
		return nil, apperr.New("err.review_own")
	}
	if r.Status != models.ReviewRequested {
		return nil, apperr.New("err.review_closed")
	}
	return r, nil
}

// Approve commits a requested file to the repository (owners and managers, not the translator)
func (s *ReviewService) Approve(ctx context.Context, reviewer *models.User, p *models.Project, id int64) (*models.ModuleReview, error) {
	r, err := s.reviewable(ctx, reviewer, p, id)
	if err != nil {
		return nil, err
	}
	c, err := s.catalogs.catalogs.GetByID(ctx, r.CatalogID)
	if err != nil {
		return nil, err
	}
	return r, s.push(ctx, reviewer, p, c, r)
}

// PushNow commits a file to the repository right away (owners and managers, any member's file)
func (s *ReviewService) PushNow(ctx context.Context, manager *models.User, p *models.Project, c *models.Catalog) (*models.ModuleReview, error) {
	if !p.Role.CanManage() {
		return nil, apperr.ErrForbidden
	}
	if p.ReviewRequired { // the project sends files only through an approved review
		return nil, apperr.New("err.review_required")
	}
	if c.DeletedAt != nil || c.ProjectID != p.ID {
		return nil, apperr.ErrNotFound
	}
	g := s.DirectGroup(ctx, p, c)
	if g == nil {
		return nil, apperr.New("err.review_unavailable")
	}
	if n := s.openComments(ctx, p, c.ID); n > 0 {
		return nil, apperr.New("err.review_open_comments", n)
	}
	latest, err := s.repo.Latest(ctx, []int64{c.ID})
	if err != nil {
		return nil, err
	}
	r := latest[c.ID]
	created := false
	if r == nil || r.Status != models.ReviewRequested { // no open request: one made by the manager's button
		gid := g.ID
		r = &models.ModuleReview{ProjectID: p.ID, CatalogID: c.ID, GroupID: &gid, Module: models.ModuleOf(c), RepoPath: c.RepoPath,
			Status: models.ReviewRequested, RequestedBy: c.OwnerID}
		if err := s.repo.Create(ctx, r); err != nil {
			return nil, err
		}
		created = true
	}
	err = s.push(ctx, manager, p, c, r)
	if err != nil && created { // nobody asked for this review: do not leave it waiting in the queue
		r.Status = models.ReviewWithdrawn
		if uerr := s.repo.Update(ctx, r); uerr != nil {
			log.Printf("reviews: withdraw failed direct send %d: %v", r.ID, uerr)
		}
	}
	return r, err
}

// push commits the file's changes to the branch's current file; a failure stays on the open request
func (s *ReviewService) push(ctx context.Context, reviewer *models.User, p *models.Project, c *models.Catalog, r *models.ModuleReview) error {
	err := s.commit(ctx, reviewer, p, c, r)
	if err == nil {
		return nil
	}
	r.Error = err.Error()
	if ue, ok := apperr.AsUserError(err); ok {
		r.Error = source.FetchErrorText(ue)
	}
	if uerr := s.repo.Update(ctx, r); uerr != nil {
		log.Printf("reviews: record failure of %d: %v", r.ID, uerr)
	}
	return err
}

func (s *ReviewService) commit(ctx context.Context, reviewer *models.User, p *models.Project, c *models.Catalog, r *models.ModuleReview) error {
	g := s.DirectGroup(ctx, p, c)
	gh := s.glossary.GitHubApp()
	if g == nil || gh == nil {
		return apperr.New("err.review_unavailable")
	}
	if n := s.openComments(ctx, p, c.ID); n > 0 {
		return apperr.New("err.review_open_comments", n) // review comments must be resolved first
	}
	owner, err := s.users.GetByID(ctx, c.OwnerID)
	if err != nil {
		return err
	}
	sent := s.lastSent(ctx, c.ID)
	want, err := s.catalogs.ChangedTargets(ctx, c, sent)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		f, err := gh.GetFile(ctx, g.GitInstallation, g.GitRepo, g.GitBranch, c.RepoPath)
		if err != nil {
			return gitHubErr(err, c.RepoPath)
		}
		// never overwrite what someone else changed upstream meanwhile: the member pulls it first
		if conflicts := s.catalogs.UpstreamConflicts(ctx, c, f.Data, want, sent); len(conflicts) > 0 {
			return apperr.New("err.review_upstream_conflict", len(conflicts), conflictList(conflicts))
		}
		data, n := gitpatch.ApplyCredit(f.Data, want, TranslatorCredit(owner)) // the header credits the translator
		if n == 0 {
			return apperr.New("err.review_nothing")
		}
		msg := s.commitMessage(p, c, owner, reviewer, r, n)
		cm, err := gh.PutFile(ctx, g.GitInstallation, g.GitRepo, g.GitBranch, c.RepoPath, data, f.SHA, msg,
			githubapp.Person{Name: owner.CreditName(), Email: owner.CreditEmail()})
		if errors.Is(err, githubapp.ErrConflict) && attempt == 0 {
			continue // someone pushed meanwhile: apply to their version
		}
		if err != nil {
			return gitHubErr(err, c.RepoPath)
		}
		now := time.Now()
		r.Status, r.ReviewedBy, r.ReviewedAt = models.ReviewPushed, &reviewer.ID, &now
		// remember only the strings Pelokalan rewrote: they are "ours" from now on, the rest stays upstream's
		r.CommitSHA, r.CommitURL, r.Entries, r.Error, r.PushedData = cm.SHA, cm.URL, n, "", gz.Compress(sentEntries(f.Data, want))
		if err := s.repo.Update(ctx, r); err != nil {
			return err
		}
		s.reservations.ReleasePushed(ctx, p.ID, r.Module, c.OwnerID)
		if g.HasFeed() {
			s.glossary.FetchInBackground(g) // project files, glossary and "accepted upstream" follow the new branch
		}
		if reviewer.ID != c.OwnerID {
			go s.notifyTranslator(context.Background(), p, r, reviewer, "pushed")
		}
		return nil
	}
}

// sentEntries is a PO file of the wanted entries that differ in the branch's file (what a commit rewrites)
func sentEntries(branch []byte, want map[string]gitpatch.Target) []byte {
	cat, err := po.ParseBytes(branch)
	if err != nil {
		return nil
	}
	out := &po.Catalog{Header: cat.Header}
	for _, e := range cat.Entries {
		t, ok := want[e.Key()]
		if !ok || e.Obsolete || sameStrings(e.Translation, t.Translation) && e.Fuzzy == t.Fuzzy {
			continue
		}
		out.Entries = append(out.Entries, &po.Entry{HasContext: e.HasContext, Context: e.Context, ID: e.ID,
			IDPlural: e.IDPlural, Translation: t.Translation, Fuzzy: t.Fuzzy})
	}
	return out.Bytes()
}

// conflictList names the first few conflicting strings (shortened) for the error message
func conflictList(ids []string) string {
	var parts []string
	for i, id := range ids {
		if i == 3 {
			parts = append(parts, "…")
			break
		}
		if r := []rune(strings.ReplaceAll(id, "\n", " ")); len(r) > 40 {
			id = string(r[:40]) + "…"
		}
		parts = append(parts, "“"+id+"”")
	}
	return strings.Join(parts, ", ")
}

func gitHubErr(err error, path string) error {
	switch {
	case errors.Is(err, githubapp.ErrNotInstalled):
		return apperr.New("err.github_not_installed")
	case errors.Is(err, githubapp.ErrNoWrite):
		return apperr.New("err.github_no_write")
	case errors.Is(err, githubapp.ErrNotFound):
		return apperr.New("err.github_file_missing", path)
	case errors.Is(err, githubapp.ErrConflict):
		return apperr.New("err.github_conflict")
	}
	log.Printf("github: %v", err)
	return apperr.New("err.github_unreachable")
}

// commitMessage: what changed, who translated, who reviewed, where it was done
func (s *ReviewService) commitMessage(p *models.Project, c *models.Catalog, owner, reviewer *models.User, r *models.ModuleReview, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Update translation of %s\n\n", c.RepoPath)
	strs := "strings"
	if n == 1 {
		strs = "string"
	}
	fmt.Fprintf(&b, "%d %s translated by %s in Pelokalan.\n", n, strs, owner.CreditName())
	if r.Note != "" {
		fmt.Fprintf(&b, "\n%s\n", r.Note)
	}
	b.WriteString("\n")
	if reviewer.ID != owner.ID {
		fmt.Fprintf(&b, "Reviewed-by: %s <%s>\n", reviewer.CreditName(), reviewer.CreditEmail())
	}
	fmt.Fprintf(&b, "Pelokalan: %s%s\n", strings.TrimRight(s.site.BaseURL, "/"), p.Path())
	return b.String()
}

// notifyReviewers emails the project's owners and managers (not the translator) about a new request
func (s *ReviewService) notifyReviewers(ctx context.Context, p *models.Project, from *models.User, c *models.Catalog, r *models.ModuleReview) {
	if s.mailer == nil || s.projects == nil {
		return
	}
	members, err := s.projects.ListMembers(ctx, p.ID)
	if err != nil {
		log.Printf("reviews: members of %d: %v", p.ID, err)
		return
	}
	link := strings.TrimRight(s.site.BaseURL, "/") + p.Path() + "/reviews/" + fmt.Sprint(r.ID)
	for _, m := range members {
		if !m.Role.CanReview() || m.User.ID == from.ID {
			continue
		}
		u := m.User
		lang := i18n.Normalize(u.Lang)
		subject := i18n.T(lang, "mail.review_request_subject", r.RepoPath, p.Name)
		body := i18n.T(lang, "mail.review_request", from.DisplayName(), r.RepoPath, r.Entries, p.Name)
		s.send(ctx, &u, lang, subject, body, r.Note, link, i18n.T(lang, "mail.review_open_button"), p)
	}
}

// notifyTranslator emails the translator that their file was committed ("pushed") or sent back ("rejected")
func (s *ReviewService) notifyTranslator(ctx context.Context, p *models.Project, r *models.ModuleReview, reviewer *models.User, kind string) {
	if s.mailer == nil || s.users == nil {
		return
	}
	c, err := s.catalogs.catalogs.GetByID(ctx, r.CatalogID)
	if err != nil {
		return
	}
	u, err := s.users.GetByID(ctx, c.OwnerID)
	if err != nil {
		return
	}
	lang := i18n.Normalize(u.Lang)
	base := strings.TrimRight(s.site.BaseURL, "/")
	subject := i18n.T(lang, fmt.Sprintf("mail.review_%s_subject", kind), r.RepoPath, p.Name)
	var body, note, link, button string
	if kind == "pushed" {
		body = i18n.T(lang, "mail.review_pushed", reviewer.DisplayName(), r.Entries, r.RepoPath, p.Name)
		link, button = r.CommitURL, i18n.T(lang, "mail.review_commit_button")
	} else {
		body = i18n.T(lang, "mail.review_rejected", reviewer.DisplayName(), r.RepoPath, p.Name)
		note = r.ReviewNote
		link, button = base+r.FileURL(), i18n.T(lang, "mail.review_file_button")
	}
	s.send(ctx, u, lang, subject, body, note, link, button, p)
}

func (s *ReviewService) send(ctx context.Context, u *models.User, lang, subject, body, note, link, button string, p *models.Project) {
	hello := i18n.T(lang, "mail.badge_hello", u.DisplayName())
	text := hello + "\n\n" + body + "\n\n"
	noteHTML := ""
	if note != "" {
		text += note + "\n\n"
		noteHTML = `<blockquote style="margin:0 0 16px;padding:8px 12px;border-left:3px solid #c7d2fe;background:#f5f7ff;white-space:pre-wrap">` +
			html.EscapeString(note) + `</blockquote>`
	}
	text += link + "\n\n— " + s.site.Name + "\n"
	htmlBody := fmt.Sprintf(`<meta charset="utf-8"><div style="font-family:sans-serif;max-width:560px;color:#1d1f24"><p>%s</p><p>%s</p>%s<p>%s</p>
<p style="color:#888;font-size:12px">%s · %s</p></div>`,
		html.EscapeString(hello), html.EscapeString(body), noteHTML, mailer.Button(link, button),
		html.EscapeString(s.site.Name), html.EscapeString(p.Name))
	if err := s.mailer.Send(ctx, &mailer.Message{To: u.Email, Subject: subject, Text: text, HTML: htmlBody, Category: "review"}); err != nil {
		log.Printf("reviews: email to user %d: %v", u.ID, err)
	}
}
