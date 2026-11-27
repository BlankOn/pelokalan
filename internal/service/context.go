package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/repository"
)

const (
	maxContextLen = 10000
	// ContextHistoryLimit is how many saves the editor lists
	ContextHistoryLimit = 50
)

// ErrContextConflict: someone saved the Konteks after the editor opened it
var ErrContextConflict = apperr.New("err.context_conflict")

// ContextService manages Konteks: one note per module of a project (all PO files whose
// name starts with the same word), kept in Pelokalan only. Everyone who can read the
// project reads it; members edit it together. It does not depend on any one file, so it
// comes back when someone takes the module again.
type ContextService struct {
	repo repository.ContextRepository
}

// NewContextService creates the Konteks service
func NewContextService(repo repository.ContextRepository) *ContextService {
	return &ContextService{repo: repo}
}

// ModuleOf returns the module a PO file belongs to
func ModuleOf(c *models.Catalog) string { return moduleKey(c.Filename) }

// ModuleKey is the module of a file name (the part before the first dot, lowercase)
func ModuleKey(filename string) string { return moduleKey(filename) }

// ForModule returns a module's Konteks (also when its note is empty), or nil
func (s *ContextService) ForModule(ctx context.Context, projectID int64, module string) (*models.ModuleContext, error) {
	n, err := s.repo.Get(ctx, projectID, module)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return n, err
}

// ForCatalog returns the Konteks note of a PO file's module, or nil when there is none
func (s *ContextService) ForCatalog(ctx context.Context, c *models.Catalog) (*models.ModuleContext, error) {
	if c.IsTemplate {
		return nil, nil
	}
	n, err := s.ForModule(ctx, c.ProjectID, ModuleOf(c))
	if err != nil || n == nil || strings.TrimSpace(n.Note) == "" {
		return nil, err
	}
	return n, nil
}

// Edits lists the latest saves of a module's Konteks
func (s *ContextService) Edits(ctx context.Context, n *models.ModuleContext) ([]models.ContextEdit, error) {
	if n == nil {
		return nil, nil
	}
	return s.repo.Edits(ctx, n.ID, ContextHistoryLimit)
}

// Save stores the Konteks of a file's module (members only). version is the version
// the editor started from (0 = there was none). If someone saved in between, nothing is
// stored: it returns the latest Konteks with ErrContextConflict.
func (s *ContextService) Save(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, note string, version int64) (*models.ModuleContext, error) {
	if user == nil || !p.Role.IsMember() || c.IsTemplate {
		return nil, apperr.ErrForbidden
	}
	note = strings.TrimRight(strings.ReplaceAll(note, "\r\n", "\n"), " \n")
	if len([]rune(note)) > maxContextLen {
		return nil, apperr.New("err.context_length", maxContextLen)
	}
	module := ModuleOf(c)
	if cur, err := s.ForModule(ctx, p.ID, module); err == nil && cur != nil && cur.Version == version && cur.Note == note {
		return cur, nil // nothing changed: no new history entry
	}
	n := &models.ModuleContext{ProjectID: p.ID, Module: module, Note: note, UpdatedBy: &user.ID, UpdaterName: user.DisplayName()}
	err := s.repo.Save(ctx, n, version)
	if errors.Is(err, repository.ErrConflict) {
		latest, _ := s.ForModule(ctx, p.ID, module)
		return latest, ErrContextConflict
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// maxContextTitle is the longest title of a Konteks item without a module
const maxContextTitle = 120

// CreateGeneral adds a Konteks item that belongs to no module (members only)
func (s *ContextService) CreateGeneral(ctx context.Context, user *models.User, p *models.Project, title, note string) (*models.ModuleContext, error) {
	if user == nil || !p.Role.IsMember() {
		return nil, apperr.ErrForbidden
	}
	title = strings.Join(strings.Fields(title), " ")
	note = strings.TrimRight(strings.ReplaceAll(note, "\r\n", "\n"), " \n")
	if title == "" || len([]rune(title)) > maxContextTitle {
		return nil, apperr.New("err.context_title", maxContextTitle)
	}
	if strings.TrimSpace(note) == "" || len([]rune(note)) > maxContextLen {
		return nil, apperr.New("err.context_length", maxContextLen)
	}
	n := &models.ModuleContext{ProjectID: p.ID, Module: "~" + randomHex(6), Title: title, Note: note, UpdatedBy: &user.ID,
		UpdaterName: user.DisplayName()}
	if err := s.repo.Save(ctx, n, 0); err != nil {
		return nil, err
	}
	return n, nil
}

// SaveGeneral changes a Konteks item without a module (members only; an empty note hides it). version is the
// version the form started from; ErrContextConflict with the latest when someone saved in between.
func (s *ContextService) SaveGeneral(ctx context.Context, user *models.User, p *models.Project, key, title, note string, version int64) (*models.ModuleContext, error) {
	if user == nil || !p.Role.IsMember() {
		return nil, apperr.ErrForbidden
	}
	cur, err := s.ForModule(ctx, p.ID, key)
	if err != nil || cur == nil || !cur.IsGeneral() {
		return nil, apperr.ErrNotFound
	}
	title = strings.Join(strings.Fields(title), " ")
	note = strings.TrimRight(strings.ReplaceAll(note, "\r\n", "\n"), " \n")
	if title == "" || len([]rune(title)) > maxContextTitle {
		return nil, apperr.New("err.context_title", maxContextTitle)
	}
	if len([]rune(note)) > maxContextLen {
		return nil, apperr.New("err.context_length", maxContextLen)
	}
	if cur.Version == version && cur.Note == note && cur.Title == title {
		return cur, nil
	}
	n := &models.ModuleContext{ProjectID: p.ID, Module: key, Title: title, Note: note, UpdatedBy: &user.ID, UpdaterName: user.DisplayName()}
	err = s.repo.Save(ctx, n, version)
	if errors.Is(err, repository.ErrConflict) {
		latest, _ := s.ForModule(ctx, p.ID, key)
		return latest, ErrContextConflict
	}
	return n, err
}

// DeleteGeneral removes an item without a module (like emptying its note: it leaves the list, its edits stay)
func (s *ContextService) DeleteGeneral(ctx context.Context, user *models.User, p *models.Project, key string) error {
	if user == nil || !p.Role.IsMember() {
		return apperr.ErrForbidden
	}
	cur, err := s.ForModule(ctx, p.ID, key)
	if err != nil || cur == nil || !cur.IsGeneral() {
		return apperr.ErrNotFound
	}
	if cur.Note == "" {
		return nil
	}
	n := &models.ModuleContext{ProjectID: p.ID, Module: key, Title: cur.Title, Note: "", UpdatedBy: &user.ID, UpdaterName: user.DisplayName()}
	if err := s.repo.Save(ctx, n, cur.Version); errors.Is(err, repository.ErrConflict) {
		return ErrContextConflict
	} else if err != nil {
		return err
	}
	return nil
}

// List returns the project's Konteks notes, most recently changed first
func (s *ContextService) List(ctx context.Context, p *models.Project, q string) ([]*models.ModuleContext, error) {
	return s.repo.ListForProject(ctx, p.ID, q)
}

// randomHex returns n random bytes as hex
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
