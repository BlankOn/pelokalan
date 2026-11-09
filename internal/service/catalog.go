package service

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"pelokalan/internal/config"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/repository"
)

// CatalogService handles translation files and their entries
type CatalogService struct {
	catalogs repository.CatalogRepository
	commits  repository.CommitRepository
	projects *ProjectService
	site     config.SiteConfig
}

// NewCatalogService creates a new catalog service
func NewCatalogService(catalogs repository.CatalogRepository, commits repository.CommitRepository, projects *ProjectService, site config.SiteConfig) *CatalogService {
	return &CatalogService{catalogs: catalogs, commits: commits, projects: projects, site: site}
}

// EntryFilter narrows an entry listing
type EntryFilter struct {
	// Statuses: an entry is listed when it matches any of them ("todo", "untranslated",
	// "fuzzy", "translated", "issues", "ai", "glossary", "memory"); none = all entries
	Statuses []string
	Query    string
	Order    string // "", "file", "source", "translation", "todo_first"
}

// EntryListing is the result of listing entries
type EntryListing struct {
	Entries  []*models.Entry
	Stats    models.CatalogStats
	Issues   int
	AI       int // entries translated by a language model, not yet saved by a person
	Glossary int // entries pre-translated from the glossary, not yet saved by a person
	Memory   int // entries pre-translated from translation memory, not yet saved by a person
}

// MergeResult summarizes an update from the template
type MergeResult struct {
	Added    int
	Kept     int
	Obsolete int
	Skipped  int // files of another module the template was not merged into
}

// ListAll returns every member's files in the project
func (s *CatalogService) ListAll(ctx context.Context, user *models.User, p *models.Project) ([]*models.Catalog, error) {
	list, err := s.catalogs.ListAllByProject(ctx, p.ID)
	for _, c := range list {
		c.Owned = user != nil && c.WritableBy(user.ID)
	}
	return list, err
}

// List returns the user's own files in the project (template first)
func (s *CatalogService) List(ctx context.Context, user *models.User, p *models.Project) ([]*models.Catalog, error) {
	return s.catalogs.ListByProject(ctx, p.ID, user.ID)
}

