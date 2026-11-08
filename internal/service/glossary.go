package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// GlossaryService builds and searches project glossaries
type GlossaryService struct {
	repo     repository.GlossaryRepository
	fetcher  *glossary.Fetcher
	matchers matcherCache
}

// NewGlossaryService creates a new glossary service
func NewGlossaryService(repo repository.GlossaryRepository, fetcher *glossary.Fetcher) *GlossaryService {
	return &GlossaryService{repo: repo, fetcher: fetcher}
}

// GlossaryImportInput is an upload request
type GlossaryImportInput struct {
	Name     string // e.g. "gnome"
	Version  string // e.g. "51"
	Filename string
	Data     []byte
	Language string // optional override
}

// GlossaryImportResult summarizes an upload
type GlossaryImportResult struct {
	Version *models.ProjectVersion
	Imports []*models.GlossaryImport
	Skipped []glossary.FileError
}

var (
	versionNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	versionPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~]*$`)
)

// cleanVersion validates a release name and version such as "gnome" and "51"
func cleanVersion(name, version string) (string, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return "", "", userErr("err.version_required")
	}
	if len(name) > 40 || len(version) > 40 || !versionPartRe.MatchString(version) ||
		!versionNameRe.MatchString(name) {
		return "", "", userErr("err.version_invalid")
	}
	return name, version, nil
}

// Import reads an archive or PO file and adds its translations to the glossary
// under the given version. Files of different languages create one import per
// language; an earlier import of the same version and language is replaced.
func (s *GlossaryService) Import(ctx context.Context, user *models.User, p *models.Project, in GlossaryImportInput) (*GlossaryImportResult, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	name, version, err := cleanVersion(in.Name, in.Version)
	if err != nil {
		return nil, err
	}
	language := strings.TrimSpace(in.Language)
	if language != "" && !validLanguageCode(language) {
		return nil, ErrInvalidLanguage
	}
	v, err := s.repo.GetOrCreateVersion(ctx, p.ID, name, version)
	if err != nil {
		return nil, err
	}
	return s.importInto(ctx, v, in.Filename, in.Data, language, &user.ID)
}

func (s *GlossaryService) importInto(ctx context.Context, v *models.ProjectVersion, filename string, data []byte, language string, uploadedBy *int64) (*GlossaryImportResult, error) {
	files, err := glossary.ReadFiles(filename, data, glossary.Limits{})
	switch {
	case errors.Is(err, glossary.ErrUnsupported):
		return nil, userErr("err.glossary_unsupported")
	case errors.Is(err, glossary.ErrNoPOFiles):
		return nil, userErr("err.glossary_no_files")
	case errors.Is(err, glossary.ErrTooLarge):
		return nil, userErr("err.glossary_too_large")
	case err != nil:
		return nil, userErr("err.glossary_unreadable")
	}

	built := glossary.Build(files, language)
	if len(built.Terms) == 0 {
		return nil, userErr("err.glossary_empty")
	}

	byLang := map[string][]repository.GlossaryTerm{}
	entries := map[string]int{}
	for _, t := range built.Terms {
		byLang[t.Language] = append(byLang[t.Language], repository.GlossaryTerm{
			Source: t.Source, SourceNorm: t.SourceNorm,
			Translation: t.Translation, TranslationNorm: t.TranslationNorm,
			WordCount: t.WordCount, Occurrences: t.Occurrences,
			Modules: t.Modules, Contexts: t.Contexts,
		})
		entries[t.Language] += t.Occurrences
	}

	res := &GlossaryImportResult{Skipped: built.Skipped, Version: v}
	for _, lang := range built.Languages {
		terms := byLang[lang]
		imp := &models.GlossaryImport{
			ProjectID:  v.ProjectID,
			VersionID:  v.ID,
			Filename:   cleanFilename(filename, "glossary"),
			Language:   lang,
			FileCount:  countFiles(files, built, lang),
			EntryCount: entries[lang],
			TermCount:  len(terms),
			UploadedBy: uploadedBy,
		}
		if err := s.repo.CreateImport(ctx, imp, terms); err != nil {
			return nil, err
		}
		res.Imports = append(res.Imports, imp)
	}
	return res, nil
}

// FeedInput defines an automatic download source
type FeedInput struct {
	Name, Version, URL, Language string
}

// SetFeed stores a download URL for a version and fetches it right away. The
// URL is kept even when this first download fails; the error is returned.
func (s *GlossaryService) SetFeed(ctx context.Context, user *models.User, p *models.Project, in FeedInput) (*models.ProjectVersion, *FetchOutcome, error) {
	if !p.Role.CanManage() {
		return nil, nil, ErrForbidden
	}
	name, version, err := cleanVersion(in.Name, in.Version)
	if err != nil {
		return nil, nil, err
	}
	u, err := glossary.ValidateURL(in.URL)
	if err != nil {
		return nil, nil, userErr("err.feed_url_invalid")
	}
	language := strings.TrimSpace(in.Language)
	if language != "" && !validLanguageCode(language) {
		return nil, nil, ErrInvalidLanguage
	}
	v, err := s.repo.GetOrCreateVersion(ctx, p.ID, name, version)
	if err != nil {
		return nil, nil, err
	}
	if err := s.repo.SetFeed(ctx, v.ID, u.String(), language, &user.ID); err != nil {
		return nil, nil, err
	}
	if v, err = s.repo.GetVersion(ctx, v.ID); err != nil {
		return nil, nil, err
	}
	out := s.Fetch(ctx, v)
	return v, out, nil
}

func (s *GlossaryService) projectVersion(ctx context.Context, p *models.Project, versionID int64) (*models.ProjectVersion, error) {
	v, err := s.repo.GetVersion(ctx, versionID)
	if err != nil || v.ProjectID != p.ID {
		return nil, ErrNotFound
	}
	return v, nil
}

// DeleteVersion removes a version and everything imported for it
func (s *GlossaryService) DeleteVersion(ctx context.Context, p *models.Project, versionID int64) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	v, err := s.projectVersion(ctx, p, versionID)
	if err != nil {
		return err
	}
	return s.repo.DeleteVersion(ctx, v.ID)
}

// RemoveFeed stops the automatic download of a version (its glossary stays)
func (s *GlossaryService) RemoveFeed(ctx context.Context, p *models.Project, versionID int64) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	v, err := s.projectVersion(ctx, p, versionID)
	if err != nil {
		return err
	}
	return s.repo.SetFeed(ctx, v.ID, "", "", nil)
}

// FetchNow downloads a version's source immediately
func (s *GlossaryService) FetchNow(ctx context.Context, p *models.Project, versionID int64) (*FetchOutcome, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	v, err := s.projectVersion(ctx, p, versionID)
	if err != nil {
		return nil, err
	}
	if !v.HasFeed() {
		return nil, ErrNotFound
	}
	return s.Fetch(ctx, v), nil
}

// FetchOutcome describes one download
type FetchOutcome struct {
	Changed bool
	Result  *GlossaryImportResult
	Err     error
}

// Fetch downloads a version's source and re-imports it when the content changed.
// The outcome is recorded on the version.
func (s *GlossaryService) Fetch(ctx context.Context, v *models.ProjectVersion) *FetchOutcome {
	out := &FetchOutcome{}
	if s.fetcher == nil {
		out.Err = errors.New("fetcher not configured")
		return out
	}
	// a full download is needed when nothing was imported yet
	etag, lastMod := v.FetchETag, v.FetchLastModified
	if v.ImportCount == 0 {
		etag, lastMod = "", ""
	}
	res, err := s.fetcher.Fetch(ctx, v.GlossaryURL, etag, lastMod)
	switch {
	case errors.Is(err, glossary.ErrNotModified):
		v.LastFetchError = ""
	case err != nil:
		out.Err = err
		v.LastFetchError = fetchErrorText(err)
	default:
		sum := sha256.Sum256(res.Data)
		hash := hex.EncodeToString(sum[:])
		v.FetchETag, v.FetchLastModified = res.ETag, res.LastModified
		if hash == v.FetchContentHash && v.ImportCount > 0 {
			v.LastFetchError = ""
			break
		}
		imported, err := s.importInto(ctx, v, res.Filename, res.Data, v.GlossaryLanguage, v.FeedCreatedBy)
		if err != nil {
			out.Err = err
			v.LastFetchError = fetchErrorText(err)
			v.FetchETag, v.FetchLastModified = "", ""
			break
		}
		v.FetchContentHash = hash
		v.LastFetchError = ""
		out.Changed = true
		out.Result = imported
	}
	if err := s.repo.RecordFetch(ctx, v, out.Changed); err != nil {
		log.Printf("glossary: record fetch of version %d: %v", v.ID, err)
	}
	return out
}

func fetchErrorText(err error) string {
	if ue, ok := AsUserError(err); ok {
		return ue.Key
	}
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// FetchDue downloads every source not fetched within the interval
func (s *GlossaryService) FetchDue(ctx context.Context, interval time.Duration) {
	due, err := s.repo.DueFeeds(ctx, time.Now().Add(-interval))
	if err != nil {
		log.Printf("glossary: list due sources: %v", err)
		return
	}
	for _, v := range due {
		out := s.Fetch(ctx, v)
		switch {
		case out.Err != nil:
			log.Printf("glossary: fetch %s (version %d): %v", v.GlossaryURL, v.ID, out.Err)
		case out.Changed:
			log.Printf("glossary: updated version %d from %s", v.ID, v.GlossaryURL)
		}
	}
}

// countFiles reports how many files contributed to a language. With a single
// language every parsed file counts.
func countFiles(files []glossary.File, built *glossary.Result, lang string) int {
	if len(built.Languages) == 1 {
		return built.Files
	}
	n := 0
	for _, f := range files {
		if strings.HasSuffix(strings.TrimSuffix(f.Name, ".po"), "."+lang) {
			n++
		}
	}
	return n
}

// Term returns one glossary string by its hash
func (s *GlossaryService) Term(ctx context.Context, p *models.Project, hash string) (*models.GlossaryTerm, error) {
	t, err := s.repo.Term(ctx, p.ID, strings.ToLower(hash))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return t, err
}

// Imports lists the uploads of a project
func (s *GlossaryService) Imports(ctx context.Context, p *models.Project) ([]*models.GlossaryImport, error) {
	return s.repo.ListImports(ctx, p.ID)
}

// DeleteImport removes an upload and its terms
func (s *GlossaryService) DeleteImport(ctx context.Context, p *models.Project, id int64) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	imp, err := s.repo.GetImport(ctx, id)
	if err != nil || imp.ProjectID != p.ID {
		return ErrNotFound
	}
	return s.repo.DeleteImport(ctx, id)
}

// Versions lists the project versions, newest release first ("gnome-51"
// before "gnome-50", "gnome-100" before "gnome-99")
func (s *GlossaryService) Versions(ctx context.Context, p *models.Project) ([]*models.ProjectVersion, error) {
	versions, err := s.repo.ListVersions(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	SortVersions(versions)
	return versions, nil
}

// SortVersions orders versions by name, then by version number descending
func SortVersions(versions []*models.ProjectVersion) {
	sort.SliceStable(versions, func(i, j int) bool {
		a, b := versions[i], versions[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return naturalLess(b.Version, a.Version)
	})
}

// naturalLess compares strings treating digit runs as numbers ("9" < "10")
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ca, cb := a[0], b[0]
		if isDigit(ca) && isDigit(cb) {
			na, ra := digitRun(a)
			nb, rb := digitRun(b)
			na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRun(s string) (string, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

// Languages lists the glossary languages of a project
func (s *GlossaryService) Languages(ctx context.Context, p *models.Project) ([]string, error) {
	return s.repo.Languages(ctx, p.ID)
}

// GlossarySearch is a search request from the UI
type GlossarySearch struct {
	Language     string
	VersionID    int64 // 0 = all versions
	Query        string
	TermsOnly    bool
	Inconsistent bool
	Module       string // only terms used in this module (PO file)
	Order        string
	Dir          string
	Offset       int
}

// Modules lists the modules (PO files) of a language's glossary, for the filter
func (s *GlossaryService) Modules(ctx context.Context, p *models.Project, language string, versionID int64) ([]repository.ModuleCount, error) {
	return s.repo.Modules(ctx, p.ID, language, versionID)
}

// GlossaryPage is one page of search results
type GlossaryPage struct {
	Groups     []*models.GlossaryGroup
	Total      int
	Offset     int
	NextOffset int
	HasMore    bool
}

// GlossaryPageSize is the number of terms per page
const GlossaryPageSize = 50

// Search finds glossary terms
func (s *GlossaryService) Search(ctx context.Context, p *models.Project, in GlossarySearch) (*GlossaryPage, error) {
	if in.Offset < 0 {
		in.Offset = 0
	}
	q := repository.GlossaryQuery{
		ProjectID:    p.ID,
		Language:     in.Language,
		VersionID:    in.VersionID,
		Query:        glossary.Normalize(glossary.Clean(in.Query)),
		Inconsistent: in.Inconsistent,
		Module:       strings.TrimSpace(in.Module),
		Order:        in.Order,
		Dir:          in.Dir,
		Limit:        GlossaryPageSize,
		Offset:       in.Offset,
	}
	if in.TermsOnly {
		q.MaxWords = glossary.TermMaxWords
	}
	groups, total, err := s.repo.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	next := in.Offset + len(groups)
	return &GlossaryPage{Groups: groups, Total: total, Offset: in.Offset, NextOffset: next, HasMore: next < total}, nil
}
