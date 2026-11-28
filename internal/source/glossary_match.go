package source

import (
	"context"
	"math"
	"strconv"
	"sync"
	"time"

	"pelokalan/pkg/ai"
	"pelokalan/pkg/glossary"
)

// EntryTerm is a glossary term found in a source string, shown in the editor
type EntryTerm struct {
	Term        string
	Translation string
	Share       int    // percent of uses with this translation
	Hash        string // glossary string page id
	// Module: the file's module uses this translation, which differs from the project-wide Project one
	Module  bool
	Project string
	// Overridden: the project's AI context or the file's Konteks talks about a word of this
	// term, so AI pre-translation treats the glossary translation as a suggestion only
	Overridden bool
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
	modules map[string]*matcherEntry // project matcher + a module's own translations, by module
	build   sync.Mutex
}

// maxModuleMatchers bounds the cached module overlays (they are small; the base matcher is shared)
const maxModuleMatchers = 2000

// ModuleMinUses is how often a module must use its own translation of a term for it to win over the
// project-wide one, unless it is the module's only translation of the term
const ModuleMinUses = 2

// ModuleOverlay puts a module's own translations ahead of the project-wide ones in base: those the module uses
// at least ModuleMinUses times or exclusively, that differ from the project's, and (when found only by AI) do not
// replace a translation people agreed on
func ModuleOverlay(base *ai.Glossary, mod []ModuleTerm) *ai.Glossary {
	var over []ai.Term
	for _, m := range mod {
		if m.Count < ModuleMinUses && m.Variants != 1 || m.Total == 0 {
			continue
		}
		t := ai.Term{Source: m.Source, Translation: m.Translation, Share: float64(m.Count) / float64(m.Total),
			Words: m.Words, AI: m.AI, Module: true}
		if b, ok := base.Lookup(m.Source); ok {
			if glossary.Normalize(b.Translation) == glossary.Normalize(m.Translation) || m.AI && !b.AI {
				continue
			}
			t.Source, t.Project = b.Source, b.Translation
		}
		over = append(over, t)
	}
	return base.WithOverlay(over)
}

// moduleMatcher is the project matcher with the module's own translations on top (cached with the same stamp)
func (s *Service) moduleMatcher(ctx context.Context, projectID int64, language, module string) (*ai.Glossary, error) {
	base, err := s.matcher(ctx, projectID, language)
	if err != nil || module == "" {
		return base, err
	}
	c := &s.matchers
	key := language + "/" + strconv.FormatInt(projectID, 10)
	c.mu.Lock()
	stamp := ""
	if e := c.entries[key]; e != nil {
		stamp = e.stamp
	}
	mkey := key + "/" + module
	if e := c.modules[mkey]; e != nil && e.stamp == stamp {
		c.mu.Unlock()
		return e.g, nil
	}
	c.mu.Unlock()
	mod, err := s.repo.ModuleTerms(ctx, projectID, language, 0, module)
	if err != nil {
		return nil, err
	}
	g := ModuleOverlay(base, mod)
	c.mu.Lock()
	if c.modules == nil || len(c.modules) >= maxModuleMatchers {
		c.modules = map[string]*matcherEntry{}
	}
	c.modules[mkey] = &matcherEntry{stamp: stamp, g: g}
	c.mu.Unlock()
	return g, nil
}

// stampCheckInterval limits how often the cheap "did the glossary change" query runs
const stampCheckInterval = 30 * time.Second

func (s *Service) matcher(ctx context.Context, projectID int64, language string) (*ai.Glossary, error) {
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
		terms[i] = ai.Term{Source: t.Source, Translation: t.Translation, Share: t.Share, Words: t.Words, AI: t.AI}
	}
	g := ai.NewGlossary(terms)
	c.mu.Lock()
	c.entries[key] = &matcherEntry{stamp: stamp, checked: time.Now(), g: g}
	c.mu.Unlock()
	return g, nil
}

// ForEntry finds glossary terms (all versions) that occur in an entry's source text; module (models.ModuleKey of
// the file, "" = none) puts that module's own translations first
func (s *Service) ForEntry(ctx context.Context, projectID int64, language, module, source, plural string) (*EntryGlossary, error) {
	g, err := s.moduleMatcher(ctx, projectID, language, module)
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
				Module:      t.Module,
				Project:     t.Project,
			})
		}
	}
	return out, nil
}