// Get returns a file and its project. Files belong to the project and can be
// read by anyone who can read the project (user may be nil); only the owner can
// change them, which c.Owned reports and every write method checks.
func (s *CatalogService) Get(ctx context.Context, user *models.User, id int64) (*models.Catalog, *models.Project, error) {
	c, err := s.catalogs.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	c.Owned = user != nil && c.WritableBy(user.ID)
	p, err := s.projects.GetPublic(ctx, user, c.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	if c.Stats, err = s.catalogs.Stats(ctx, c.ID); err != nil {
		return nil, nil, err
	}
	return c, p, nil
}

// Template returns the owner's template in the project, or nil
func (s *CatalogService) Template(ctx context.Context, p *models.Project, ownerID int64) (*models.Catalog, error) {
	c, err := s.catalogs.GetTemplate(ctx, p.ID, ownerID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return c, err
}

func cleanFilename(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return fallback
	}
	return name
}

// Upload imports a PO or POT file. A POT file (or a file without a language)
// becomes the project template; other files become a translation for their
// language. When language is not empty it overrides the file header.
func (s *CatalogService) Upload(ctx context.Context, user *models.User, p *models.Project, filename string, data []byte, language string) (*models.Catalog, *MergeResult, error) {
	if !p.Role.IsMember() {
		return nil, nil, ErrForbidden
	}
	pc, err := po.ParseBytes(data)
	if err != nil {
		var pe *po.ParseError
		if errors.As(err, &pe) {
			return nil, nil, userErr("err.parse_failed", pe.Line, pe.Msg)
		}
		return nil, nil, ErrInvalidFile
	}
	if len(pc.Entries) == 0 {
		return nil, nil, ErrEmptyCatalog
	}

	language = strings.TrimSpace(language)
	if language == "" {
		language = pc.HeaderField("Language")
	}
	isTemplate := strings.HasSuffix(strings.ToLower(filename), ".pot") || language == ""
	if isTemplate {
		language = ""
	} else if !validLanguageCode(language) {
		return nil, nil, ErrInvalidLanguage
	}

	c := &models.Catalog{
		ProjectID:      p.ID,
		OwnerID:        user.ID,
		IsTemplate:     isTemplate,
		Language:       language,
		OwnerUsername:  user.Username,
		ProjectSlug:    p.Slug,
		Filename:       cleanFilename(filename, "messages.po"),
		Header:         pc.Header,
		HeaderComments: strings.Join(pc.HeaderComments, "\n"),
		CreatedBy:      &user.ID,
	}
	if !isTemplate {
		c.Header = po.SetHeaderField(c.Header, "Language", language)
		c.PluralForms = pc.HeaderField("Plural-Forms")
		if c.PluralForms == "" {
			c.PluralForms = po.LookupLanguage(language).PluralForms
			c.Header = po.SetHeaderField(c.Header, "Plural-Forms", c.PluralForms)
		}
	}
	c.NPlurals = po.NPlurals(c.PluralForms)

	entries := make([]*models.Entry, 0, len(pc.Entries))
	for i, pe := range pc.Entries {
		if isTemplate {
			// a template never carries translations
			pe.Translation = emptyForms(pe.IsPlural(), 2)
			pe.Fuzzy = false
		}
		e := models.EntryFromPO(pe, i)
		e.UpdatedBy = &user.ID
		entries = append(entries, e)
	}

	// Re-uploading the template replaces it and refreshes every translation.
	if isTemplate {
		existing, err := s.Template(ctx, p, user.ID)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			c.ID = existing.ID
			if c.Filename, err = s.uniqueFilename(ctx, c, c.Filename); err != nil {
				return nil, nil, err
			}
			if err := s.catalogs.ReplaceEntries(ctx, c, entries); err != nil {
				return nil, nil, err
			}
			res, err := s.UpdateAllFromTemplate(ctx, user, p)
			return c, res, err
		}
		if c.Filename, err = s.uniqueFilename(ctx, c, c.Filename); err != nil {
			return nil, nil, err
		}
		if err := s.catalogs.Create(ctx, c, entries); err != nil {
			return nil, nil, err
		}
		s.SafetyCommit(ctx, user.ID, c, CommitMsgInitialTemplate)
		return c, nil, nil
	}

	var err2 error
	if c.Filename, err2 = s.uniqueFilename(ctx, c, c.Filename); err2 != nil {
		return nil, nil, err2
	}
	if err := s.catalogs.Create(ctx, c, entries); err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrFilenameTaken
		}
		return nil, nil, err
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgInitialUpload)
	return c, nil, nil
}

// Replace imports a new version of an existing translation file
func (s *CatalogService) Replace(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, filename string, data []byte) error {
	if !c.WritableBy(user.ID) || c.IsTemplate {
		return ErrForbidden
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgBeforeReplace)
	name, err := s.uniqueFilename(ctx, c, cleanFilename(filename, c.Filename))
	if err != nil {
		return err
	}
	c.Filename = name
	if err := s.replaceContent(ctx, user, c, data); err != nil {
		return err
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgUploaded)
	return nil
}

// replaceContent parses PO text and replaces the catalog's header and entries
func (s *CatalogService) replaceContent(ctx context.Context, user *models.User, c *models.Catalog, data []byte) error {
	pc, err := po.ParseBytes(data)
	if err != nil {
		var pe *po.ParseError
		if errors.As(err, &pe) {
			return userErr("err.parse_failed", pe.Line, pe.Msg)
		}
		return ErrInvalidFile
	}
	if len(pc.Entries) == 0 {
		return ErrEmptyCatalog
	}
	c.Header = pc.Header
	c.HeaderComments = strings.Join(pc.HeaderComments, "\n")
	if !c.IsTemplate {
		c.Header = po.SetHeaderField(c.Header, "Language", c.Language)
		if pf := pc.HeaderField("Plural-Forms"); pf != "" {
			c.PluralForms = pf
		} else {
			c.Header = po.SetHeaderField(c.Header, "Plural-Forms", c.PluralForms)
		}
		c.NPlurals = po.NPlurals(c.PluralForms)
	}
	entries := make([]*models.Entry, 0, len(pc.Entries))
	for i, pe := range pc.Entries {
		var origin string
		pe.Flags, origin = splitOriginFlag(pe.Flags)
		e := models.EntryFromPO(pe, i)
		e.Origin = origin
		e.UpdatedBy = &user.ID
		entries = append(entries, e)
	}
	return s.catalogs.ReplaceEntries(ctx, c, entries)
}

