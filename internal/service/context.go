package service

import (
	"context"
	"errors"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

const maxContextLen = 10000

// ContextService manages Konteks: one free note per PO file, kept in Pelokalan only.
// Anyone who can read the file can read it; only the file owner edits it.
type ContextService struct {
	repo repository.ContextRepository
}

// NewContextService creates the Konteks service
func NewContextService(repo repository.ContextRepository) *ContextService {
	return &ContextService{repo: repo}
}

// ForCatalog returns the note of a PO file, or nil
func (s *ContextService) ForCatalog(ctx context.Context, c *models.Catalog) (*models.CatalogContext, error) {
	n, err := s.repo.Get(ctx, c.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return n, err
}

// Save stores (or, when empty, removes) the note of a PO file; only its owner may
func (s *ContextService) Save(ctx context.Context, user *models.User, c *models.Catalog, note string) (*models.CatalogContext, error) {
	if user == nil || c.OwnerID != user.ID {
		return nil, ErrForbidden
	}
	note = strings.TrimRight(strings.ReplaceAll(note, "\r\n", "\n"), " \n")
	if len([]rune(note)) > maxContextLen {
		return nil, userErr("err.context_length", maxContextLen)
	}
	n := &models.CatalogContext{CatalogID: c.ID, Note: note, UpdatedBy: &user.ID, UpdaterName: user.DisplayName()}
	if err := s.repo.Set(ctx, n); err != nil {
		return nil, err
	}
	if strings.TrimSpace(note) == "" {
		return nil, nil
	}
	return n, nil
}

// List returns the notes of the project's PO files, most recently changed first
func (s *ContextService) List(ctx context.Context, p *models.Project, q string) ([]*models.CatalogContext, error) {
	return s.repo.ListForProject(ctx, p.ID, q)
}
