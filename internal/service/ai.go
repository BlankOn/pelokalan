package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"pelokalan/internal/ai"
	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/repository"
)

// Defaults suggested when a project first configures AI. Prices are USD per
// 1M tokens and must be checked against the provider's current price list.
const (
	DefaultAIModel       = "gpt-4o-mini"
	DefaultAIPriceInput  = 0.15
	DefaultAIPriceOutput = 0.60
)

// Batch limits for model requests
const (
	aiBatchItems = 25
	aiBatchChars = 6000
)

// AIService runs LLM pre-translation with glossary constraints
type AIService struct {
	repo         repository.AIRepository
	catalogs     repository.CatalogRepository
	glossary     repository.GlossaryRepository
	box          *ai.Box
	allowPrivate bool
	// history records versions before and after a job (optional)
	history historyRecorder
	// contexts supplies the PO file's Konteks note for the prompt (optional)
	contexts fileContexts
	// newClient is replaceable in tests
	newClient func(s *models.AISettings, key string) chatClient
}

type historyRecorder interface {
	AutoCommitByID(ctx context.Context, authorID, catalogID int64, message string)
}

type fileContexts interface {
	ForCatalog(ctx context.Context, c *models.Catalog) (*models.ModuleContext, error)
}

// SetContexts makes prompts include the PO file's Konteks note
func (s *AIService) SetContexts(c fileContexts) { s.contexts = c }

// SetHistory makes jobs record catalog versions before and after running
func (s *AIService) SetHistory(h historyRecorder) { s.history = h }

type chatClient interface {
	Chat(ctx context.Context, system, user string) (string, ai.Usage, error)
	Ping(ctx context.Context) error
}

// NewAIService creates the AI service
func NewAIService(repo repository.AIRepository, catalogs repository.CatalogRepository, glossaryRepo repository.GlossaryRepository,
	box *ai.Box, allowPrivate bool) *AIService {
	s := &AIService{repo: repo, catalogs: catalogs, glossary: glossaryRepo, box: box, allowPrivate: allowPrivate}
	s.newClient = func(st *models.AISettings, key string) chatClient {
		return ai.NewClient(st.BaseURL, key, st.Model, s.allowPrivate)
	}
	return s
}

// RecoverJobs marks jobs interrupted by a restart as failed
func (s *AIService) RecoverJobs(ctx context.Context) error { return s.repo.FailRunning(ctx) }

// Settings returns the project's AI settings, or nil when not configured
func (s *AIService) Settings(ctx context.Context, p *models.Project) (*models.AISettings, error) {
	st, err := s.repo.GetSettings(ctx, p.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return st, err
}

// AISettingsInput is the settings form
type AISettingsInput struct {
	BaseURL       string
	Model         string
	APIKey        string // empty keeps the stored key
	PriceInput    string
	PriceOutput   string
	MonthlyBudget string
	ExtraContext  string // project-wide guidance for the model
}

func parseMoney(v string) (float64, bool) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
	if v == "" {
		return 0, true
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil && f >= 0 && f < 1e6
}

// SaveSettings stores the AI settings (owner or manager)
func (s *AIService) SaveSettings(ctx context.Context, user *models.User, p *models.Project, in AISettingsInput) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	current, err := s.Settings(ctx, p)
	if err != nil {
		return err
	}
	st := &models.AISettings{ProjectID: p.ID, UpdatedBy: &user.ID}
	st.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if st.BaseURL == "" {
		st.BaseURL = ai.DefaultBaseURL
	}
	if _, err := glossary.ValidateURL(st.BaseURL); err != nil {
		return userErr("err.ai_base_url")
	}
	st.Model = strings.TrimSpace(in.Model)
	if st.Model == "" || len(st.Model) > 100 || strings.ContainsAny(st.Model, " /?#") {
		return userErr("err.ai_model")
	}
	var ok1, ok2, ok3 bool
	st.PriceInput, ok1 = parseMoney(in.PriceInput)
	st.PriceOutput, ok2 = parseMoney(in.PriceOutput)
	st.MonthlyBudget, ok3 = parseMoney(in.MonthlyBudget)
	if !ok1 || !ok2 || !ok3 {
		return userErr("err.ai_price")
	}
	st.ExtraContext = strings.TrimSpace(strings.ReplaceAll(in.ExtraContext, "\r\n", "\n"))
	if len([]rune(st.ExtraContext)) > ai.MaxProjectContext {
		return userErr("err.ai_context_length", ai.MaxProjectContext)
	}
	key := strings.TrimSpace(in.APIKey)
	switch {
	case key != "":
		if len(key) < 10 || len(key) > 500 || strings.ContainsAny(key, " \n\r\t") {
			return userErr("err.ai_key")
		}
		if st.APIKeyEnc, err = s.box.Seal(key); err != nil {
			return err
		}
		st.APIKeyHint = ai.KeyHint(key)
	case current != nil:
		st.APIKeyEnc, st.APIKeyHint = current.APIKeyEnc, current.APIKeyHint
	default:
		return userErr("err.ai_key_required")
	}
	return s.repo.SaveSettings(ctx, st)
}

