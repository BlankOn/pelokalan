package service

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

	"pelokalan/internal/blobstore"
	"pelokalan/internal/config"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// Subject is the thing a discussion is about
type Subject struct {
	Type  string
	Key   string
	Title string
	URL   string // page showing the subject and its discussion
}

// Thread is a subject with its discussion (nil when nobody commented yet)
type Thread struct {
	Subject    *Subject
	Discussion *models.Discussion
	Comments   []*models.Comment
}

// DiscussionService manages comment threads attached to project subjects
type DiscussionService struct {
	repo     repository.DiscussionRepository
	projects repository.ProjectRepository
	glossary repository.GlossaryRepository
	catalogs repository.CatalogRepository
	blobs    blobstore.Store
	mailer   Mailer
	site     config.SiteConfig
}

// NewDiscussionService creates the discussion service
func NewDiscussionService(repo repository.DiscussionRepository, projects repository.ProjectRepository,
	glossaryRepo repository.GlossaryRepository, catalogs repository.CatalogRepository, blobs blobstore.Store,
	mailer Mailer, site config.SiteConfig) *DiscussionService {
	return &DiscussionService{repo: repo, projects: projects, glossary: glossaryRepo, catalogs: catalogs, blobs: blobs,
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
func (s *DiscussionService) ResolveSubject(ctx context.Context, p *models.Project, subjectType, key string) (*Subject, error) {
	switch subjectType {
	case models.SubjectGlossaryTerm:
		if len(key) != 64 {
			return nil, ErrNotFound
		}
		term, err := s.glossary.Term(ctx, p.ID, key)
		if errors.Is(err, repository.ErrNotFound) {
			// the term may have left the glossary; keep its discussion reachable
			if d, derr := s.repo.Get(ctx, p.ID, subjectType, key); derr == nil {
				return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: GlossaryTermURL(p, key)}, nil
			}
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		return &Subject{Type: subjectType, Key: key, Title: term.Source, URL: GlossaryTermURL(p, key)}, nil
	case models.SubjectPOEntry:
		c, hash, err := s.entryCatalog(ctx, p, key)
		if err != nil {
			return nil, err
		}
		url := c.URL() + "?entry=" + hash
		entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.KeyHash() == hash {
				return &Subject{Type: subjectType, Key: key, Title: entryTitle(e.MsgID, c), URL: url}, nil
			}
		}
		// the entry may have been removed; keep its discussion reachable
		if d, derr := s.repo.Get(ctx, p.ID, subjectType, key); derr == nil {
			return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: url}, nil
		}
		return nil, ErrNotFound
	case models.SubjectTopic:
		// a topic exists only once it has been started (CreateTopic)
		if !topicKeyRe.MatchString(key) {
			return nil, ErrNotFound
		}
		d, err := s.repo.Get(ctx, p.ID, subjectType, key)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		return &Subject{Type: subjectType, Key: key, Title: d.Title, URL: TopicURL(p, key)}, nil
	}
	return nil, ErrNotFound
}

var topicKeyRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// TopicURL is the page of a free discussion
func TopicURL(p *models.Project, key string) string { return p.Path() + "/topics/" + key }

const maxTopicTitle = 140

