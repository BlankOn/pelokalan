package pretranslate

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/source"
	"pelokalan/pkg/ai"
	"pelokalan/pkg/glossary"
)

// Consistency check of a file under review ("Cek konsistensi" in the review editor): the review's strings go through
// the same extraction as the glossary (stored results are reused, the rest is sent to the project's model and stored),
// then every term is compared with the glossary's preferred translation and with the other strings of the review.
// Runs in the background, one per file; the result is kept in memory for the review editor to show.

// consistencyMinShare: a glossary translation counts as the agreed one from this share of its uses
const consistencyMinShare = 0.5

// TermUse is one translation of a term in the checked strings
type TermUse struct {
	Translation string
	Entries     []int64 // entry ids
	Sources     []string
	Glossary    bool // equals the glossary's preferred translation
}

// ConsistencyFinding is a term translated differently from the glossary or in more than one way
type ConsistencyFinding struct {
	Term          string
	Glossary      string // preferred glossary translation ("" = the glossary has no agreed one)
	GlossaryShare int    // percent
	Uses          []TermUse
	InFile        bool // translated in more than one way in these strings
}

// ConsistencyState is a check's progress and outcome
type ConsistencyState struct {
	Running  bool
	Done     int // strings checked
	Total    int
	Cost     float64
	Error    string // i18n "key|arg"
	Findings []ConsistencyFinding
	Terms    int // distinct terms found
	Finished time.Time
}

func (s *Service) consistencyState(catalogID int64) *ConsistencyState {
	if v, ok := s.consistency.Load(catalogID); ok {
		st := v.(*consistencyRun)
		st.mu.Lock()
		defer st.mu.Unlock()
		cp := st.state
		return &cp
	}
	return nil
}

type consistencyRun struct {
	mu    sync.Mutex
	state ConsistencyState
}

// Consistency returns the file's last check (nil when none ran since the server started)
func (s *Service) Consistency(catalogID int64) *ConsistencyState {
	return s.consistencyState(catalogID)
}