// RemoveSettings deletes the stored key and settings
func (s *AIService) RemoveSettings(ctx context.Context, p *models.Project) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	return s.repo.DeleteSettings(ctx, p.ID)
}

func (s *AIService) client(st *models.AISettings) (chatClient, error) {
	key, err := s.box.Open(st.APIKeyEnc)
	if err != nil {
		return nil, userErr("err.ai_key_unreadable")
	}
	return s.newClient(st, key), nil
}

// TestConnection checks the key and model against the API
func (s *AIService) TestConnection(ctx context.Context, p *models.Project) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return err
	}
	if st == nil {
		return userErr("err.ai_not_configured")
	}
	c, err := s.client(st)
	if err != nil {
		return err
	}
	return translateAIError(c.Ping(ctx))
}

func translateAIError(err error) error {
	var apiErr *ai.APIError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ai.ErrUnauthorized):
		return userErr("err.ai_unauthorized")
	case errors.As(err, &apiErr):
		return userErr("err.ai_api", apiErr.Message)
	default:
		return userErr("err.ai_api", err.Error())
	}
}

// MonthSpend returns the project's AI cost since the start of the month (UTC)
func (s *AIService) MonthSpend(ctx context.Context, p *models.Project) (float64, error) {
	now := time.Now().UTC()
	return s.repo.Spend(ctx, p.ID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
}

// AIOptions select what to pre-translate
type AIOptions struct {
	UseMemory    bool  // fill exact matches from the user's other files first
	UseAI        bool  // send what is left to the language model
	IncludeFuzzy bool  // also redo entries marked as needing work
	VersionID    int64 // glossary version; 0 = all versions, -1 = no glossary
	MarkFuzzy    bool  // mark every result as needing review
}

// aiPlan is the work prepared for a run
type aiPlan struct {
	memory   map[int64]string // entry id -> translation memory match
	direct   map[int64]string // entry id -> glossary translation
	items    []ai.Item
	versions map[int64]int // entry id -> entry version when planned
	batches  [][]ai.Item
	system   string
	terms    int
}

// AIEstimate is shown before starting a run
type AIEstimate struct {
	Configured       bool
	UseAI            bool
	Model            string
	Entries          int
	FromMemory       int
	FromGlossary     int
	Unmatched        int // left untranslated when the model is not used
	ToModel          int
	Batches          int
	GlossaryTerms    int
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	MonthSpent       float64
	Budget           float64
	BudgetLeft       float64
	OverBudget       bool
}

func (s *AIService) plan(ctx context.Context, p *models.Project, c *models.Catalog, opts AIOptions) (*aiPlan, error) {
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return nil, err
	}
	var g *ai.Glossary
	if opts.VersionID >= 0 {
		pref, err := s.glossary.PreferredTerms(ctx, p.ID, c.Language, opts.VersionID)
		if err != nil {
			return nil, err
		}
		terms := make([]ai.Term, len(pref))
		for i, t := range pref {
			terms[i] = ai.Term{Source: t.Source, Translation: t.Translation, Share: t.Share, Words: t.Words}
		}
		g = ai.NewGlossary(terms)
	}

	memory := map[int64]string{}
	if opts.UseMemory {
		if memory, err = s.catalogs.MemoryMatches(ctx, c.ID, c.OwnerID, c.Language); err != nil {
			return nil, err
		}
	}

	// the file's Konteks note: sent with the prompt, and it outranks glossary terms it talks about
	fileContext := ""
	if s.contexts != nil {
		if n, err := s.contexts.ForCatalog(ctx, c); err == nil && n != nil {
			fileContext = n.Note
		}
	}
	// project-wide guidance from the AI settings
	projectContext := ""
	if st, err := s.Settings(ctx, p); err == nil && st != nil {
		projectContext = st.ExtraContext
	}
	contextWords := ai.ContextWords(projectContext + "\n" + fileContext)

	pl := &aiPlan{memory: map[int64]string{}, direct: map[int64]string{}, versions: map[int64]int{}, terms: g.Len()}
	for _, e := range entries {
		switch e.Status() {
		case "translated":
			continue
		case "fuzzy":
			if !opts.IncludeFuzzy {
				continue
			}
		}
		pl.versions[e.ID] = e.Version
		if tr, ok := memory[e.ID]; ok {
			pl.memory[e.ID] = tr
			continue
		}
		// a whole-string glossary match is applied without the model, unless the note
		// talks about one of its words (then the model decides with the note) or the entry
		// already has a draft translation to review
		draft := e.Fuzzy && hasText(e.MsgStr)
		if !e.IsPlural() && !draft && !ai.Mentioned(e.MsgID, contextWords) {
			if tr, ok := g.Exact(e.MsgID); ok {
				pl.direct[e.ID] = tr
				continue
			}
		}
		// an entry that needs review keeps its translation as a draft for the model to improve
		var current []string
		if draft {
			current = e.MsgStr
		}
		pl.items = append(pl.items, buildItem(e, c, g, contextWords, current))
	}
	pl.batches = ai.Batch(pl.items, aiBatchItems, aiBatchChars)
	pl.system = ai.SystemPrompt(languageLabel(p.SourceLanguage), languageLabel(c.Language), projectContext, fileContext)
	return pl, nil
}

