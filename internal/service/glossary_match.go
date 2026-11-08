package service

import (
	"context"
	"math"
	"strconv"
	"sync"
	"time"

	"pelokalan/internal/ai"
	"pelokalan/internal/glossary"
)

// EntryTerm is a glossary term found in a source string, shown in the editor
type EntryTerm struct {
	Term        string
	Translation string
	Share       int    // percent of uses with this translation
	Hash        string // glossary string page id
}

// EntryGlossary is what the editor shows above the source text
type EntryGlossary struct {
	HasGlossary bool // the project has glossary terms for the language
	Terms       []EntryTerm
}

type matcherEntry struct {
	stamp   string
	checked time.Time
	g       *ai.Glossary
}

// matcherCache keeps one term matcher per project and language. Building one reads
// every preferred term (tens of thousands), so it is rebuilt only when the imports change.
type matcherCache struct {
	mu      sync.Mutex
	entries map[string]*matcherEntry
	build   sync.Mutex
}

// stampCheckInterval limits how often the cheap "did the glossary change" query runs
const stampCheckInterval = 30 * time.Second

func (s *GlossaryService) matcher(ctx context.Context, projectID int64, language string) (*ai.Glossary, error) {
	key := language + "/" + strconv.FormatInt(projectID, 10)
	c := &s.matchers
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*matcherEntry{}
	}
	cur := c.entries[key]
	c.mu.Unlock()
	if cur != nil && time.Since(cur.checked) < stampCheckInterval {
		return cur.g, nil
	}
	stamp, err := s.repo.Stamp(ctx, projectID, language)
	if err != nil {
		return nil, err
	}
	if cur != nil && cur.stamp == stamp {
		c.mu.Lock()
		cur.checked = time.Now()
		c.mu.Unlock()
		return cur.g, nil
	}
	// one build at a time; another request may have finished it meanwhile
	c.build.Lock()
	defer c.build.Unlock()
	c.mu.Lock()
	if e := c.entries[key]; e != nil && e.stamp == stamp {
		c.mu.Unlock()
		return e.g, nil
	}
	c.mu.Unlock()
	pref, err := s.repo.PreferredTerms(ctx, projectID, language, 0)
	if err != nil {
		return nil, err
	}
	terms := make([]ai.Term, len(pref))
	for i, t := range pref {
		terms[i] = ai.Term{Source: t.Source, Translation: t.Translation, Share: t.Share, Words: t.Words}
	}
	g := ai.NewGlossary(terms)
	c.mu.Lock()
	c.entries[key] = &matcherEntry{stamp: stamp, checked: time.Now(), g: g}
	c.mu.Unlock()
	return g, nil
}

// ForEntry finds glossary terms (all versions) that occur in an entry's source text
func (s *GlossaryService) ForEntry(ctx context.Context, projectID int64, language, source, plural string) (*EntryGlossary, error) {
	g, err := s.matcher(ctx, projectID, language)
	if err != nil {
		return nil, err
	}
	out := &EntryGlossary{HasGlossary: g.Len() > 0}
	seen := map[string]bool{}
	for _, text := range []string{source, plural} {
		if text == "" {
			continue
		}
		for _, t := range g.MatchTerms(text) {
			norm := glossary.Normalize(t.Source)
			if seen[norm] {
				continue
			}
			seen[norm] = true
			out.Terms = append(out.Terms, EntryTerm{
				Term:        t.Source,
				Translation: t.Translation,
				Share:       int(math.Round(t.Share * 100)),
				Hash:        glossary.TermHash(norm),
			})
		}
	}
	return out, nil
}
