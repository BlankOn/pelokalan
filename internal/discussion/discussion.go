package discussion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"pelokalan/internal/activity"
	"pelokalan/internal/catalog"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/blobstore"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/platform/mailer"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/project"
	"pelokalan/internal/source"
)

// Subject is the thing a discussion is about
type Subject struct {
	Type  string
	Key   string
	Title string
	URL   string // page showing the subject and its discussion
	// ReadOnly: the subject is gone (e.g. the PO file was deleted). Its discussion
	// stays readable on its own page, but nobody can comment, edit or delete there.
	ReadOnly bool
}

// ErrDiscussionClosed: the discussion's subject is gone, so it is read-only
var ErrDiscussionClosed = apperr.New("err.discussion_closed")

// ThreadURL is the standalone page of a discussion whose subject is gone
func ThreadURL(p *models.Project, subjectType, key string) string {
	return p.Path() + "/threads/" + subjectType + "/" + key
}

// Thread is a subject with its discussion (nil when nobody commented yet)
type Thread struct {
	Subject    *Subject
	Discussion *models.Discussion
	Comments   []*models.Comment
}

// DiscussionService manages comment threads attached to project subjects
type Service struct {
	repo     Repository
	projects project.Repository
	glossary source.Repository
	catalogs catalog.Repository
	blobs    blobstore.Store
	mailer   mailer.Mailer
	site     config.SiteConfig
	notifs   project.NotificationRepository // members' email choices (nil: email everyone)
	activity *activity.Service

	// reviewFor returns the open review a comment by user on a file's entry belongs to (nil = a plain comment)
	reviewFor func(ctx context.Context, p *models.Project, catalogID int64, user *models.User) *int64
}

// SetReviews makes comments by reviewers on files waiting for review into review comments
func (s *Service) SetReviews(f func(ctx context.Context, p *models.Project, catalogID int64, user *models.User) *int64) {
	s.reviewFor = f
}