// CreateFromTemplate starts a new translation of the project template
func (s *CatalogService) CreateFromTemplate(ctx context.Context, user *models.User, p *models.Project, language string) (*models.Catalog, error) {
	if !p.Role.IsMember() {
		return nil, ErrForbidden
	}
	language = strings.TrimSpace(language)
	if !validLanguageCode(language) {
		return nil, ErrInvalidLanguage
	}
	tmpl, err := s.Template(ctx, p, user.ID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, ErrNoTemplate
	}
	tentries, err := s.catalogs.ListEntries(ctx, tmpl.ID, false)
	if err != nil {
		return nil, err
	}

	lang := po.LookupLanguage(language)
	base := strings.TrimSuffix(tmpl.Filename, filepath.Ext(tmpl.Filename))
	if base == "" {
		base = "messages"
	}
	now := time.Now().Format("2006-01-02 15:04-0700")
	header := tmpl.Header
	if header == "" {
		header = "Content-Type: text/plain; charset=UTF-8\nContent-Transfer-Encoding: 8bit\n"
	}
	header = po.SetHeaderField(header, "PO-Revision-Date", now)
	header = po.SetHeaderField(header, "Language-Team", lang.Name)
	header = po.SetHeaderField(header, "Language", language)
	header = po.SetHeaderField(header, "Content-Type", "text/plain; charset=UTF-8")
	header = po.SetHeaderField(header, "Plural-Forms", lang.PluralForms)

	c := &models.Catalog{
		ProjectID:      p.ID,
		OwnerID:        user.ID,
		OwnerUsername:  user.Username,
		ProjectSlug:    p.Slug,
		Language:       language,
		Filename:       language + ".po",
		Header:         header,
		HeaderComments: tmpl.HeaderComments,
		PluralForms:    lang.PluralForms,
		NPlurals:       po.NPlurals(lang.PluralForms),
		CreatedBy:      &user.ID,
	}
	if base != "messages" && base != language {
		c.Filename = base + "-" + language + ".po"
	}
	if c.Filename, err = s.uniqueFilename(ctx, c, c.Filename); err != nil {
		return nil, err
	}
	entries := make([]*models.Entry, 0, len(tentries))
	for i, te := range tentries {
		e := *te
		e.ID = 0
		e.Position = i
		e.Fuzzy = false
		e.Previous = ""
		e.TranslatorComments = ""
		e.UpdatedBy = nil
		e.MsgStr = emptyForms(te.IsPlural(), c.NPlurals)
		entries = append(entries, &e)
	}
	if err := s.catalogs.Create(ctx, c, entries); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrFilenameTaken
		}
		return nil, err
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgInitialTemplate)
	return c, nil
}

// UpdateFromTemplate merges the project template into a translation: new
// strings are added, existing translations kept and removed strings become obsolete.
func (s *CatalogService) UpdateFromTemplate(ctx context.Context, p *models.Project, c *models.Catalog) (*MergeResult, error) {
	if c.IsTemplate || !c.Owned {
		return nil, ErrForbidden
	}
	tmpl, err := s.Template(ctx, p, c.OwnerID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, ErrNoTemplate
	}
	tentries, err := s.catalogs.ListEntries(ctx, tmpl.ID, false)
	if err != nil {
		return nil, err
	}
	current, err := s.catalogs.ListEntries(ctx, c.ID, true)
	if err != nil {
		return nil, err
	}
	// A member keeps several modules in one project; never merge the
	// template into a file of another module (it would turn every string obsolete).
	if !templateFits(tentries, current) {
		return nil, ErrTemplateMismatch
	}
	s.SafetyCommit(ctx, c.OwnerID, c, CommitMsgBeforeMerge)
	merged, res := mergeEntries(tentries, current, c.NPlurals)
	return res, s.catalogs.ReplaceEntries(ctx, c, merged)
}