// buildItem prepares one entry for the model: source, plural forms, context, notes and
// glossary hints (optional where the Konteks talks about them). A non-empty current is an
// existing translation to improve; its own word choices then outrank all glossary hints.
func buildItem(e *models.Entry, c *models.Catalog, g *ai.Glossary, contextWords map[string]bool, current []string) ai.Item {
	it := ai.Item{ID: e.ID, Source: e.MsgID, Plural: e.MsgIDPlural, Context: e.ContextValue(), Flags: e.FlagList()}
	notes := strings.TrimSpace(e.ExtractedComments)
	if len(notes) > 500 {
		notes = notes[:500]
	}
	it.Notes = notes
	if e.IsPlural() {
		it.Forms = c.NPlurals
	}
	it.Glossary = ai.MatchCase(ai.MarkOptional(g.Matches(e.MsgID+" "+e.MsgIDPlural), contextWords), e.MsgID+" "+e.MsgIDPlural)
	// a draft damaged by an earlier run (control characters) is not worth improving
	if hasText(current) && !ai.HasControlChars(current) {
		it.Current = current
		for i := range it.Glossary {
			it.Glossary[i].Optional = true
		}
	}
	return it
}

func languageLabel(code string) string {
	return po.LanguageName(code) + " (" + code + ")"
}

// Estimate predicts the work and cost of pre-translating a catalog
func (s *AIService) Estimate(ctx context.Context, p *models.Project, c *models.Catalog, opts AIOptions) (*AIEstimate, error) {
	st, err := s.Settings(ctx, p)
	if err != nil {
		return nil, err
	}
	pl, err := s.plan(ctx, p, c, opts)
	if err != nil {
		return nil, err
	}
	est := &AIEstimate{
		Configured:    st != nil,
		UseAI:         opts.UseAI && st != nil,
		FromMemory:    len(pl.memory),
		FromGlossary:  len(pl.direct),
		GlossaryTerms: pl.terms,
	}
	if !est.UseAI {
		est.Unmatched = len(pl.items)
		est.Entries = est.FromMemory + est.FromGlossary
		return est, nil
	}
	est.Entries = est.FromMemory + est.FromGlossary + len(pl.items)
	est.ToModel, est.Batches = len(pl.items), len(pl.batches)
	u := ai.EstimateBatches(pl.batches, pl.system)
	est.PromptTokens, est.CompletionTokens = u.PromptTokens, u.CompletionTokens
	if st != nil {
		est.Model = st.Model
		est.Cost = st.Cost(u.PromptTokens, u.CompletionTokens)
		est.Budget = st.MonthlyBudget
		if est.MonthSpent, err = s.MonthSpend(ctx, p); err != nil {
			return nil, err
		}
		if st.MonthlyBudget > 0 {
			est.BudgetLeft = st.MonthlyBudget - est.MonthSpent
			est.OverBudget = est.Cost > est.BudgetLeft
		}
	}
	return est, nil
}

