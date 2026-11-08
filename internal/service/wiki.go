package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pelokalan/internal/blobstore"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// Wiki limits
const (
	wikiMaxTitle = 120
	wikiMaxBody  = 200_000
)

var (
	ErrWikiTitle       = userErr("err.wiki_title", wikiMaxTitle)
	ErrWikiBody        = userErr("err.wiki_body", wikiMaxBody)
	ErrWikiSlug        = userErr("err.wiki_slug")
	ErrWikiSlugTaken   = userErr("err.wiki_slug_taken")
	ErrWikiParent      = userErr("err.wiki_parent", models.WikiMaxDepth)
	ErrWikiConflict    = userErr("err.wiki_conflict")
	ErrWikiHasChildren = userErr("err.wiki_has_children")
)

// wikiReserved are page addresses that clash with wiki routes
var wikiReserved = map[string]bool{"new": true, "files": true, "preview": true}

// WikiService manages project wiki pages and their files
type WikiService struct {
	repo  repository.WikiRepository
	blobs blobstore.Store
}

// NewWikiService creates the wiki service
func NewWikiService(repo repository.WikiRepository, blobs blobstore.Store) *WikiService {
	return &WikiService{repo: repo, blobs: blobs}
}

// WikiInput is the editable part of a page
type WikiInput struct {
	Title    string
	Slug     string // empty on create derives it from the title; on edit, empty keeps it
	Body     string
	ParentID int64 // 0 = top level
	Position int
	Version  int64 // the page's Version() when editing started
}

// WikiTree is a project's pages arranged by parent
type WikiTree struct {
	Roots []*models.WikiPage
	ByID  map[int64]*models.WikiPage
}

// First is the page shown on the wiki tab: the first top-level page
func (t *WikiTree) First() *models.WikiPage {
	if len(t.Roots) == 0 {
		return nil
	}
	return t.Roots[0]
}

// height is the number of levels from p down to its deepest descendant (1 = no children)
func height(p *models.WikiPage) int {
	h := 0
	for _, c := range p.Children {
		h = max(h, height(c))
	}
	return h + 1
}

// isWithin reports whether p is node or one of its descendants
func isWithin(p, node *models.WikiPage) bool {
	for q := p; q != nil; {
		if q.ID == node.ID {
			return true
		}
		q = q.Parent()
	}
	return false
}

