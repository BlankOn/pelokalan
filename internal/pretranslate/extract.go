package pretranslate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/gz"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/source"
	"pelokalan/pkg/ai"
	"pelokalan/pkg/glossary"
	"pelokalan/pkg/po"
)

// Glossary extraction ("Ekstrak istilah dengan AI" on the glossary page): the long strings of a module group's
// files are sent with their translations to the project's model, which names the terms in them; ai.CheckPairs keeps
// the pairs that really occur in both texts. Results are stored per string (term_extractions, shared by projects),
// so a string is sent once: after a sync only new or changed strings cost anything. The pairs become glossary rows
// with origin "ai" of the import they were found in, rebuilt from the stored results after every import.

const (
	extractBatchItems = 20
	extractBatchChars = 9000
	extractParallel   = 3
)

// extractString is a long translated string of an import
type extractString struct {
	hash        string
	source      string
	translation string
	module      string
}

// stringHash identifies a string and its translation for the stored results
func stringHash(source, translation string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + translation))
	return hex.EncodeToString(sum[:])
}

// importStrings reads an import's project files and returns its long translated strings (each text once)
func (s *Service) importStrings(ctx context.Context, imp *models.ModuleGroupImport) ([]extractString, error) {
	files, err := s.glossary.ImportFiles(ctx, imp.ID)
	if err != nil {
		return nil, err
	}
	var out []extractString
	seen := map[string]int{}
	for _, f := range files {
		data, err := gz.Decompress(f.Data)
		if err != nil {
			continue
		}
		cat, err := po.ParseBytes(data)
		if err != nil {
			continue
		}
		module := glossary.ModuleName(f.Filename, imp.Language)
		for _, e := range cat.Entries {
			if e.Obsolete || e.Fuzzy || !e.IsTranslated() || po.IsCreditsEntry(e.ID) {
				continue
			}
			if !ai.ExtractCandidate(e.ID, e.Translation[0]) {
				continue
			}
			h := stringHash(e.ID, e.Translation[0])
			if i, ok := seen[h]; ok {
				if !strings.Contains(out[i].module, module) && len(out[i].module) < 400 {
					out[i].module += "\x1f" + module
				}
				continue
			}
			seen[h] = len(out)
			out = append(out, extractString{hash: h, source: e.ID, translation: e.Translation[0], module: module})
		}
	}
	return out, nil
}

func hashesOf(list []extractString) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.hash
	}
	return out
}