// UpdateAllFromTemplate merges the template into every translation of the project
func (s *CatalogService) UpdateAllFromTemplate(ctx context.Context, user *models.User, p *models.Project) (*MergeResult, error) {
	catalogs, err := s.catalogs.ListByProject(ctx, p.ID, user.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range catalogs {
		c.Owned = true
	}
	total := &MergeResult{}
	for _, c := range catalogs {
		if c.IsTemplate {
			continue
		}
		res, err := s.UpdateFromTemplate(ctx, p, c)
		if errors.Is(err, ErrTemplateMismatch) {
			total.Skipped++
			continue
		}
		if err != nil {
			return nil, err
		}
		total.Added += res.Added
		total.Kept += res.Kept
		total.Obsolete += res.Obsolete
	}
	return total, nil
}

// templateFits reports whether a translation belongs to the template's module:
// at least half of its current strings must still be in the template. Empty
// files fit any template.
func templateFits(tmpl, current []*models.Entry) bool {
	keys := make(map[string]bool, len(tmpl))
	for _, te := range tmpl {
		keys[te.Key()] = true
	}
	active, matched := 0, 0
	for _, ce := range current {
		if ce.Obsolete || ce.MsgID == "" {
			continue
		}
		active++
		if keys[ce.Key()] {
			matched++
		}
	}
	return active == 0 || matched*2 >= active
}

func mergeEntries(tmpl, current []*models.Entry, nplurals int) ([]*models.Entry, *MergeResult) {
	res := &MergeResult{}
	byKey := make(map[string]*models.Entry, len(current))
	var order []string
	for _, e := range current {
		k := e.Key()
		if _, dup := byKey[k]; !dup {
			order = append(order, k)
		}
		byKey[k] = e
	}

	out := make([]*models.Entry, 0, len(tmpl)+len(current))
	for _, te := range tmpl {
		ne := *te
		ne.ID = 0
		ne.Position = len(out)
		ne.Obsolete = false
		ne.Previous = ""
		ne.TranslatorComments = ""
		ne.Fuzzy = false
		ne.UpdatedBy = nil
		if ce, ok := byKey[te.Key()]; ok {
			delete(byKey, te.Key())
			ne.MsgStr = fitForms(ce.MsgStr, te.IsPlural(), nplurals)
			ne.Fuzzy = ce.Fuzzy || (ce.MsgIDPlural != te.MsgIDPlural && ce.IsTranslated())
			ne.TranslatorComments = ce.TranslatorComments
			ne.Previous = ce.Previous
			ne.UpdatedBy = ce.UpdatedBy
			ne.Origin = ce.Origin
			ne.ReplacedMsgStr = ce.ReplacedMsgStr
			res.Kept++
		} else {
			ne.MsgStr = emptyForms(te.IsPlural(), nplurals)
			res.Added++
		}
		out = append(out, &ne)
	}
	for _, k := range order {
		ce, ok := byKey[k]
		if !ok || !ce.IsTranslated() {
			continue // untranslated strings that disappeared are dropped
		}
		oe := *ce
		oe.ID = 0
		oe.Position = len(out)
		if !oe.Obsolete {
			res.Obsolete++
		}
		oe.Obsolete = true
		out = append(out, &oe)
	}
	return out, res
}

func emptyForms(plural bool, nplurals int) []string {
	if !plural {
		return []string{""}
	}
	if nplurals < 1 {
		nplurals = 2
	}
	return make([]string, nplurals)
}

func fitForms(forms []string, plural bool, nplurals int) []string {
	if !plural {
		if len(forms) == 0 {
			return []string{""}
		}
		return []string{forms[0]}
	}
	out := emptyForms(true, nplurals)
	copy(out, forms)
	return out
}

// Delete removes a file from the project
func (s *CatalogService) Delete(ctx context.Context, user *models.User, c *models.Catalog) error {
	if !c.WritableBy(user.ID) {
		return ErrForbidden
	}
	return s.catalogs.Delete(ctx, c.ID, user.ID)
}

// Entries lists, filters and validates the entries of a catalog
func (s *CatalogService) Entries(ctx context.Context, c *models.Catalog, f EntryFilter) (*EntryListing, error) {
	all, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return nil, err
	}
	l := &EntryListing{}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	for _, e := range all {
		e.Issues = po.Validate(e.ToPO(), c.NPlurals)
		l.Stats.Total++
		switch e.Status() {
		case "translated":
			l.Stats.Translated++
		case "fuzzy":
			l.Stats.Fuzzy++
		default:
			l.Stats.Untranslated++
		}
		if e.HasIssues() {
			l.Issues++
		}
		switch e.Origin {
		case models.OriginAI:
			l.AI++
		case models.OriginGlossary:
			l.Glossary++
		case models.OriginMemory:
			l.Memory++
		}
		if !matchAnyStatus(e, f.Statuses) || (q != "" && !matchQuery(e, q)) {
			continue
		}
		l.Entries = append(l.Entries, e)
	}
	sortEntries(l.Entries, f.Order)
	return l, nil
}