// Tree loads all pages of a project (without bodies) and links them up
func (s *WikiService) Tree(ctx context.Context, p *models.Project) (*WikiTree, error) {
	pages, err := s.repo.ListPages(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	t := &WikiTree{ByID: make(map[int64]*models.WikiPage, len(pages))}
	for _, pg := range pages {
		t.ByID[pg.ID] = pg
	}
	for _, pg := range pages { // already ordered by position, title
		if pg.ParentID != nil {
			if parent := t.ByID[*pg.ParentID]; parent != nil {
				parent.Children = append(parent.Children, pg)
				pg.SetParent(parent)
				continue
			}
		}
		t.Roots = append(t.Roots, pg)
	}
	var walk func(list []*models.WikiPage, depth int)
	walk = func(list []*models.WikiPage, depth int) {
		for _, pg := range list {
			pg.Depth = depth
			walk(pg.Children, depth+1)
		}
	}
	walk(t.Roots, 1)
	return t, nil
}

// WikiParentOption is a page that can hold the edited page
type WikiParentOption struct {
	ID    int64
	Title string
	Depth int
}

// ParentOptions lists pages that may become the parent of page (nil = a new page) without
// exceeding the depth limit; page itself and its descendants are excluded.
func (t *WikiTree) ParentOptions(page *models.WikiPage) []WikiParentOption {
	need := 1
	var self *models.WikiPage
	if page != nil {
		if self = t.ByID[page.ID]; self != nil {
			need = height(self)
		}
	}
	var out []WikiParentOption
	var walk func(list []*models.WikiPage)
	walk = func(list []*models.WikiPage) {
		for _, pg := range list {
			if self != nil && isWithin(pg, self) {
				continue
			}
			if pg.Depth+need <= models.WikiMaxDepth {
				out = append(out, WikiParentOption{ID: pg.ID, Title: pg.Title, Depth: pg.Depth})
			}
			walk(pg.Children)
		}
	}
	walk(t.Roots)
	return out
}

// Breadcrumbs returns the ancestors of a page, top first
func (t *WikiTree) Breadcrumbs(page *models.WikiPage) []*models.WikiPage {
	var out []*models.WikiPage
	pg := t.ByID[page.ID]
	for pg != nil && pg.Parent() != nil {
		pg = pg.Parent()
		out = append([]*models.WikiPage{pg}, out...)
	}
	return out
}

// Redirect finds where a page that used to have this address lives now
func (s *WikiService) Redirect(ctx context.Context, p *models.Project, slug string) (string, error) {
	cur, err := s.repo.Redirect(ctx, p.ID, strings.ToLower(slug))
	if errors.Is(err, repository.ErrNotFound) {
		return "", ErrNotFound
	}
	return cur, err
}

// wikiHistoryLimit is how many saves the page shows
const wikiHistoryLimit = 50

// Edits lists who saved the page and when, newest first
func (s *WikiService) Edits(ctx context.Context, pg *models.WikiPage) ([]models.WikiEdit, error) {
	return s.repo.Edits(ctx, pg.ID, wikiHistoryLimit)
}

// Page loads one page with its body
func (s *WikiService) Page(ctx context.Context, p *models.Project, slug string) (*models.WikiPage, error) {
	pg, err := s.repo.GetPage(ctx, p.ID, strings.ToLower(slug))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return pg, err
}

func (s *WikiService) validate(in *WikiInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.ReplaceAll(in.Body, "\r\n", "\n")
	if in.Title == "" || len([]rune(in.Title)) > wikiMaxTitle {
		return ErrWikiTitle
	}
	if len(in.Body) > wikiMaxBody {
		return ErrWikiBody
	}
	return nil
}

// checkParent verifies the parent exists and the page (with its subtree) still fits the depth limit
func (s *WikiService) checkParent(t *WikiTree, page *models.WikiPage, parentID int64) (*int64, error) {
	if parentID == 0 {
		return nil, nil
	}
	for _, o := range t.ParentOptions(page) {
		if o.ID == parentID {
			return &parentID, nil
		}
	}
	return nil, ErrWikiParent
}

func (s *WikiService) uniqueSlug(ctx context.Context, projectID int64, base string) (string, error) {
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = base + "-" + strconv.Itoa(i)
		}
		if wikiReserved[cand] {
			continue
		}
		taken, err := s.repo.SlugExists(ctx, projectID, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	return "", ErrWikiSlugTaken
}

// wikiSlug derives a page address from its title ("Panduan Gaya" -> "panduan-gaya")
func wikiSlug(title string) string {
	s := Slugify(title)
	// Slugify pads short or empty results with "project"; say "page" for wiki pages instead
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), "project") {
		if s == "project" {
			return "page"
		}
		if rest, ok := strings.CutPrefix(s, "project-"); ok {
			return "page-" + rest
		}
	}
	return s
}

// Create adds a page (project members)
func (s *WikiService) Create(ctx context.Context, user *models.User, p *models.Project, in WikiInput) (*models.WikiPage, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	t, err := s.Tree(ctx, p)
	if err != nil {
		return nil, err
	}
	parent, err := s.checkParent(t, nil, in.ParentID)
	if err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		if slug, err = s.uniqueSlug(ctx, p.ID, wikiSlug(in.Title)); err != nil {
			return nil, err
		}
	} else {
		if !validSlug(slug) || wikiReserved[slug] {
			return nil, ErrWikiSlug
		}
		if taken, err := s.repo.SlugExists(ctx, p.ID, slug); err != nil {
			return nil, err
		} else if taken {
			return nil, ErrWikiSlugTaken
		}
	}
	// new pages go to the end of their level
	siblings := t.Roots
	if parent != nil {
		siblings = t.ByID[*parent].Children
	}
	pos := 0
	for _, sb := range siblings {
		pos = max(pos, sb.Position+1)
	}
	pg := &models.WikiPage{ProjectID: p.ID, ParentID: parent, Slug: slug, Title: in.Title, Body: in.Body,
		Position: pos, CreatedBy: &user.ID}
	if err := s.repo.CreatePage(ctx, pg); err != nil {
		return nil, err
	}
	return pg, nil
}