// CreateTopic starts a free discussion (project members) with a title and a first post.
// Nothing is stored when the post is invalid.
func (s *DiscussionService) CreateTopic(ctx context.Context, user *models.User, p *models.Project, title, body, lang string, uploads []Upload) (*Subject, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || len([]rune(title)) > maxTopicTitle {
		return nil, userErr("err.topic_title", maxTopicTitle)
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
func (s *DiscussionService) entryCatalog(ctx context.Context, p *models.Project, key string) (*models.Catalog, string, error) {
	idPart, hash, ok := strings.Cut(key, "-")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if !ok || err != nil || len(hash) != 16 {
		return nil, "", ErrNotFound
	}
	c, err := s.catalogs.GetByID(ctx, id)
	if err != nil || c.ProjectID != p.ID {
		return nil, "", ErrNotFound
	}
	return c, hash, nil
}

// entryTitle names an entry discussion: its source text and file
func entryTitle(msgid string, c *models.Catalog) string {
	src := strings.Join(strings.Fields(msgid), " ")
	if r := []rune(src); len(r) > 80 {
		src = string(r[:80]) + "…"
	}
	return src + " — " + c.OwnerUsername + "/" + c.Filename
}

// SubjectURL returns the page of a stored discussion's subject
func (s *DiscussionService) SubjectURL(ctx context.Context, p *models.Project, d *models.Discussion) string {
	switch d.SubjectType {
	case models.SubjectGlossaryTerm:
		return GlossaryTermURL(p, d.SubjectKey)
	case models.SubjectTopic:
		return TopicURL(p, d.SubjectKey)
	case models.SubjectPOEntry:
		if c, hash, err := s.entryCatalog(ctx, p, d.SubjectKey); err == nil {
			return c.URL() + "?entry=" + hash
		}
	}
	return p.Path() + "/discussions"
}

// Thread loads the discussion of a subject
func (s *DiscussionService) Thread(ctx context.Context, p *models.Project, subjectType, key string) (*Thread, error) {
	subj, err := s.ResolveSubject(ctx, p, subjectType, key)
	if err != nil {
		return nil, err
	}
	t := &Thread{Subject: subj}
	d, err := s.repo.Get(ctx, p.ID, subjectType, key)
	if errors.Is(err, repository.ErrNotFound) {
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
func (s *DiscussionService) Members(ctx context.Context, p *models.Project) (map[string]*models.User, error) {
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
		return "", userErr("err.comment_length", maxCommentLen)
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

func attachmentFilename(name string) string {
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
func (s *DiscussionService) storeUploads(ctx context.Context, uploads []Upload) ([]*models.Attachment, error) {
	if len(uploads) > MaxAttachments {
		return nil, userErr("err.attachments_count", MaxAttachments)
	}
	var out []*models.Attachment
	for _, u := range uploads {
		if len(u.Data) == 0 {
			continue
		}
		if len(u.Data) > MaxAttachmentBytes {
			return nil, userErr("err.attachment_size", MaxAttachmentBytes>>20)
		}
		// the type comes from the content, never from the file name
		ctype, _, _ := strings.Cut(http.DetectContentType(u.Data), ";")
		// only images for now (png, jpeg, gif, webp)
		if !models.IsInlineType(ctype) {
			return nil, userErr("err.attachment_type")
		}
		key, err := s.blobs.Put(ctx, u.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, &models.Attachment{BlobKey: key, Filename: attachmentFilename(u.Filename), ContentType: ctype, Size: int64(len(u.Data))})
	}
	return out, nil
}

// Comment adds a comment to the discussion of a subject (project members only).
// A comment needs text, files, or both.
func (s *DiscussionService) Comment(ctx context.Context, user *models.User, p *models.Project, subjectType, key, body, lang string, uploads []Upload) (*models.Comment, *Subject, error) {
	if !p.Role.IsMember() {
		return nil, nil, ErrForbidden
	}
	// text is optional when files are attached, but never too long
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if len([]rune(body)) > maxCommentLen || (body == "" && len(uploads) == 0) {
		return nil, nil, userErr("err.comment_length", maxCommentLen)
	}
	subj, err := s.ResolveSubject(ctx, p, subjectType, key)
	if err != nil {
		return nil, nil, err
	}
	c, err := s.post(ctx, user, p, subj, body, lang, uploads)
	return c, subj, err
}

// post stores a comment on a resolved subject, creating its discussion on first use
func (s *DiscussionService) post(ctx context.Context, user *models.User, p *models.Project, subj *Subject, body, lang string, uploads []Upload) (*models.Comment, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if len([]rune(body)) > maxCommentLen || (body == "" && len(uploads) == 0) {
		return nil, userErr("err.comment_length", maxCommentLen)
	}
	attachments, err := s.storeUploads(ctx, uploads)
	if err != nil {
		return nil, err
	}
	if body == "" && len(attachments) == 0 {
		return nil, userErr("err.comment_length", maxCommentLen)
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
	if err := s.repo.AddComment(ctx, c, ids(mentioned), attachments); err != nil {
		return nil, err
	}
	// emails go out in the background so posting stays fast
	go s.notify(context.Background(), user, p, subj, c, mentioned, members)
	return c, nil
}

func (s *DiscussionService) resolveMentions(body string, members map[string]*models.User) []*models.User {
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

// notify emails every other project member about a new comment; mentioned
// members get the mention wording instead
func (s *DiscussionService) notify(ctx context.Context, author *models.User, p *models.Project, subj *Subject, c *models.Comment,
	mentioned []*models.User, members map[string]*models.User) {
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
	isMentioned := map[int64]bool{}
	for _, u := range mentioned {
		isMentioned[u.ID] = true
	}
	for _, u := range members {
		if u.ID == author.ID {
			continue
		}
		kind := "comment"
		if isMentioned[u.ID] {
			kind = "mention"
		}
		lang := i18n.Normalize(u.Lang)
		subject := i18n.T(lang, "mail."+kind+"_subject", author.DisplayName(), subj.Title)
		intro := i18n.T(lang, "mail."+kind+"_intro", author.DisplayName(), subj.Title, p.Name)
		text := intro + "\n\n" + c.Body + "\n\n" + link + "\n\n— " + s.site.Name
		htmlBody := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:560px"><p>%s</p>
<blockquote style="border-left:3px solid #ccc;margin:0;padding:6px 12px;color:#333;white-space:pre-wrap">%s</blockquote>
<p><a href="%s" style="display:inline-block;background:#2f6fde;color:#fff;padding:9px 16px;border-radius:6px;text-decoration:none">%s</a></p>
<p style="color:#888;font-size:12px">%s</p></div>`,
			html.EscapeString(intro), html.EscapeString(c.Body), link,
			html.EscapeString(i18n.T(lang, "mail.mention_button")),
			html.EscapeString(i18n.T(lang, "mail.comment_footer", p.Name)))
		if err := s.mailer.Send(ctx, &Message{To: u.Email, Subject: subject, Text: text, HTML: htmlBody, Category: kind}); err != nil {
			log.Printf("discussion: %s email to %s: %v", kind, u.Email, err)
		}
	}
}

func (s *DiscussionService) projectComment(ctx context.Context, p *models.Project, id int64) (*models.Comment, *models.Discussion, error) {
	c, err := s.repo.GetComment(ctx, id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	d, err := s.repo.GetByID(ctx, c.DiscussionID)
	if err != nil || d.ProjectID != p.ID {
		return nil, nil, ErrNotFound
	}
	return c, d, nil
}

// EditComment changes the author's own comment
func (s *DiscussionService) EditComment(ctx context.Context, user *models.User, p *models.Project, id int64, body string) (*models.Discussion, error) {
	c, d, err := s.projectComment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if c.AuthorID == nil || *c.AuthorID != user.ID || c.IsDeleted() || !p.Role.IsMember() {
		return nil, ErrForbidden
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

// DeleteComment removes a comment (its author, or a project owner/manager)
func (s *DiscussionService) DeleteComment(ctx context.Context, user *models.User, p *models.Project, id int64) (*models.Discussion, error) {
	c, d, err := s.projectComment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	own := c.AuthorID != nil && *c.AuthorID == user.ID
	if !own && !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	return d, s.repo.DeleteComment(ctx, id, user.ID)
}

// List returns the discussions of a project, newest activity first
func (s *DiscussionService) List(ctx context.Context, p *models.Project, f repository.DiscussionFilter) ([]*models.Discussion, int, error) {
	return s.repo.List(ctx, p.ID, f)
}

// Attachment returns an attachment of the project and its content
func (s *DiscussionService) Attachment(ctx context.Context, p *models.Project, id int64) (*models.Attachment, []byte, error) {
	a, projectID, err := s.repo.Attachment(ctx, id)
	if err != nil || projectID != p.ID {
		return nil, nil, ErrNotFound
	}
	data, err := s.blobs.Get(ctx, a.BlobKey)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return a, data, nil
}

// Counts returns comment counts of subjects
func (s *DiscussionService) Counts(ctx context.Context, p *models.Project, subjectType string, keys []string) (map[string]int, error) {
	return s.repo.Counts(ctx, p.ID, subjectType, keys)
}

// EntrySubjectKey is the discussion key of a PO entry: "<catalog id>-<entry key hash>"
func EntrySubjectKey(c *models.Catalog, e *models.Entry) string {
	return strconv.FormatInt(c.ID, 10) + "-" + e.KeyHash()
}