func matchAnyStatus(e *models.Entry, statuses []string) bool {
	if len(statuses) == 0 {
		return true
	}
	for _, s := range statuses {
		if matchStatus(e, s) {
			return true
		}
	}
	return false
}

func matchStatus(e *models.Entry, status string) bool {
	switch status {
	case "todo":
		return e.Status() != "translated"
	case "untranslated", "fuzzy", "translated":
		return e.Status() == status
	case "issues":
		return e.HasIssues()
	case "ai":
		return e.IsAI()
	case "glossary":
		return e.Origin == models.OriginGlossary
	case "memory":
		return e.Origin == models.OriginMemory
	default:
		return true
	}
}

func matchQuery(e *models.Entry, q string) bool {
	if strings.Contains(strings.ToLower(e.MsgID), q) ||
		strings.Contains(strings.ToLower(e.MsgIDPlural), q) ||
		strings.Contains(strings.ToLower(e.ContextValue()), q) ||
		strings.Contains(strings.ToLower(e.TranslatorComments), q) ||
		strings.Contains(strings.ToLower(e.References), q) {
		return true
	}
	for _, s := range e.MsgStr {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

func sortEntries(entries []*models.Entry, order string) {
	switch order {
	case "source":
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].MsgID) < strings.ToLower(entries[j].MsgID)
		})
	case "translation":
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].FirstTranslation()) < strings.ToLower(entries[j].FirstTranslation())
		})
	case "todo_first":
		rank := func(e *models.Entry) int {
			switch {
			case e.HasIssues():
				return 0
			case e.Status() == "untranslated":
				return 1
			case e.Status() == "fuzzy":
				return 2
			}
			return 3
		}
		sort.SliceStable(entries, func(i, j int) bool { return rank(entries[i]) < rank(entries[j]) })
	}
}

// Entry returns a validated entry of the catalog
func (s *CatalogService) Entry(ctx context.Context, c *models.Catalog, id int64) (*models.Entry, error) {
	e, err := s.catalogs.GetEntry(ctx, id)
	if err != nil || e.CatalogID != c.ID || e.Obsolete {
		return nil, ErrNotFound
	}
	e.Issues = po.Validate(e.ToPO(), c.NPlurals)
	return e, nil
}

// EntryInput holds the editable fields of an entry
type EntryInput struct {
	Version     int
	Translation []string
	Fuzzy       bool
	Comment     string
}

// SaveEntry stores a translation. It fails with ErrEntryConflict when someone
// else saved the entry since it was loaded.
func (s *CatalogService) SaveEntry(ctx context.Context, user *models.User, c *models.Catalog, id int64, in EntryInput) (*models.Entry, error) {
	if c.IsTemplate || user == nil || !c.WritableBy(user.ID) {
		return nil, ErrForbidden
	}
	e, err := s.Entry(ctx, c, id)
	if err != nil {
		return nil, err
	}
	forms := fitForms(in.Translation, e.IsPlural(), c.NPlurals)
	for i := range forms {
		// browsers submit CRLF line endings
		forms[i] = strings.ReplaceAll(forms[i], "\r\n", "\n")
	}
	e.MsgStr = forms
	e.Fuzzy = in.Fuzzy
	e.TranslatorComments = strings.TrimRight(strings.ReplaceAll(in.Comment, "\r\n", "\n"), "\n")
	e.Version = in.Version
	e.Origin = "" // saved by a person: no longer an unreviewed machine translation
	e.ReplacedMsgStr = nil
	if err := s.catalogs.UpdateEntry(ctx, e, user.ID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrEntryConflict
		}
		return nil, err
	}
	return s.Entry(ctx, c, id)
}