// Update saves a page (project members); a change made meanwhile by someone else is refused
func (s *WikiService) Update(ctx context.Context, user *models.User, p *models.Project, slug string, in WikiInput) (*models.WikiPage, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	pg, err := s.Page(ctx, p, slug)
	if err != nil {
		return nil, err
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	// a new address must be valid and free; the old one keeps redirecting here
	if slug := strings.ToLower(strings.TrimSpace(in.Slug)); slug != "" && slug != pg.Slug {
		if !validSlug(slug) || wikiReserved[slug] {
			return nil, ErrWikiSlug
		}
		if taken, err := s.repo.SlugExists(ctx, p.ID, slug); err != nil {
			return nil, err
		} else if taken {
			return nil, ErrWikiSlugTaken
		}
		pg.Slug = slug
	}
	// the place in the tree changes only by moving (Move)
	pg.Title, pg.Body, pg.UpdatedBy = in.Title, in.Body, &user.ID
	if err := s.repo.UpdatePage(ctx, pg, in.Version); errors.Is(err, repository.ErrConflict) {
		return nil, ErrWikiConflict
	} else if err != nil {
		return nil, err
	}
	return pg, nil
}

// Move places a page before or after another page ("before", "after") or as its last
// subpage ("inside"); project members may reorganize the wiki. The depth limit applies to
// the moved page together with its subpages.
func (s *WikiService) Move(ctx context.Context, user *models.User, p *models.Project, slug string, targetID int64, mode string) error {
	if !p.Role.IsMember() {
		return ErrForbidden
	}
	t, err := s.Tree(ctx, p)
	if err != nil {
		return err
	}
	var page *models.WikiPage
	for _, pg := range t.ByID {
		if pg.Slug == strings.ToLower(slug) {
			page = pg
		}
	}
	target := t.ByID[targetID]
	if page == nil || target == nil {
		return ErrNotFound
	}
	if isWithin(target, page) { // onto itself or into its own subtree
		return ErrWikiParent
	}
	var parent *models.WikiPage
	switch mode {
	case "inside":
		parent = target
	case "before", "after":
		parent = target.Parent()
	default:
		return ErrWikiParent
	}
	depth := 1
	if parent != nil {
		depth = parent.Depth + 1
	}
	if depth+height(page)-1 > models.WikiMaxDepth {
		return ErrWikiParent
	}
	siblings := t.Roots
	if parent != nil {
		siblings = parent.Children
	}
	var ids []int64
	for _, sb := range siblings {
		if sb.ID == page.ID {
			continue
		}
		if sb.ID == target.ID && mode == "before" {
			ids = append(ids, page.ID)
		}
		ids = append(ids, sb.ID)
		if sb.ID == target.ID && mode == "after" {
			ids = append(ids, page.ID)
		}
	}
	if mode == "inside" {
		ids = append(ids, page.ID)
	}
	var parentID *int64
	if parent != nil {
		parentID = &parent.ID
	}
	return s.repo.Reorder(ctx, p.ID, parentID, ids)
}

// CanDelete reports whether the user may delete the page: owners, managers and its author
func CanDeleteWikiPage(user *models.User, p *models.Project, pg *models.WikiPage) bool {
	if user == nil || !p.Role.IsMember() {
		return false
	}
	return p.Role.CanManage() || (pg.CreatedBy != nil && *pg.CreatedBy == user.ID)
}

// Delete removes a page without subpages
func (s *WikiService) Delete(ctx context.Context, user *models.User, p *models.Project, slug string) error {
	pg, err := s.Page(ctx, p, slug)
	if err != nil {
		return err
	}
	if !CanDeleteWikiPage(user, p, pg) {
		return ErrForbidden
	}
	if has, err := s.repo.HasChildren(ctx, pg.ID); err != nil {
		return err
	} else if has {
		return ErrWikiHasChildren
	}
	return s.repo.DeletePage(ctx, p.ID, pg.ID)
}

// Upload stores images for wiki pages (project members). Same rules as comment
// attachments: png/jpeg/gif/webp detected from the content, 10 MB each, 5 per request.
func (s *WikiService) Upload(ctx context.Context, user *models.User, p *models.Project, uploads []Upload) ([]*models.WikiFile, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	if len(uploads) > MaxAttachments {
		return nil, userErr("err.attachments_count", MaxAttachments)
	}
	type checked struct {
		u     Upload
		ctype string
	}
	var ok []checked
	for _, u := range uploads {
		if len(u.Data) == 0 {
			continue
		}
		if len(u.Data) > MaxAttachmentBytes {
			return nil, userErr("err.attachment_size", MaxAttachmentBytes>>20)
		}
		ctype, _, _ := strings.Cut(http.DetectContentType(u.Data), ";")
		if !models.IsInlineType(ctype) {
			return nil, userErr("err.attachment_type")
		}
		ok = append(ok, checked{u, ctype})
	}
	var out []*models.WikiFile
	for _, c := range ok {
		key, err := s.blobs.Put(ctx, c.u.Data)
		if err != nil {
			return nil, err
		}
		f := &models.WikiFile{ProjectID: p.ID, BlobKey: key, Filename: attachmentFilename(c.u.Filename),
			ContentType: c.ctype, Size: int64(len(c.u.Data)), UploadedBy: &user.ID}
		if err := s.repo.CreateFile(ctx, f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// File returns an uploaded file with its bytes
func (s *WikiService) File(ctx context.Context, p *models.Project, id int64) (*models.WikiFile, []byte, error) {
	f, err := s.repo.GetFile(ctx, p.ID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	data, err := s.blobs.Get(ctx, f.BlobKey)
	if err != nil {
		return nil, nil, err
	}
	return f, data, nil
}