// entryCatalog is the file of a po_entry subject key ("<catalog id>-<entry key hash>"; 0 = not one)
func entryCatalog(subjectType, key string) int64 {
	if subjectType != models.SubjectPOEntry {
		return 0
	}
	id, _, ok := strings.Cut(key, "-")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// SetNotifications connects the members' notification settings
func (s *Service) SetNotifications(r project.NotificationRepository) { s.notifs = r }

// NotificationSettings returns a member's email choices in a project
func (s *Service) NotificationSettings(ctx context.Context, user *models.User, p *models.Project) (*models.NotificationSettings, error) {
	if user == nil || !p.Role.IsMember() {
		return nil, apperr.ErrForbidden
	}
	if s.notifs == nil {
		return &models.NotificationSettings{Off: map[string]bool{}}, nil
	}
	all, err := s.notifs.Settings(ctx, p.ID, []int64{user.ID})
	if err != nil {
		return nil, err
	}
	if n := all[user.ID]; n != nil {
		return n, nil
	}
	return &models.NotificationSettings{Off: map[string]bool{}}, nil
}

// SaveNotificationSettings stores a member's email choices; on lists the kinds switched on
func (s *Service) SaveNotificationSettings(ctx context.Context, user *models.User, p *models.Project, muted bool, on map[string]bool) error {
	if user == nil || !p.Role.IsMember() {
		return apperr.ErrForbidden
	}
	if s.notifs == nil {
		return nil
	}
	n := &models.NotificationSettings{Muted: muted, Off: map[string]bool{}}
	for _, k := range models.NotifyKinds {
		if !on[k] {
			n.Off[k] = true
		}
	}
	return s.notifs.Save(ctx, user.ID, p.ID, n)
}

// SetActivity makes the service record comments in the activity log
func (s *Service) SetActivity(a *activity.Service) { s.activity = a }

// NewDiscussionService creates the discussion service
func New(repo Repository, projects project.Repository,
	glossaryRepo source.Repository, catalogs catalog.Repository, blobs blobstore.Store,
	mailer mailer.Mailer, site config.SiteConfig) *Service {
	return &Service{repo: repo, projects: projects, glossary: glossaryRepo, catalogs: catalogs, blobs: blobs,
		mailer: mailer, site: site}
}

// maxCommentLen limits a comment body
const maxCommentLen = 5000

// GlossaryTermURL is the page of a glossary string
func GlossaryTermURL(p *models.Project, hash string) string {
	return p.Path() + "/glossary/" + hash
}

// ResolveSubject checks that a subject exists and returns its title and page.
// Supporting a new kind of subject only needs a case here.
func (s *Service) ResolveSubject(ctx context.Context, p *models.Project, subjectType, key string) (*Subject, error) {
	switch subjectType {
	case models.SubjectGlossaryTerm:
		if len(key) != 64 {
			return nil, apperr.ErrNotFound
		}
		term, err := s.glossary.Term(ctx, p.ID, key)
		if errors.Is(err, store.ErrNotFound) {
			// the term may have left the glossary; keep its discussion reachable
			if d, derr := s.repo.Get(ctx, p.ID, subjectType, key); derr == nil {
				return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: GlossaryTermURL(p, key)}, nil
			}
			return nil, apperr.ErrNotFound
		} else if err != nil {
			return nil, err
		}
		return &Subject{Type: subjectType, Key: key, Title: term.Source, URL: GlossaryTermURL(p, key)}, nil
	case models.SubjectPOEntry:
		c, hash, err := s.entryCatalog(ctx, p, key)
		if err != nil {
			// the PO file was deleted: keep the discussion readable on its own page
			if d, derr := s.repo.Get(ctx, p.ID, subjectType, key); derr == nil {
				return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: ThreadURL(p, subjectType, key), ReadOnly: true}, nil
			}
			return nil, err
		}
		url := c.URL() + "?entry=" + hash
		entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
		if err != nil {
			return nil, err
		}
		// a deleted file stays readable in the editor; its discussions are read-only
		for _, e := range entries {
			if e.KeyHash() == hash {
				return &Subject{Type: subjectType, Key: key, Title: entryTitle(e.MsgID, c), URL: url, ReadOnly: c.IsDeleted()}, nil
			}
		}
		// the entry may have been removed; keep its discussion reachable
		if d, derr := s.repo.Get(ctx, p.ID, subjectType, key); derr == nil {
			return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: url, ReadOnly: c.IsDeleted()}, nil
		}
		return nil, apperr.ErrNotFound
	case models.SubjectTopic:
		// a topic exists only once it has been started (CreateTopic)
		if !topicKeyRe.MatchString(key) {
			return nil, apperr.ErrNotFound
		}
		d, err := s.repo.Get(ctx, p.ID, subjectType, key)
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.ErrNotFound
		} else if err != nil {
			return nil, err
		}
		return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: TopicURL(p, key)}, nil
	}
	return nil, apperr.ErrNotFound
}

var topicKeyRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// TopicURL is the page of a free discussion
func TopicURL(p *models.Project, key string) string { return p.Path() + "/topics/" + key }

const maxTopicTitle = 140