// Suggestions returns translation memory matches for an entry
func (s *CatalogService) Suggestions(ctx context.Context, user *models.User, c *models.Catalog, e *models.Entry) ([]repository.Suggestion, error) {
	if c.IsTemplate || e.IsPlural() {
		return nil, nil
	}
	return s.catalogs.Suggestions(ctx, user.ID, c.Language, e.MsgID, e.ID, 5)
}

// PreTranslate fills untranslated entries from the translation memory
func (s *CatalogService) PreTranslate(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, markFuzzy bool) (int, error) {
	if c.IsTemplate || user == nil || !c.WritableBy(user.ID) {
		return 0, ErrForbidden
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgBeforeMemory)
	return s.catalogs.PreTranslate(ctx, c.ID, user.ID, c.Language, markFuzzy)
}

// Build assembles the catalog as a file
func (s *CatalogService) Build(ctx context.Context, user *models.User, c *models.Catalog) (*po.Catalog, error) {
	entries, err := s.catalogs.ListEntries(ctx, c.ID, true)
	if err != nil {
		return nil, err
	}
	pc := &po.Catalog{Header: c.Header}
	if c.HeaderComments != "" {
		pc.HeaderComments = strings.Split(c.HeaderComments, "\n")
	}
	if !c.IsTemplate && (user == nil || user.ID != c.OwnerID) {
		// someone else's file is exported as stored, without their identity
		pc.Header = po.SetHeaderField(pc.Header, "Language", c.Language)
		pc.Header = po.SetHeaderField(pc.Header, "Plural-Forms", c.PluralForms)
	} else if !c.IsTemplate {
		// sync the translator identity: Last-Translator, Language-Team and the
		// contributor line in the header comments
		now := time.Now()
		pc.Header = po.SetHeaderField(pc.Header, "PO-Revision-Date", now.Format("2006-01-02 15:04-0700"))
		pc.Header = po.SetHeaderField(pc.Header, "Last-Translator", user.TranslatorIdentity())
		// the personal team only fills a missing or placeholder Language-Team;
		// a team set in the file properties wins
		if team := po.GetHeaderField(pc.Header, "Language-Team"); user.LanguageTeam != "" &&
			(team == "" || team == po.LanguageName(c.Language) || strings.HasPrefix(team, "LANGUAGE <")) {
			pc.Header = po.SetHeaderField(pc.Header, "Language-Team", user.LanguageTeam)
		}
		pc.HeaderComments = po.AddContributor(pc.HeaderComments, user.CreditName(), user.CreditEmail(), now.Year())
		pc.Header = po.SetHeaderField(pc.Header, "Language", c.Language)
		pc.Header = po.SetHeaderField(pc.Header, "Plural-Forms", c.PluralForms)
		pc.Header = po.SetHeaderField(pc.Header, "X-Generator", s.site.Name)
	}
	for _, e := range entries {
		pc.Entries = append(pc.Entries, e.ToPO())
	}
	return pc, nil
}

// SaveRaw replaces the catalog with edited PO text. updatedAt must be the
// catalog's UpdatedAt when the text was loaded; a newer change is a conflict.
func (s *CatalogService) SaveRaw(ctx context.Context, user *models.User, c *models.Catalog, text string, updatedAt int64) error {
	if !c.WritableBy(user.ID) {
		return ErrForbidden
	}
	if c.UpdatedAt.UnixNano() != updatedAt {
		return userErr("err.raw_conflict")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	s.SafetyCommit(ctx, user.ID, c, CommitMsgBeforeRaw)
	return s.replaceContent(ctx, user, c, []byte(text))
}

// CatalogProperties are the editable header values of a catalog
type CatalogProperties struct {
	ProjectVersion string
	ReportBugsTo   string
	LanguageTeam   string
	Contributors   string // header comment lines (copyright, translators)
}

// Properties returns the editable header values
func (s *CatalogService) Properties(c *models.Catalog) CatalogProperties {
	return CatalogProperties{
		ProjectVersion: po.GetHeaderField(c.Header, "Project-Id-Version"),
		ReportBugsTo:   po.GetHeaderField(c.Header, "Report-Msgid-Bugs-To"),
		LanguageTeam:   po.GetHeaderField(c.Header, "Language-Team"),
		Contributors:   c.HeaderComments,
	}
}

// UpdateProperties saves header values and the contributor comments
func (s *CatalogService) UpdateProperties(ctx context.Context, user *models.User, c *models.Catalog, in CatalogProperties) error {
	if !c.WritableBy(user.ID) {
		return ErrForbidden
	}
	clean := func(v string) string { return strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(v)) }
	if v := clean(in.ProjectVersion); v != "" || po.GetHeaderField(c.Header, "Project-Id-Version") != "" {
		c.Header = po.SetHeaderField(c.Header, "Project-Id-Version", v)
	}
	if v := clean(in.ReportBugsTo); v != "" || po.GetHeaderField(c.Header, "Report-Msgid-Bugs-To") != "" {
		c.Header = po.SetHeaderField(c.Header, "Report-Msgid-Bugs-To", v)
	}
	if v := clean(in.LanguageTeam); v != "" || po.GetHeaderField(c.Header, "Language-Team") != "" {
		c.Header = po.SetHeaderField(c.Header, "Language-Team", v)
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(in.Contributors, "\r\n", "\n"), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(strings.TrimPrefix(l, "#"), " ")
	}
	c.HeaderComments = strings.Join(lines, "\n")
	if strings.TrimSpace(c.HeaderComments) == "" {
		c.HeaderComments = ""
	}
	return s.catalogs.UpdateHeader(ctx, c)
}