// rebuildAITerms turns the stored results of an import's strings into its AI glossary rows
func (s *Service) rebuildAITerms(ctx context.Context, imp *models.ModuleGroupImport) (int, error) {
	list, err := s.importStrings(ctx, imp)
	if err != nil {
		return 0, err
	}
	found, err := s.repo.ExtractedPairs(ctx, imp.Language, hashesOf(list))
	if err != nil {
		return 0, err
	}
	terms := map[string]*source.GlossaryTerm{}
	var keys []string
	for _, x := range list {
		for _, p := range found[x.hash] {
			sn, tn := glossary.Normalize(p.Source), glossary.Normalize(p.Translation)
			key := sn + "\x00" + tn
			t := terms[key]
			if t == nil {
				t = &source.GlossaryTerm{Source: p.Source, SourceNorm: sn, Translation: p.Translation, TranslationNorm: tn,
					WordCount: len(strings.Fields(p.Source))}
				terms[key] = t
				keys = append(keys, key)
			}
			// the lowercase spelling wins as the shown one ("module" over "Module" at a sentence start)
			if p.Source < t.Source {
				t.Source = p.Source
			}
			if p.Translation > t.Translation {
				t.Translation = p.Translation
			}
			t.Occurrences++
			for _, m := range strings.Split(x.module, "\x1f") {
				if len(t.Modules) < 30 && !contains(t.Modules, m) {
					t.Modules = append(t.Modules, m)
				}
			}
		}
	}
	sort.Strings(keys)
	out := make([]source.GlossaryTerm, len(keys))
	for i, k := range keys {
		out[i] = *terms[k]
	}
	return len(out), s.glossary.ReplaceAITerms(ctx, imp, out)
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// extractPlan is the work of an extraction run: the strings not extracted before, per language
type extractPlan struct {
	imports []*models.ModuleGroupImport
	strings int // long strings of the group
	pending map[string][]ai.ExtractItem
	toSend  int
	batches map[string][][]ai.ExtractItem
	hashes  map[string]map[int64]string // language -> item id -> string hash
	system  map[string]string
}

func (s *Service) groupImports(ctx context.Context, projectID, groupID int64) ([]*models.ModuleGroupImport, error) {
	all, err := s.glossary.ListGroupImports(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []*models.ModuleGroupImport
	for _, imp := range all {
		if imp.GroupID == groupID {
			out = append(out, imp)
		}
	}
	return out, nil
}

func (s *Service) planExtract(ctx context.Context, projectID, groupID int64) (*extractPlan, error) {
	imps, err := s.groupImports(ctx, projectID, groupID)
	if err != nil {
		return nil, err
	}
	pl := &extractPlan{imports: imps, pending: map[string][]ai.ExtractItem{}, batches: map[string][][]ai.ExtractItem{},
		hashes: map[string]map[int64]string{}, system: map[string]string{}}
	seen := map[string]bool{}
	for _, imp := range imps {
		list, err := s.importStrings(ctx, imp)
		if err != nil {
			return nil, err
		}
		pl.strings += len(list)
		done, err := s.repo.ExtractedPairs(ctx, imp.Language, hashesOf(list))
		if err != nil {
			return nil, err
		}
		if pl.hashes[imp.Language] == nil {
			pl.hashes[imp.Language] = map[int64]string{}
			pl.system[imp.Language] = ai.ExtractSystemPrompt("English", languageLabel(imp.Language))
		}
		for _, x := range list {
			if _, ok := done[x.hash]; ok || seen[imp.Language+x.hash] {
				continue
			}
			seen[imp.Language+x.hash] = true
			id := int64(len(pl.hashes[imp.Language]) + 1)
			pl.hashes[imp.Language][id] = x.hash
			pl.pending[imp.Language] = append(pl.pending[imp.Language],
				ai.ExtractItem{ID: id, Source: x.source, Translation: x.translation})
			pl.toSend++
		}
	}
	for lang, items := range pl.pending {
		pl.batches[lang] = ai.ExtractBatch(items, extractBatchItems, extractBatchChars)
	}
	return pl, nil
}

// ExtractEstimate is shown before an extraction run
type ExtractEstimate struct {
	Configured       bool
	Model            string
	GroupID          int64
	Strings          int // long strings of the group
	Done             int // extracted before
	ToSend           int
	Batches          int
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	MonthSpent       float64
	Budget           float64
	BudgetLeft       float64
	OverBudget       bool
	Running          bool
}

func (pl *extractPlan) estimate(st *models.AISettings) (n int, prompt, completion int64, cost float64) {
	for lang, batches := range pl.batches {
		for _, b := range batches {
			n++
			prompt += ai.EstimateTokens(pl.system[lang]) + ai.EstimateTokens(ai.ExtractUserPrompt(b))
			completion += int64(len(b) * ai.ExtractCompletionTokens)
		}
	}
	return n, prompt, completion, st.Cost(prompt, completion)
}

// EstimateExtract predicts the work and cost of extracting glossary terms from a module group's long strings
func (s *Service) EstimateExtract(ctx context.Context, p *models.Project, groupID int64) (*ExtractEstimate, error) {
	if !p.Role.CanManage() {
		return nil, apperr.ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return nil, err
	}
	est := &ExtractEstimate{GroupID: groupID, Running: s.extracting(p.ID)}
	if st == nil {
		return est, nil
	}
	est.Configured, est.Model = true, st.Model
	pl, err := s.planExtract(ctx, p.ID, groupID)
	if err != nil {
		return nil, err
	}
	est.Strings, est.ToSend = pl.strings, pl.toSend
	est.Done = max(pl.strings-pl.toSend, 0)
	est.Batches, est.PromptTokens, est.CompletionTokens, est.Cost = pl.estimate(st)
	est.Budget = st.MonthlyBudget
	if est.MonthSpent, err = s.MonthSpend(ctx, p); err != nil {
		return nil, err
	}
	if st.MonthlyBudget > 0 {
		est.BudgetLeft = st.MonthlyBudget - est.MonthSpent
		est.OverBudget = est.Cost > est.BudgetLeft
	}
	return est, nil
}

// extractLock returns the project's extraction lock: one run per project at a time
func (s *Service) extractLock(projectID int64) *sync.Mutex {
	m, _ := s.extractLocks.LoadOrStore(projectID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (s *Service) extracting(projectID int64) bool {
	m := s.extractLock(projectID)
	if m.TryLock() {
		m.Unlock()
		return false
	}
	return true
}

// StartExtract starts extracting glossary terms from a module group's long strings in the background; strings that
// are new after later syncs of the group are extracted too
func (s *Service) StartExtract(ctx context.Context, user *models.User, p *models.Project, groupID int64) (*models.TermExtractionJob, error) {
	if !p.Role.CanManage() {
		return nil, apperr.ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, apperr.New("err.ai_not_configured")
	}
	g, err := s.glossary.GetGroup(ctx, groupID)
	if err != nil || g.ProjectID != p.ID {
		return nil, apperr.ErrNotFound
	}
	cl, err := s.client(st)
	if err != nil {
		return nil, err
	}
	lock := s.extractLock(p.ID)
	if !lock.TryLock() {
		return nil, apperr.New("err.extract_running")
	}
	pl, err := s.planExtract(ctx, p.ID, groupID)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if st.MonthlyBudget > 0 {
		_, _, _, cost := pl.estimate(st)
		spent, err := s.MonthSpend(ctx, p)
		if err != nil {
			lock.Unlock()
			return nil, err
		}
		if pl.toSend > 0 && spent+cost > st.MonthlyBudget {
			lock.Unlock()
			return nil, apperr.New("err.ai_budget", st.MonthlyBudget-spent)
		}
	}
	if err := s.glossary.SetGroupAITerms(ctx, groupID, true); err != nil {
		lock.Unlock()
		return nil, err
	}
	job := &models.TermExtractionJob{ProjectID: p.ID, GroupID: &groupID, UserID: &user.ID, Status: models.AIJobRunning,
		Model: st.Model, Total: pl.toSend}
	if err := s.repo.CreateExtractJob(ctx, job); err != nil {
		lock.Unlock()
		return nil, err
	}
	go func() {
		defer lock.Unlock()
		s.runExtract(job, st, cl, pl)
	}()
	return job, nil
}

// AfterImport is called (in the background) after a module group was imported or synced: the new imports get their
// AI glossary rows from the stored results, and when extraction was started for the group, strings that are new or
// changed are extracted (within the monthly budget) first.
func (s *Service) AfterImport(projectID, groupID int64, imports []*models.ModuleGroupImport) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	g, err := s.glossary.GetGroup(ctx, groupID)
	if err != nil || !g.AITerms {
		return
	}
	for _, imp := range imports { // free: only stored results
		if _, err := s.rebuildAITerms(ctx, imp); err != nil {
			log.Printf("term extraction: rebuild import %d: %v", imp.ID, err)
		}
	}
	p := &models.Project{ID: projectID}
	st, err := s.Settings(ctx, p)
	if err != nil || st == nil {
		return
	}
	cl, err := s.client(st)
	if err != nil {
		return
	}
	lock := s.extractLock(projectID)
	lock.Lock() // after a run that is still going
	defer lock.Unlock()
	pl, err := s.planExtract(ctx, projectID, groupID)
	if err != nil || pl.toSend == 0 {
		return
	}
	if st.MonthlyBudget > 0 {
		if spent, err := s.MonthSpend(ctx, p); err != nil || spent >= st.MonthlyBudget {
			return
		}
	}
	job := &models.TermExtractionJob{ProjectID: projectID, GroupID: &groupID, Status: models.AIJobRunning,
		Model: st.Model, Total: pl.toSend}
	if err := s.repo.CreateExtractJob(ctx, job); err != nil {
		log.Printf("term extraction: group %d: %v", groupID, err)
		return
	}
	s.runExtract(job, st, cl, pl)
}

// CancelExtract stops the project's running extraction (finished batches are kept)
func (s *Service) CancelExtract(ctx context.Context, p *models.Project) error {
	if !p.Role.CanManage() {
		return apperr.ErrForbidden
	}
	if c, ok := s.extractCancels.Load(p.ID); ok {
		c.(context.CancelFunc)()
	}
	return nil
}

// LatestExtract returns the project's newest extraction run, or nil
func (s *Service) LatestExtract(ctx context.Context, p *models.Project) (*models.TermExtractionJob, error) {
	j, err := s.repo.LatestExtractJob(ctx, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return j, err
}

type extractBatch struct {
	lang  string
	items []ai.ExtractItem
}

func (s *Service) runExtract(job *models.TermExtractionJob, st *models.AISettings, cl chatClient, pl *extractPlan) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.extractCancels.Store(job.ProjectID, cancel)
	defer s.extractCancels.Delete(job.ProjectID)

	budgetLeft := -1.0
	if st.MonthlyBudget > 0 {
		now := time.Now().UTC()
		spent, _ := s.repo.Spend(ctx, job.ProjectID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
		budgetLeft = st.MonthlyBudget - spent + job.Cost
	}
	var queue []extractBatch
	langs := make([]string, 0, len(pl.batches))
	for l := range pl.batches {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		for _, b := range pl.batches[l] {
			queue = append(queue, extractBatch{l, b})
		}
	}

	var mu sync.Mutex // guards job and fatal
	var fatal string
	work := make(chan extractBatch)
	var wg sync.WaitGroup
	for range extractParallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range work {
				content, u, err := cl.Chat(ctx, pl.system[b.lang], ai.ExtractUserPrompt(b.items))
				mu.Lock()
				job.PromptTokens += u.PromptTokens
				job.CompletionTokens += u.CompletionTokens
				job.Cost = st.Cost(job.PromptTokens, job.CompletionTokens)
				mu.Unlock()
				if err != nil {
					if ctx.Err() == nil {
						mu.Lock()
						if fatal == "" {
							var ue *apperr.UserError
							if errors.As(translateAIError(err), &ue) {
								fatal = ue.Key + "|" + joinArgs(ue.Args)
							} else {
								fatal = err.Error()
							}
						}
						mu.Unlock()
						cancel()
					}
					continue
				}
				found := map[string][]ai.Pair{}
				answers, perr := ai.ParseExtractResponse(content)
				n := 0
				for _, it := range b.items {
					pairs, ok := answers[it.ID]
					if perr != nil || !ok {
						continue // not answered: sent again next time
					}
					kept := ai.CheckPairs(it, pairs)
					found[pl.hashes[b.lang][it.ID]] = kept
					n += len(kept)
				}
				if err := s.repo.SaveExtractedPairs(ctx, b.lang, st.Model, found); err != nil {
					log.Printf("term extraction job %d: save: %v", job.ID, err)
				}
				mu.Lock()
				job.Processed += len(b.items)
				job.Pairs += n
				over := budgetLeft >= 0 && job.Cost >= budgetLeft
				if over && fatal == "" {
					fatal = "err.ai_budget_reached"
				}
				_ = s.repo.UpdateExtractJob(context.Background(), job)
				mu.Unlock()
				if over {
					cancel()
				}
			}
		}()
	}
	for _, b := range queue {
		if ctx.Err() != nil {
			break
		}
		select {
		case work <- b:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()

	// whatever was found becomes glossary rows, also after a stop
	bg, cancelBg := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancelBg()
	for _, imp := range pl.imports {
		if _, err := s.rebuildAITerms(bg, imp); err != nil {
			log.Printf("term extraction job %d: rebuild import %d: %v", job.ID, imp.ID, err)
		}
	}
	now := time.Now()
	job.FinishedAt = &now
	switch {
	case fatal != "":
		job.Status, job.Error = models.AIJobFailed, fatal
	case ctx.Err() != nil:
		job.Status = models.AIJobCancelled
	default:
		job.Status = models.AIJobDone
	}
	if err := s.repo.UpdateExtractJob(bg, job); err != nil {
		log.Printf("term extraction job %d: %v", job.ID, err)
	}
}