// CreateTopic starts a free discussion (project members) with a title and a first post.
// Nothing is stored when the post is invalid.
func (s *Service) CreateTopic(ctx context.Context, user *models.User, p *models.Project, title, body, lang string, uploads []Upload) (*Subject, error) {
	if !p.Role.IsMember() {
		return nil, apperr.ErrForbidden
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || len([]rune(title)) > maxTopicTitle {
		return nil, apperr.New("err.topic_title", maxTopicTitle)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	key := hex.EncodeToString(b)
	subj := &Subject{Type: models.SubjectTopic, Key: key, Title: title, URL: TopicURL(p, key)}
	if _, err := s.post(ctx, user, p, subj, body, lang, uploads); err != nil {
		return nil, err
	}
	return subj, nil
}

// entryCatalog parses a PO entry subject key and loads its file
func (s *Service) entryCatalog(ctx context.Context, p *models.Project, key string) (*models.Catalog, string, error) {
	idPart, hash, ok := strings.Cut(key, "-")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if !ok || err != nil || len(hash) != 16 {
		return nil, "", apperr.ErrNotFound
	}
	c, err := s.catalogs.GetByID(ctx, id)
	if err != nil || c.ProjectID != p.ID {
		return nil, "", apperr.ErrNotFound
	}
	return c, hash, nil
}

// entryTitle names an entry discussion: its file (owner/file name) first, then its source text
func entryTitle(msgid string, c *models.Catalog) string {
	src := strings.Join(strings.Fields(msgid), " ")
	if r := []rune(src); len(r) > 80 {
		src = string(r[:80]) + "…"
	}
	return c.OwnerUsername + "/" + c.Filename + " — " + src
}

// SubjectURL returns the page of a stored discussion's subject
func (s *Service) SubjectURL(ctx context.Context, p *models.Project, d *models.Discussion) string {
	switch d.SubjectType {
	case models.SubjectGlossaryTerm:
		return GlossaryTermURL(p, d.SubjectKey)
	case models.SubjectTopic:
		return TopicURL(p, d.SubjectKey)
	case models.SubjectPOEntry:
		if c, hash, err := s.entryCatalog(ctx, p, d.SubjectKey); err == nil {
			return c.URL() + "?entry=" + hash
		}
		return ThreadURL(p, d.SubjectType, d.SubjectKey)
	}
	return p.Path() + "/discussions"
}

// Thread loads the discussion of a subject
func (s *Service) Thread(ctx context.Context, p *models.Project, subjectType, key string) (*Thread, error) {
	subj, err := s.ResolveSubject(ctx, p, subjectType, key)
	if err != nil {
		return nil, err
	}
	t := &Thread{Subject: subj}
	d, err := s.repo.Get(ctx, p.ID, subjectType, key)
	if errors.Is(err, store.ErrNotFound) {
		return t, nil
	} else if err != nil {
		return nil, err
	}
	t.Discussion = d
	t.Comments, err = s.repo.Comments(ctx, d.ID)
	return t, err
}

var mentionRe = regexp.MustCompile(`(^|[^\w@./-])@([a-z0-9][a-z0-9._-]{1,28}[a-z0-9])`)

// Mentions returns the distinct usernames mentioned in a text
func Mentions(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(strings.ToLower(body), -1) {
		u := strings.TrimRight(m[2], ".")
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// Members returns the project members keyed by username (for mentions)
func (s *Service) Members(ctx context.Context, p *models.Project) (map[string]*models.User, error) {
	members, err := s.projects.ListMembers(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*models.User, len(members))
	for _, m := range members {
		u := m.User
		out[u.Username] = &u
	}
	return out, nil
}

func cleanBody(body string) (string, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" || len([]rune(body)) > maxCommentLen {
		return "", apperr.New("err.comment_length", maxCommentLen)
	}
	return body, nil
}

// Upload is a file sent with a comment
type Upload struct {
	Filename string
	Data     []byte
}

// Attachment limits
const (
	MaxAttachmentBytes = 10 << 20
	MaxAttachments     = 5
)

// AttachmentFilename is the name an uploaded file is stored under (no directories, safe characters)
func AttachmentFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' {
			return -1
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	return name
}

// storeUploads saves the files and returns attachment records (not yet linked)
func (s *Service) storeUploads(ctx context.Context, uploads []Upload) ([]*models.Attachment, error) {
	if len(uploads) > MaxAttachments {
		return nil, apperr.New("err.attachments_count", MaxAttachments)
	}
	var out []*models.Attachment
	for _, u := range uploads {
		if len(u.Data) == 0 {
			continue
		}
		if len(u.Data) > MaxAttachmentBytes {
			return nil, apperr.New("err.attachment_size", MaxAttachmentBytes>>20)
		}
		// the type comes from the content, never from the file name
		ctype, _, _ := strings.Cut(http.DetectContentType(u.Data), ";")
		// only images for now (png, jpeg, gif, webp)
		if !models.IsInlineType(ctype) {
			return nil, apperr.New("err.attachment_type")
		}
		key, err := s.blobs.Put(ctx, u.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, &models.Attachment{BlobKey: key, Filename: AttachmentFilename(u.Filename), ContentType: ctype, Size: int64(len(u.Data))})
	}
	return out, nil
}

// Comment adds a comment to the discussion of a subject (project members only).
// A comment needs text, files, or both.
func (s *Service) Comment(ctx context.Context, user *models.User, p *models.Project, subjectType, key, body, lang string, uploads []Upload) (*models.Comment, *Subject, error) {
	if !p.Role.IsMember() {
		return nil, nil, apperr.ErrForbidden
	}
	// text is optional when files are attached, but never too long
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if len([]rune(body)) > maxCommentLen || (body == "" && len(uploads) == 0) {
		return nil, nil, apperr.New("err.comment_length", maxCommentLen)
	}
	subj, err := s.ResolveSubject(ctx, p, subjectType, key)
	if err != nil {
		return nil, nil, err
	}
	c, err := s.post(ctx, user, p, subj, body, lang, uploads)
	return c, subj, err
}

// post stores a comment on a resolved subject, creating its discussion on first use
func (s *Service) post(ctx context.Context, user *models.User, p *models.Project, subj *Subject, body, lang string, uploads []Upload) (*models.Comment, error) {
	if subj.ReadOnly {
		return nil, ErrDiscussionClosed
	}
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if len([]rune(body)) > maxCommentLen || (body == "" && len(uploads) == 0) {
		return nil, apperr.New("err.comment_length", maxCommentLen)
	}
	attachments, err := s.storeUploads(ctx, uploads)
	if err != nil {
		return nil, err
	}
	if body == "" && len(attachments) == 0 {
		return nil, apperr.New("err.comment_length", maxCommentLen)
	}
	d := &models.Discussion{ProjectID: p.ID, SubjectType: subj.Type, SubjectKey: subj.Key, Title: subj.Title, CreatedBy: &user.ID}
	if err := s.repo.GetOrCreate(ctx, d); err != nil {
		return nil, err
	}
	members, err := s.Members(ctx, p)
	if err != nil {
		return nil, err
	}
	mentioned := s.resolveMentions(body, members)
	c := &models.Comment{DiscussionID: d.ID, AuthorID: &user.ID, Body: body}
	if cid := entryCatalog(subj.Type, subj.Key); cid > 0 && s.reviewFor != nil {
		c.ReviewID = s.reviewFor(ctx, p, cid, user) // a reviewer's comment on a file waiting for review
	}
	if err := s.repo.AddComment(ctx, c, ids(mentioned), attachments); err != nil {
		return nil, err
	}
	s.activity.Record(ctx, &models.ActivityEvent{Kind: models.ActivityCommentPosted, UserID: &user.ID, ProjectID: &p.ID,
		Data: map[string]any{"subject": subj.Type, "mentions": len(mentioned)}})
	// emails go out in the background so posting stays fast
	go s.notify(context.Background(), user, p, subj, d, c, mentioned, members)
	return c, nil
}

func (s *Service) resolveMentions(body string, members map[string]*models.User) []*models.User {
	var out []*models.User
	for _, name := range Mentions(body) {
		if u, ok := members[name]; ok {
			out = append(out, u)
		}
	}
	return out
}

func ids(users []*models.User) []int64 {
	out := make([]int64, len(users))
	for i, u := range users {
		out[i] = u.ID
	}
	return out
}

// reasons lists why a member hears about a comment, strongest first (models.Notify*)
func (s *Service) reasons(u *models.User, subj *Subject, d *models.Discussion, fileOwner int64,
	commenters map[int64]bool, newTopic, mentioned bool) []string {
	var out []string
	if mentioned {
		out = append(out, models.NotifyMention)
	}
	if fileOwner == u.ID {
		out = append(out, models.NotifyMyFile)
	}
	if d.CreatedBy != nil && *d.CreatedBy == u.ID {
		out = append(out, models.NotifyStarted)
	}
	if commenters[u.ID] {
		out = append(out, models.NotifyParticipated)
	}
	if newTopic {
		out = append(out, models.NotifyNewTopic)
	}
	switch subj.Type {
	case models.SubjectTopic:
		out = append(out, models.NotifyOtherTopic)
	case models.SubjectGlossaryTerm:
		out = append(out, models.NotifyOtherGlossary)
	case models.SubjectPOEntry:
		out = append(out, models.NotifyOtherEntry)
	}
	return out
}

// notify emails the other project members about a new comment. Each member gets at most
// one email, sent when any reason that applies to them is switched on in their settings;
// the email says which reason it was and links to the settings.
func (s *Service) notify(ctx context.Context, author *models.User, p *models.Project, subj *Subject, d *models.Discussion,
	c *models.Comment, mentioned []*models.User, members map[string]*models.User) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("discussion: notify panicked: %v", r)
		}
	}()
	link := s.site.BaseURL + subj.URL
	if strings.Contains(link, "#") {
		link = link[:strings.Index(link, "#")]
	}
	link += "#comment-" + strconv.FormatInt(c.ID, 10)
	settingsLink := s.site.BaseURL + p.Path() + "/settings#notifications"
	isMentioned := map[int64]bool{}
	for _, u := range mentioned {
		isMentioned[u.ID] = true
	}
	// who commented before this comment, and whether this comment started a free discussion
	commenters := map[int64]bool{}
	earlier := 0
	if list, err := s.repo.Comments(ctx, d.ID); err == nil {
		for _, x := range list {
			if x.ID == c.ID {
				continue
			}
			earlier++
			if x.AuthorID != nil {
				commenters[*x.AuthorID] = true
			}
		}
	}
	newTopic := subj.Type == models.SubjectTopic && earlier == 0
	var fileOwner int64
	if subj.Type == models.SubjectPOEntry {
		if cat, _, err := s.entryCatalog(ctx, p, subj.Key); err == nil {
			fileOwner = cat.OwnerID
		}
	}
	var memberIDs []int64
	for _, u := range members {
		memberIDs = append(memberIDs, u.ID)
	}
	settings := map[int64]*models.NotificationSettings{}
	if s.notifs != nil {
		if got, err := s.notifs.Settings(ctx, p.ID, memberIDs); err == nil {
			settings = got
		} else {
			log.Printf("discussion: notification settings: %v", err)
		}
	}
	for _, u := range members {
		if u.ID == author.ID {
			continue
		}
		reason := ""
		for _, r := range s.reasons(u, subj, d, fileOwner, commenters, newTopic, isMentioned[u.ID]) {
			if settings[u.ID].Wants(r) {
				reason = r
				break
			}
		}
		if reason == "" {
			continue // switched off by the member
		}
		kind := "comment"
		if reason == models.NotifyMention {
			kind = "mention"
		}
		lang := i18n.Normalize(u.Lang)
		subject := i18n.T(lang, "mail."+kind+"_subject", author.DisplayName(), subj.Title)
		intro := i18n.T(lang, "mail."+kind+"_intro", author.DisplayName(), subj.Title, p.Name)
		why := i18n.T(lang, "mail.reason", i18n.T(lang, "notify."+reason))
		manage := i18n.T(lang, "mail.manage_notifications")
		text := intro + "\n\n" + c.Body + "\n\n" + link + "\n\n— " + s.site.Name + "\n" + why + "\n" + manage + ": " + settingsLink
		htmlBody := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:560px"><p>%s</p>
<blockquote style="border-left:3px solid #ccc;margin:0;padding:6px 12px;color:#333;white-space:pre-wrap">%s</blockquote>
<p>%s</p>
<p style="color:#888;font-size:12px">%s<br>%s · <a href="%s" style="color:#888">%s</a></p></div>`,
			html.EscapeString(intro), html.EscapeString(c.Body), mailer.Button(link, i18n.T(lang, "mail.mention_button")),
			html.EscapeString(i18n.T(lang, "mail.comment_footer", p.Name)), html.EscapeString(why), settingsLink, html.EscapeString(manage))
		if err := s.mailer.Send(ctx, &mailer.Message{To: u.Email, Subject: subject, Text: text, HTML: htmlBody, Category: kind}); err != nil {
			log.Printf("discussion: %s email to %s: %v", kind, u.Email, err)
		}
	}
}

func (s *Service) projectComment(ctx context.Context, p *models.Project, id int64) (*models.Comment, *models.Discussion, error) {
	c, err := s.repo.GetComment(ctx, id)
	if err != nil {
		return nil, nil, apperr.ErrNotFound
	}
	d, err := s.repo.GetByID(ctx, c.DiscussionID)
	if err != nil || d.ProjectID != p.ID {
		return nil, nil, apperr.ErrNotFound
	}
	// comments of a read-only discussion (its subject is gone) are not changed any more
	if subj, err := s.ResolveSubject(ctx, p, d.SubjectType, d.SubjectKey); err == nil && subj.ReadOnly {
		return nil, nil, ErrDiscussionClosed
	}
	return c, d, nil
}

// EditComment changes the author's own comment
func (s *Service) EditComment(ctx context.Context, user *models.User, p *models.Project, id int64, body string) (*models.Discussion, error) {
	c, d, err := s.projectComment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if c.AuthorID == nil || *c.AuthorID != user.ID || c.IsDeleted() || !p.Role.IsMember() {
		return nil, apperr.ErrForbidden
	}
	body, err = cleanBody(body)
	if err != nil {
		return nil, err
	}
	members, err := s.Members(ctx, p)
	if err != nil {
		return nil, err
	}
	return d, s.repo.UpdateComment(ctx, id, body, ids(s.resolveMentions(body, members)))
}

// DeleteComment removes a comment (its author, or the project owner)
func (s *Service) DeleteComment(ctx context.Context, user *models.User, p *models.Project, id int64) (*models.Discussion, error) {
	c, d, err := s.projectComment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	own := c.AuthorID != nil && *c.AuthorID == user.ID
	if !own && !p.Role.IsOwner() { // only its author or the project owner
		return nil, apperr.ErrForbidden
	}
	return d, s.repo.DeleteComment(ctx, id, user.ID)
}

// ResolveComment closes an open review comment: the file's owner (the translator), the comment's author or an
// owner/manager may
func (s *Service) ResolveComment(ctx context.Context, user *models.User, p *models.Project, id int64) (*models.Discussion, error) {
	c, d, err := s.projectComment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if !c.OpenReview() {
		return d, nil
	}
	allowed := p.Role.CanManage() || c.AuthorID != nil && *c.AuthorID == user.ID
	if !allowed {
		if cid := entryCatalog(d.SubjectType, d.SubjectKey); cid > 0 {
			if cat, err := s.catalogs.GetByID(ctx, cid); err == nil && cat.OwnerID == user.ID {
				allowed = true
			}
		}
	}
	if !allowed {
		return nil, apperr.ErrForbidden
	}
	return d, s.repo.ResolveComment(ctx, id, user.ID)
}

// OpenReviewComments counts a file's open review comments by entry key hash
func (s *Service) OpenReviewComments(ctx context.Context, p *models.Project, catalogID int64) map[string]int {
	out, err := s.repo.OpenReviewComments(ctx, p.ID, catalogID)
	if err != nil {
		return map[string]int{}
	}
	return out
}

// List returns the discussions of a project, newest activity first
func (s *Service) List(ctx context.Context, p *models.Project, f DiscussionFilter) ([]*models.Discussion, int, error) {
	return s.repo.List(ctx, p.ID, f)
}

// Attachment returns an attachment of the project and its content
func (s *Service) Attachment(ctx context.Context, p *models.Project, id int64) (*models.Attachment, []byte, error) {
	a, projectID, err := s.repo.Attachment(ctx, id)
	if err != nil || projectID != p.ID {
		return nil, nil, apperr.ErrNotFound
	}
	data, err := s.blobs.Get(ctx, a.BlobKey)
	if err != nil {
		return nil, nil, apperr.ErrNotFound
	}
	return a, data, nil
}

// Counts returns comment counts of subjects
func (s *Service) Counts(ctx context.Context, p *models.Project, subjectType string, keys []string) (map[string]int, error) {
	return s.repo.Counts(ctx, p.ID, subjectType, keys)
}

// EntrySubjectKey is the discussion key of a PO entry: "<catalog id>-<entry key hash>"
func EntrySubjectKey(c *models.Catalog, e *models.Entry) string {
	return strconv.FormatInt(c.ID, 10) + "-" + e.KeyHash()
}

// CanDeleteDiscussion reports whether a user may delete a whole discussion: whoever
// started it, or a project owner/manager; never a read-only one (its subject is gone)
func CanDeleteDiscussion(user *models.User, p *models.Project, t *Thread) bool {
	if user == nil || t == nil || t.Discussion == nil || t.Subject.ReadOnly || !p.Role.IsMember() {
		return false
	}
	if p.Role.IsOwner() {
		return true
	}
	// its starter, only while every comment in it is theirs: other people's comments are removed only by
	// themselves or the project owner
	if t.Discussion.CreatedBy == nil || *t.Discussion.CreatedBy != user.ID {
		return false
	}
	for _, c := range t.Comments {
		if !c.IsDeleted() && (c.AuthorID == nil || *c.AuthorID != user.ID) {
			return false
		}
	}
	return true
}

// DeleteDiscussion removes a whole discussion (the project owner, or its starter when all comments are theirs)
func (s *Service) DeleteDiscussion(ctx context.Context, user *models.User, p *models.Project, subjectType, key string) (*Subject, error) {
	t, err := s.Thread(ctx, p, subjectType, key)
	if err != nil {
		return nil, err
	}
	if t.Discussion == nil {
		return nil, apperr.ErrNotFound
	}
	if t.Subject.ReadOnly {
		return nil, ErrDiscussionClosed
	}
	if !CanDeleteDiscussion(user, p, t) {
		return nil, apperr.ErrForbidden
	}
	return t.Subject, s.repo.DeleteDiscussion(ctx, t.Discussion.ID)
}