// CheckConsistency starts checking the given strings of a file (the review's) in the background
func (s *Service) CheckConsistency(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, entries []*models.Entry) error {
	if !p.Role.CanReview() {
		return apperr.ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return err
	}
	if st == nil {
		return apperr.New("err.ai_not_configured")
	}
	cl, err := s.client(st)
	if err != nil {
		return err
	}
	run := &consistencyRun{}
	if v, loaded := s.consistency.LoadOrStore(c.ID, run); loaded {
		run = v.(*consistencyRun)
		run.mu.Lock()
		busy := run.state.Running
		run.mu.Unlock()
		if busy {
			return apperr.New("err.consistency_running")
		}
	}
	// the strings, each text once
	type str struct {
		hash, source, translation string
		ids                       []int64
	}
	var list []*str
	byHash := map[string]*str{}
	for _, e := range entries {
		tr := e.FirstTranslation()
		if e.Fuzzy || !e.IsTranslated() || strings.TrimSpace(tr) == "" {
			continue
		}
		h := stringHash(e.MsgID, tr)
		if x := byHash[h]; x != nil {
			x.ids = append(x.ids, e.ID)
			continue
		}
		x := &str{hash: h, source: e.MsgID, translation: tr, ids: []int64{e.ID}}
		byHash[h] = x
		list = append(list, x)
	}
	hashes := make([]string, len(list))
	for i, x := range list {
		hashes[i] = x.hash
	}
	done, err := s.repo.ExtractedPairs(ctx, c.Language, hashes)
	if err != nil {
		return err
	}
	var items []ai.ExtractItem
	idHash := map[int64]string{}
	for _, x := range list {
		if _, ok := done[x.hash]; ok {
			continue
		}
		if len(strings.Fields(ai.ExtractText(x.source))) <= 1 { // one word: the string is its own term
			if pair := ai.CheckPairs(ai.ExtractItem{Source: x.source, Translation: x.translation},
				[]ai.Pair{{Source: x.source, Translation: x.translation}}); len(pair) > 0 {
				done[x.hash] = pair
				continue
			}
		}
		id := int64(len(items) + 1)
		idHash[id] = x.hash
		items = append(items, ai.ExtractItem{ID: id, Source: x.source, Translation: x.translation})
	}
	batches := ai.ExtractBatch(items, extractBatchItems, extractBatchChars)
	system := ai.ExtractSystemPrompt("English", languageLabel(c.Language))
	if st.MonthlyBudget > 0 && len(batches) > 0 {
		var prompt, completion int64
		for _, b := range batches {
			prompt += ai.EstimateTokens(system) + ai.EstimateTokens(ai.ExtractUserPrompt(b))
			completion += int64(len(b) * ai.ExtractCompletionTokens)
		}
		spent, err := s.MonthSpend(ctx, p)
		if err != nil {
			return err
		}
		if spent+st.Cost(prompt, completion) > st.MonthlyBudget {
			return apperr.New("err.ai_budget", st.MonthlyBudget-spent)
		}
	}
	run.mu.Lock()
	run.state = ConsistencyState{Running: true, Total: len(list), Done: len(list) - len(items)}
	run.mu.Unlock()

	var job *models.TermExtractionJob
	if len(items) > 0 { // its cost counts toward the monthly budget like any extraction
		job = &models.TermExtractionJob{ProjectID: p.ID, UserID: &user.ID, Status: models.AIJobRunning, Model: st.Model,
			Total: len(items)}
		if err := s.repo.CreateExtractJob(ctx, job); err != nil {
			return err
		}
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		var mu sync.Mutex
		var fatal string
		work := make(chan []ai.ExtractItem)
		var wg sync.WaitGroup
		for range extractParallel {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for b := range work {
					content, u, err := cl.Chat(bg, system, ai.ExtractUserPrompt(b))
					found := map[string][]ai.Pair{}
					if err == nil {
						if answers, perr := ai.ParseExtractResponse(content); perr == nil {
							for _, it := range b {
								if pairs, ok := answers[it.ID]; ok {
									found[idHash[it.ID]] = ai.CheckPairs(it, pairs)
								}
							}
						}
						_ = s.repo.SaveExtractedPairs(bg, c.Language, st.Model, found)
					}
					mu.Lock()
					for h, pairs := range found {
						done[h] = pairs
					}
					if job != nil {
						job.PromptTokens += u.PromptTokens
						job.CompletionTokens += u.CompletionTokens
						job.Cost = st.Cost(job.PromptTokens, job.CompletionTokens)
						job.Processed += len(b)
					}
					if err != nil && fatal == "" {
						var ue *apperr.UserError
						if errors.As(translateAIError(err), &ue) {
							fatal = ue.Key + "|" + joinArgs(ue.Args)
						} else {
							fatal = err.Error()
						}
					}
					run.mu.Lock()
					run.state.Done += len(b)
					if job != nil {
						run.state.Cost = job.Cost
					}
					run.mu.Unlock()
					mu.Unlock()
				}
			}()
		}
		for _, b := range batches {
			work <- b
		}
		close(work)
		wg.Wait()
		if job != nil {
			now := time.Now()
			job.FinishedAt = &now
			job.Status = models.AIJobDone
			if fatal != "" {
				job.Status, job.Error = models.AIJobFailed, fatal
			}
			_ = s.repo.UpdateExtractJob(bg, job)
		}

		// compare: glossary's agreed translations (people's, not AI-only) and the strings among themselves
		// (the file's module's own translations first, like the editor and pre-translation)
		pref := map[string][2]any{}
		if terms, err := s.glossary.PreferredTerms(bg, p.ID, c.Language, 0); err == nil {
			for _, t := range terms {
				if !t.AI && t.Share >= consistencyMinShare {
					pref[glossary.Normalize(t.Source)] = [2]any{t.Translation, int(t.Share*100 + 0.5)}
				}
			}
			if mod, err := s.glossary.ModuleTerms(bg, p.ID, c.Language, 0, models.ModuleOf(c)); err == nil {
				base := make([]ai.Term, len(terms))
				for i, t := range terms {
					base[i] = ai.Term{Source: t.Source, Translation: t.Translation, Share: t.Share, Words: t.Words, AI: t.AI}
				}
				for _, t := range source.ModuleOverlay(ai.NewGlossary(base), mod).Overlay() {
					if !t.AI && t.Share >= consistencyMinShare {
						pref[glossary.Normalize(t.Source)] = [2]any{t.Translation, int(t.Share*100 + 0.5)}
					}
				}
			}
		}
		type agg struct {
			term string
			uses map[string]*TermUse
			keys []string
		}
		terms := map[string]*agg{}
		var order []string
		for _, x := range list {
			for _, pr := range done[x.hash] {
				sn, tn := glossary.Normalize(pr.Source), glossary.Normalize(pr.Translation)
				a := terms[sn]
				if a == nil {
					a = &agg{term: pr.Source, uses: map[string]*TermUse{}}
					terms[sn] = a
					order = append(order, sn)
				}
				u := a.uses[tn]
				if u == nil {
					u = &TermUse{Translation: pr.Translation}
					a.uses[tn] = u
					a.keys = append(a.keys, tn)
				}
				u.Entries = append(u.Entries, x.ids...)
				if len(u.Sources) < 3 {
					u.Sources = append(u.Sources, x.source)
				}
			}
		}
		var findings []ConsistencyFinding
		for _, sn := range order {
			a := terms[sn]
			f := ConsistencyFinding{Term: a.term, InFile: len(a.uses) > 1}
			differs := false
			if g, ok := pref[sn]; ok {
				f.Glossary, f.GlossaryShare = g[0].(string), g[1].(int)
				for _, tn := range a.keys {
					if tn == glossary.Normalize(f.Glossary) {
						a.uses[tn].Glossary = true
					} else {
						differs = true
					}
				}
			}
			if !f.InFile && !differs {
				continue
			}
			for _, tn := range a.keys {
				f.Uses = append(f.Uses, *a.uses[tn])
			}
			sort.SliceStable(f.Uses, func(i, j int) bool { return len(f.Uses[i].Entries) > len(f.Uses[j].Entries) })
			findings = append(findings, f)
		}
		// differences from the glossary first, then by term
		sort.SliceStable(findings, func(i, j int) bool {
			gi, gj := findings[i].Glossary != "", findings[j].Glossary != ""
			if gi != gj {
				return gi
			}
			return strings.ToLower(findings[i].Term) < strings.ToLower(findings[j].Term)
		})
		run.mu.Lock()
		run.state.Running, run.state.Findings, run.state.Terms, run.state.Error = false, findings, len(order), fatal
		run.state.Finished = time.Now()
		run.mu.Unlock()
	}()
	return nil
}