// LatestJob returns the most recent job of a catalog, or nil
func (s *AIService) LatestJob(ctx context.Context, c *models.Catalog) (*models.AIJob, error) {
	j, err := s.repo.LatestJob(ctx, c.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return j, err
}

// Job returns a job of the catalog
func (s *AIService) Job(ctx context.Context, c *models.Catalog, id int64) (*models.AIJob, error) {
	j, err := s.repo.GetJob(ctx, id)
	if err != nil || j.CatalogID != c.ID {
		return nil, ErrNotFound
	}
	return j, nil
}

// Cancel asks a running job to stop; the job aborts its current model request within a
// couple of seconds (see watchCancel)
func (s *AIService) Cancel(ctx context.Context, c *models.Catalog, id int64) error {
	j, err := s.Job(ctx, c, id)
	if err != nil {
		return err
	}
	return s.repo.RequestCancel(ctx, j.ID)
}

// Start begins pre-translating a catalog in the background
func (s *AIService) Start(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, opts AIOptions) (*models.AIJob, error) {
	if c.IsTemplate || !c.WritableBy(user.ID) {
		return nil, ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return nil, err
	}
	if opts.UseAI && st == nil {
		return nil, userErr("err.ai_not_configured")
	}
	if last, err := s.LatestJob(ctx, c); err != nil {
		return nil, err
	} else if last != nil && last.IsRunning() {
		return nil, userErr("err.ai_job_running")
	}
	var cl chatClient
	if opts.UseAI {
		if cl, err = s.client(st); err != nil {
			return nil, err
		}
	}
	pl, err := s.plan(ctx, p, c, opts)
	if err != nil {
		return nil, err
	}
	if !opts.UseAI {
		// without a model, strings with no memory or glossary match stay empty
		pl.items, pl.batches = nil, nil
	}
	if len(pl.memory)+len(pl.direct)+len(pl.items) == 0 {
		return nil, userErr("err.ai_nothing")
	}
	model, estimated := "", 0.0
	if opts.UseAI {
		u := ai.EstimateBatches(pl.batches, pl.system)
		model, estimated = st.Model, st.Cost(u.PromptTokens, u.CompletionTokens)
	}
	if opts.UseAI && st.MonthlyBudget > 0 {
		spent, err := s.MonthSpend(ctx, p)
		if err != nil {
			return nil, err
		}
		if spent+estimated > st.MonthlyBudget {
			return nil, userErr("err.ai_budget", st.MonthlyBudget-spent)
		}
	}

	job := &models.AIJob{
		ProjectID:     p.ID,
		CatalogID:     c.ID,
		UserID:        &user.ID,
		Status:        models.AIJobRunning,
		Model:         model,
		Total:         len(pl.memory) + len(pl.direct) + len(pl.items),
		EstimatedCost: estimated,
	}
	if s.history != nil {
		s.history.AutoCommitByID(ctx, user.ID, c.ID, CommitMsgBeforeAI)
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	go s.run(job, st, cl, c, pl, opts, user.ID)
	return job, nil
}

func (s *AIService) run(job *models.AIJob, st *models.AISettings, cl chatClient, c *models.Catalog, pl *aiPlan, opts AIOptions, userID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ai job %d panicked: %v", job.ID, r)
			job.Status, job.Error = models.AIJobFailed, "internal error"
			s.finish(job)
		}
	}()

	// 1. translation memory: exact matches from the user's other files
	for id, tr := range pl.memory {
		switch s.apply(ctx, c, id, pl.versions[id], []string{tr}, opts.MarkFuzzy, userID, models.OriginMemory) {
		case applyOK:
			job.FromMemory++
		case applySkipped:
			job.Skipped++
		}
		job.Processed++
	}

	// 2. glossary exact matches: no model call needed
	for id, tr := range pl.direct {
		switch s.apply(ctx, c, id, pl.versions[id], []string{tr}, opts.MarkFuzzy, userID, models.OriginGlossary) {
		case applyOK:
			job.FromGlossary++
		case applySkipped:
			job.Skipped++
		}
		job.Processed++
	}
	_ = s.repo.UpdateJob(ctx, job)

	// 3. model translations, batch by batch
	budgetLeft := -1.0
	if st != nil && st.MonthlyBudget > 0 {
		now := time.Now().UTC()
		spent, _ := s.repo.Spend(ctx, job.ProjectID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
		budgetLeft = st.MonthlyBudget - spent
	}
	// model requests can take a minute or more; a cancel request aborts the one in flight
	reqCtx, abort := context.WithCancel(ctx)
	defer abort()
	go s.watchCancel(reqCtx, job.ID, abort)
	for _, batch := range pl.batches {
		if cancelled, _ := s.repo.IsCancelRequested(ctx, job.ID); cancelled || reqCtx.Err() != nil {
			job.Status = models.AIJobCancelled
			s.finish(job)
			return
		}
		if budgetLeft >= 0 && job.Cost >= budgetLeft {
			job.Status, job.Error = models.AIJobFailed, "err.ai_budget_reached"
			s.finish(job)
			return
		}
		content, usage, err := cl.Chat(reqCtx, pl.system, ai.UserPrompt(batch))
		job.PromptTokens += usage.PromptTokens
		job.CompletionTokens += usage.CompletionTokens
		job.Cost = st.Cost(job.PromptTokens, job.CompletionTokens)
		if reqCtx.Err() != nil && ctx.Err() == nil {
			// cancelled while the model was answering: keep earlier batches, write nothing more
			job.Status = models.AIJobCancelled
			s.finish(job)
			return
		}
		if err != nil {
			job.Status = models.AIJobFailed
			if ue, ok := AsUserError(translateAIError(err)); ok {
				job.Error = ue.Key + "|" + joinArgs(ue.Args)
			}
			s.finish(job)
			return
		}
		results, err := ai.ParseResponse(content)
		if err != nil {
			log.Printf("ai job %d: %v", job.ID, err)
		}
		for _, it := range batch {
			forms, ok := results[it.ID]
			if !ok {
				job.Skipped++
				job.Processed++
				continue
			}
			forms = ai.FitForms(forms, it.Plural != "", c.NPlurals)
			problems := ai.Verify(it, forms, c.NPlurals, c.PluralForms)
			if len(problems) > 0 && (problems[0] == ai.ProblemEmpty || problems[0] == ai.ProblemCorrupt) {
				job.Skipped++
				job.Processed++
				continue
			}
			switch s.apply(ctx, c, it.ID, pl.versions[it.ID], forms, opts.MarkFuzzy || len(problems) > 0, userID, models.OriginAI) {
			case applyOK:
				job.Translated++
				if len(problems) > 0 {
					job.Flagged++
				}
			case applySkipped:
				job.Skipped++
			}
			job.Processed++
		}
		_ = s.repo.UpdateJob(ctx, job)
	}
	job.Status = models.AIJobDone
	s.finish(job)
}

// cancelPollInterval is how often a running job checks for a cancel request
var cancelPollInterval = 1500 * time.Millisecond

// watchCancel calls abort as soon as a cancel is requested for the job
func (s *AIService) watchCancel(ctx context.Context, jobID int64, abort context.CancelFunc) {
	t := time.NewTicker(cancelPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cancelled, err := s.repo.IsCancelRequested(ctx, jobID); err == nil && cancelled {
				abort()
				return
			}
		}
	}
}

