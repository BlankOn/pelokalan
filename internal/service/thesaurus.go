package service

import (
	"context"
	"fmt"
	"log"

	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/internal/thesaurus"
)

// ThesaurusService offers Indonesian synonyms to project members in the editor. The data is
// downloaded once (when the database has none) and kept in memory.
type ThesaurusService struct {
	repo    repository.ThesaurusRepository
	fetcher *glossary.Fetcher
	url     string
	index   thesaurus.Index
}

// NewThesaurusService creates the thesaurus service; an empty url disables downloading
func NewThesaurusService(repo repository.ThesaurusRepository, fetcher *glossary.Fetcher, url string) *ThesaurusService {
	return &ThesaurusService{repo: repo, fetcher: fetcher, url: url}
}

// Load fills the database from the URL if it is empty, then loads the thesaurus into memory
func (s *ThesaurusService) Load(ctx context.Context) error {
	n, err := s.repo.Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if s.url == "" {
			return nil
		}
		res, err := s.fetcher.Fetch(ctx, s.url, "", "")
		if err != nil {
			return fmt.Errorf("download thesaurus: %w", err)
		}
		entries, err := thesaurus.Parse(res.Data)
		if err != nil {
			return fmt.Errorf("parse thesaurus: %w", err)
		}
		if err := s.repo.ReplaceAll(ctx, entries); err != nil {
			return err
		}
		log.Printf("thesaurus: stored %d entries from %s", len(entries), s.url)
	}
	entries, err := s.repo.All(ctx)
	if err != nil {
		return err
	}
	s.index.Set(entries)
	log.Printf("thesaurus: %d entries loaded", len(entries))
	return nil
}

// Available reports whether lookups can return anything
func (s *ThesaurusService) Available() bool { return s.index.Len() > 0 }

// Lookup returns the entry of a word for a project member, or nil
func (s *ThesaurusService) Lookup(p *models.Project, word string) (*thesaurus.Entry, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	if e, ok := s.index.LookupForms(word); ok {
		return e, nil
	}
	return nil, nil
}
