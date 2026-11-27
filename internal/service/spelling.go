package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/pkg/glossary"
	"pelokalan/pkg/po"
	"pelokalan/pkg/spelling"
)

// SpellingService checks Indonesian translations against a word list taken from KBBI (downloaded
// once into spelling_words, then kept in memory) plus each project's own words (spelling_extra).
// The source has no licence, so checks are for project members only and the list is never
// published.
type SpellingService struct {
	repo    repository.SpellingRepository
	fetcher *glossary.Fetcher
	url     string
	dict    spelling.Dict

	mu     sync.Mutex
	extras map[int64]map[string]bool // project id → its extra words (loaded on first use)
}

// NewSpellingService creates the spelling service; an empty url disables downloading
func NewSpellingService(repo repository.SpellingRepository, fetcher *glossary.Fetcher, url string) *SpellingService {
	return &SpellingService{repo: repo, fetcher: fetcher, url: url, extras: map[int64]map[string]bool{}}
}

// Load fills the database from the URL if it is empty, then loads the word list into memory
func (s *SpellingService) Load(ctx context.Context) error {
	n, err := s.repo.CountWords(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if s.url == "" {
			return nil
		}
		res, err := s.fetcher.Fetch(ctx, s.url, "", "")
		if err != nil {
			return fmt.Errorf("download word list: %w", err)
		}
		words, err := spelling.Parse(res.Data)
		if err != nil {
			return fmt.Errorf("parse word list: %w", err)
		}
		if err := s.repo.ReplaceWords(ctx, words); err != nil {
			return err
		}
		log.Printf("spelling: stored %d words from %s", len(words), s.url)
	}
	words, err := s.repo.Words(ctx)
	if err != nil {
		return err
	}
	s.dict.Set(words)
	log.Printf("spelling: %d words loaded", len(words))
	return nil
}

// Indonesian reports whether a language code is Indonesian ("id", "id_ID")
func Indonesian(language string) bool {
	return po.SameLanguage(language, "id") || po.SameLanguage(language, "id_ID")
}

// Applies reports whether a file of this language gets spelling checks (Indonesian, word list loaded)
func (s *SpellingService) Applies(language string) bool {
	return s != nil && s.dict.Len() > 0 && Indonesian(language)
}

// projectChecker checks with the project's extra words
type projectChecker struct {
	dict  *spelling.Dict
	extra map[string]bool
}

func (c projectChecker) Misspelled(text, source string) []string {
	return c.dict.Misspelled(text, source, c.extra)
}

// Checker returns the project's spelling checker for a file language, or nil when it does not apply
func (s *SpellingService) Checker(ctx context.Context, p *models.Project, language string) models.SpellChecker {
	if !s.Applies(language) {
		return nil
	}
	return projectChecker{dict: &s.dict, extra: s.extra(ctx, p.ID)}
}

func (s *SpellingService) extra(ctx context.Context, projectID int64) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.extras[projectID]; ok {
		return m
	}
	m := map[string]bool{}
	if list, err := s.repo.ExtraWords(ctx, projectID); err == nil {
		for _, w := range list {
			m[w.Word] = true
		}
		s.extras[projectID] = m
	}
	return m
}

func (s *SpellingService) forget(projectID int64) {
	s.mu.Lock()
	delete(s.extras, projectID)
	s.mu.Unlock()
}

// Check returns the unknown words of a translation for a project member (nil when the check
// does not apply)
func (s *SpellingService) Check(ctx context.Context, p *models.Project, language, text, source string) ([]string, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	c := s.Checker(ctx, p, language)
	if c == nil {
		return nil, nil
	}
	return c.Misspelled(text, source), nil
}

// Suggest returns close dictionary words for a project member
func (s *SpellingService) Suggest(p *models.Project, word string) ([]string, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	if s.dict.Len() == 0 {
		return nil, nil
	}
	return s.dict.Suggest(word, 6), nil
}

// ExtraWords lists the project's own words (members)
func (s *SpellingService) ExtraWords(ctx context.Context, p *models.Project) ([]repository.ExtraWord, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	return s.repo.ExtraWords(ctx, p.ID)
}

// AddWord accepts a word in the project (any member): one word, letters with inner hyphens
func (s *SpellingService) AddWord(ctx context.Context, user *models.User, p *models.Project, word string) (string, error) {
	if !p.Role.IsMember() {
		return "", ErrForbidden
	}
	w := strings.ToLower(strings.TrimSpace(word))
	if words := spelling.Words(w); len(words) != 1 || strings.ToLower(words[0]) != w {
		return "", userErr("err.spelling_word")
	}
	if err := s.repo.AddExtraWord(ctx, p.ID, w, user.ID); err != nil {
		return "", err
	}
	s.forget(p.ID)
	return w, nil
}

// DeleteWord removes one of the project's own words (owners and managers)
func (s *SpellingService) DeleteWord(ctx context.Context, p *models.Project, word string) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	if err := s.repo.DeleteExtraWord(ctx, p.ID, strings.ToLower(strings.TrimSpace(word))); err != nil {
		return err
	}
	s.forget(p.ID)
	return nil
}