func joinArgs(args []any) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, strings.ReplaceAll(toString(a), "|", "/"))
	}
	return strings.Join(parts, "|")
}

func toString(v any) string { return fmt.Sprint(v) }

func (s *AIService) finish(job *models.AIJob) {
	now := time.Now()
	job.FinishedAt = &now
	if err := s.repo.UpdateJob(context.Background(), job); err != nil {
		log.Printf("ai job %d: save result: %v", job.ID, err)
	}
	// record what the model changed as its own version
	if s.history != nil && job.UserID != nil && job.FromMemory+job.FromGlossary+job.Translated > 0 {
		msg := CommitMsgAfterPretranslate
		if job.Translated > 0 {
			msg = CommitMsgAfterAI + "|" + job.Model
		}
		s.history.AutoCommitByID(context.Background(), *job.UserID, job.CatalogID, msg)
	}
}

type applyResult int

const (
	applyOK applyResult = iota
	applySkipped
	applyFailed
)

// apply stores a translation unless the entry changed since the run was planned
func (s *AIService) apply(ctx context.Context, c *models.Catalog, id int64, version int, forms []string, fuzzy bool, userID int64, origin string) applyResult {
	e, err := s.catalogs.GetEntry(ctx, id)
	if err != nil || e.CatalogID != c.ID || e.Obsolete {
		return applySkipped
	}
	if e.Version != version {
		return applySkipped // someone edited it meanwhile
	}
	// keep the replaced translation for comparison: the last text a person wrote (an earlier
	// machine result does not replace it)
	newForms := fitForms(forms, e.IsPlural(), c.NPlurals)
	if hasText(e.MsgStr) && !sameForms(e.MsgStr, newForms) && (e.Origin == "" || e.ReplacedMsgStr == nil) {
		e.ReplacedMsgStr = e.MsgStr
	}
	e.MsgStr = newForms
	e.Fuzzy = fuzzy
	e.Origin = origin
	if err := s.catalogs.UpdateEntry(ctx, e, userID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return applySkipped
		}
		log.Printf("ai: save entry %d: %v", id, err)
		return applyFailed
	}
	return applyOK
}