// MOFilename returns the compiled file name for a catalog
func MOFilename(c *models.Catalog) string {
	return strings.TrimSuffix(c.Filename, filepath.Ext(c.Filename)) + ".mo"
}

// uniqueFilename returns name, or a variant with the language (and a number)
// added when the owner already has a file of that name in the project.
func (s *CatalogService) uniqueFilename(ctx context.Context, c *models.Catalog, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	suffix := c.Language
	if suffix == "" {
		suffix = "template"
	}
	candidates := []string{name}
	// add the language unless the name already ends with it ("x.id.po" -> "x.id-2.po")
	if !strings.HasSuffix(base, "."+suffix) && !strings.HasSuffix(base, "-"+suffix) {
		base += "-" + suffix
		candidates = append(candidates, base+ext)
	}
	for i := 2; i < 100; i++ {
		candidates = append(candidates, base+"-"+strconv.Itoa(i)+ext)
	}
	for _, cand := range candidates {
		taken, err := s.catalogs.FilenameTaken(ctx, c.ProjectID, c.OwnerID, cand, c.ID)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	return "", ErrFilenameTaken
}

// GetByPath returns one of the user's files by its URL parts
func (s *CatalogService) GetByPath(ctx context.Context, user *models.User, projectID int64, username, filename string) (*models.Catalog, *models.Project, error) {
	c, err := s.catalogs.GetByPath(ctx, projectID, username, filename)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	return s.Get(ctx, user, c.ID)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// moduleKey names the module a PO file belongs to: the part before the first dot,
// so "tecla.po-ui.gnome-51.id.po" and "tecla.gnome-51.id.po" are the same module.
func moduleKey(filename string) string {
	name := strings.ToLower(path.Base(filename))
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return name
}

// MatchOwnFiles sets ProjectFile.Mine to the user's own translation of the same
// module and language (same file name first, else the most recently updated).
func (s *CatalogService) MatchOwnFiles(ctx context.Context, user *models.User, p *models.Project, files []*models.ProjectFile) error {
	if user == nil || len(files) == 0 {
		return nil
	}
	list, err := s.catalogs.ListAllByProject(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, f := range files {
		var best *models.Catalog
		for _, c := range list {
			if c.OwnerID != user.ID || c.IsTemplate || c.Language != f.Language {
				continue
			}
			if c.Filename == f.Filename {
				best = c
				break
			}
			if moduleKey(c.Filename) == moduleKey(f.Filename) && (best == nil || c.UpdatedAt.After(best.UpdatedAt)) {
				best = c
			}
		}
		f.Mine = best
	}
	return nil
}

// UpdateFromUpstream replaces the user's file with a newer project file. History
// continues: the current state is committed first, then the upstream version.
func (s *CatalogService) UpdateFromUpstream(ctx context.Context, user *models.User, c *models.Catalog, label string, data []byte) error {
	if !c.WritableBy(user.ID) || c.IsTemplate {
		return ErrForbidden
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgBeforeReplace)
	if err := s.replaceContent(ctx, user, c, data); err != nil {
		return err
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgFromUpstream+"|"+label)
	return nil
}