func sameForms(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasText(forms []string) bool {
	for _, f := range forms {
		if strings.TrimSpace(f) != "" {
			return true
		}
	}
	return false
}

// EntryAIResult is the model's translation of one entry, not yet saved
type EntryAIResult struct {
	Forms    []string `json:"forms"`
	Flagged  bool     `json:"flagged"` // failed a check (glossary, placeholders): mark for review
	Problems []string `json:"problems,omitempty"`
}

// TranslateEntry asks the model to translate one entry for its owner, with the same glossary,
// Konteks and draft rules as pre-translation. current is the text in the editor (a draft to
// improve when not empty). Nothing is saved; the cost is recorded toward the monthly budget.
func (s *AIService) TranslateEntry(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, e *models.Entry, current []string) (*EntryAIResult, error) {
	if c.IsTemplate || !c.WritableBy(user.ID) {
		return nil, ErrForbidden
	}
	st, err := s.Settings(ctx, p)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, userErr("err.ai_not_configured")
	}
	cl, err := s.client(st)
	if err != nil {
		return nil, err
	}
	pref, err := s.glossary.PreferredTerms(ctx, p.ID, c.Language, 0)
	if err != nil {
		return nil, err
	}
	terms := make([]ai.Term, len(pref))
	for i, t := range pref {
		terms[i] = ai.Term{Source: t.Source, Translation: t.Translation, Share: t.Share, Words: t.Words}
	}
	fileContext := ""
	if s.contexts != nil {
		if n, err := s.contexts.ForCatalog(ctx, c); err == nil && n != nil {
			fileContext = n.Note
		}
	}
	contextWords := ai.ContextWords(st.ExtraContext + "\n" + fileContext)
	it := buildItem(e, c, ai.NewGlossary(terms), contextWords, current)
	system := ai.SystemPrompt(languageLabel(p.SourceLanguage), languageLabel(c.Language), st.ExtraContext, fileContext)
	batch := []ai.Item{it}

	if st.MonthlyBudget > 0 {
		u := ai.EstimateBatches([][]ai.Item{batch}, system)
		spent, err := s.MonthSpend(ctx, p)
		if err != nil {
			return nil, err
		}
		if spent+st.Cost(u.PromptTokens, u.CompletionTokens) > st.MonthlyBudget {
			return nil, userErr("err.ai_budget", st.MonthlyBudget-spent)
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	content, usage, err := cl.Chat(callCtx, system, ai.UserPrompt(batch))
	// record the cost even when the answer is unusable
	now := time.Now()
	job := &models.AIJob{ProjectID: p.ID, CatalogID: c.ID, UserID: &user.ID, Status: models.AIJobDone, Model: st.Model,
		Total: 1, Processed: 1, Single: true, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		Cost: st.Cost(usage.PromptTokens, usage.CompletionTokens), FinishedAt: &now}
	if usage.PromptTokens+usage.CompletionTokens > 0 {
		if err := s.repo.CreateJob(context.Background(), job); err == nil {
			_ = s.repo.UpdateJob(context.Background(), job)
		}
	}
	if err != nil {
		return nil, translateAIError(err)
	}
	results, err := ai.ParseResponse(content)
	if err != nil || len(results[e.ID]) == 0 {
		return nil, userErr("err.ai_entry_empty")
	}
	forms := ai.FitForms(results[e.ID], e.IsPlural(), c.NPlurals)
	problems := ai.Verify(it, forms, c.NPlurals, c.PluralForms)
	if len(problems) > 0 && (problems[0] == ai.ProblemEmpty || problems[0] == ai.ProblemCorrupt) {
		return nil, userErr("err.ai_entry_empty")
	}
	return &EntryAIResult{Forms: forms, Flagged: len(problems) > 0, Problems: problems}, nil
}
