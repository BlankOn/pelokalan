package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pelokalan/internal/glossary"
	"pelokalan/internal/gnomel10n"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/repository"
)

// GlossaryService builds and searches project glossaries
type GlossaryService struct {
	repo     repository.GlossaryRepository
	fetcher  *glossary.Fetcher
	matchers matcherCache
	syncing  sync.Mutex                   // one download round at a time (hourly sync or the Sync button)
	projects repository.ProjectRepository // for the project's language (imports in another one are refused)
	upstream *gnomel10n.Client            // state of project files on the GNOME translation site (nil = off)
	warming  sync.Mutex                   // one upstream state round at a time (start-up and sync rounds)

	stateMu   sync.Mutex
	syncState map[int64]*SyncStatus // per project: the latest Sync button run
}

// SetProjects lets imports and syncs check the project's language
func (s *GlossaryService) SetProjects(r repository.ProjectRepository) { s.projects = r }

// SetUpstream shows the state of project files on the GNOME translation site
func (s *GlossaryService) SetUpstream(c *gnomel10n.Client) { s.upstream = c }

// SyncStatus is the progress of a project's Sync button run
type SyncStatus struct {
	Running  bool     `json:"running"`
	Versions int      `json:"versions"` // versions with a sync link
	Done     int      `json:"done"`
	Changed  []string `json:"changed"` // labels re-imported
	Failed   []string `json:"failed"`  // "label: error"
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
	if err := checkProjectLanguage(p, language); err != nil {
		return nil, err
	}
	// check the files' languages before a version is created for them
	if p.Language != "" {
		if files, err := glossary.ReadFiles(in.Filename, in.Data, glossary.Limits{}); err == nil {
			for _, lang := range glossary.Build(files, language).Languages {
				if err := checkProjectLanguage(p, lang); err != nil {
					return nil, err
				}
			}
		}
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
	// every language in the upload or download must be the project's (manual imports and syncs)
	if s.projects != nil {
		if p, err := s.projects.GetByID(ctx, v.ProjectID); err == nil {
			for _, lang := range built.Languages {
				if err := checkProjectLanguage(p, lang); err != nil {
					return nil, err
				}
			}
		}
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
		// keep the source's PO files of this language as project files
		defer func() { // a point for the progress chart right after an upload
			if err := s.repo.RecordGroupStats(ctx, v.ID, 0); err != nil {
				log.Printf("glossary: record stats of version %d: %v", v.ID, err)
			}
		}()
		if err := s.repo.AddProjectFiles(ctx, projectFiles(files, imp, language)); err != nil {
			log.Printf("glossary: store project files of import %d: %v", imp.ID, err)
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
	if err := checkProjectLanguage(p, language); err != nil {
		return nil, nil, err
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

// SyncAll downloads every version with a sync link again in the background and
// re-imports the ones that changed (glossary and project files). Managers only.
func (s *GlossaryService) SyncAll(ctx context.Context, p *models.Project) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	versions, err := s.repo.ListVersions(ctx, p.ID)
	if err != nil {
		return err
	}
	var feeds []*models.ProjectVersion
	for _, v := range versions {
		if v.HasFeed() {
			feeds = append(feeds, v)
		}
	}
	if len(feeds) == 0 && s.upstream == nil {
		return ErrNoSyncLink
	}
	if !s.syncing.TryLock() {
		return ErrSyncRunning
	}
	st := &SyncStatus{Running: true, Versions: len(feeds)}
	s.setSync(p.ID, st)
	go func() {
		defer s.syncing.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		for _, v := range feeds {
			out := s.Fetch(ctx, v)
			s.stateMu.Lock()
			st.Done++
			switch {
			case out.Err != nil:
				log.Printf("glossary: sync %s (version %d): %v", v.GlossaryURL, v.ID, out.Err)
				st.Failed = append(st.Failed, v.Label()+": "+fetchErrorText(out.Err))
			case out.Changed:
				log.Printf("glossary: synced version %d from %s", v.ID, v.GlossaryURL)
				st.Changed = append(st.Changed, v.Label())
			}
			s.stateMu.Unlock()
		}
		// and the files' states on the translation site (one page per release)
		s.RefreshUpstream(ctx)
		s.stateMu.Lock()
		st.Running = false
		s.stateMu.Unlock()
	}()
	return nil
}

func (s *GlossaryService) setSync(projectID int64, st *SyncStatus) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.syncState == nil {
		s.syncState = map[int64]*SyncStatus{}
	}
	s.syncState[projectID] = st
}

// SyncState returns a copy of the project's latest Sync button run (nil = none)
func (s *GlossaryService) SyncState(p *models.Project) *SyncStatus {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	st := s.syncState[p.ID]
	if st == nil {
		return nil
	}
	cp := *st
	cp.Changed = append([]string(nil), st.Changed...)
	cp.Failed = append([]string(nil), st.Failed...)
	return &cp
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
	// a full download is needed when nothing was imported yet, or when an import from before
	// project files existed has none stored
	etag, lastMod := v.FetchETag, v.FetchLastModified
	needFiles := false
	if v.ImportCount > 0 {
		if n, err := s.repo.CountVersionFiles(ctx, v.ID); err == nil && n == 0 {
			needFiles = true
		}
	}
	if v.ImportCount == 0 || needFiles {
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
		if hash == v.FetchContentHash && v.ImportCount > 0 && !needFiles {
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
		// "key|arg|arg" keeps the message's details (shown with errText)
		parts := []string{ue.Key}
		for _, a := range ue.Args {
			parts = append(parts, fmt.Sprint(a))
		}
		return strings.Join(parts, "|")
	}
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// FetchDue downloads every source not fetched within the interval
func (s *GlossaryService) FetchDue(ctx context.Context, interval time.Duration) {
	s.syncing.Lock()
	defer s.syncing.Unlock()
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
	// upstream states of project files, a few at a time (the site's API is rate limited)
	s.WarmUpstream(ctx)
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

// projectFiles prepares the PO files of one language from a glossary source for storage:
// statistics from the parsed file, contents gzip-compressed
func projectFiles(files []glossary.File, imp *models.GlossaryImport, language string) []*models.ProjectFile {
	var out []*models.ProjectFile
	for _, f := range files {
		cat, err := po.ParseBytes(f.Data)
		if err != nil {
			continue
		}
		lang := language
		if lang == "" {
			lang = glossary.DetectLanguage(cat, f.Name)
		}
		if lang != imp.Language {
			continue
		}
		pf := &models.ProjectFile{ImportID: imp.ID, ProjectID: imp.ProjectID, VersionID: imp.VersionID,
			Filename: f.Name, Language: lang, Size: int64(len(f.Data))}
		for _, e := range cat.Entries {
			if e.Obsolete {
				continue
			}
			pf.Total++
			switch {
			case e.Fuzzy:
				pf.Fuzzy++
			case e.IsTranslated():
				pf.Translated++
			}
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(f.Data)
		_ = zw.Close()
		pf.Data = buf.Bytes()
		out = append(out, pf)
	}
	return out
}

// ProjectFilePageSize is how many project files are listed at a time
const ProjectFilePageSize = 10

// ProjectFiles lists the project's source PO files, least complete first, with their
// upstream state. states (UpstreamFilters values, nil = all) keeps only files in those
// upstream states; that filter needs every matching file, so it pages here.
func (s *GlossaryService) ProjectFiles(ctx context.Context, p *models.Project, query string, versionIDs []int64, states map[string]bool, offset int) ([]*models.ProjectFile, int, error) {
	if offset < 0 {
		offset = 0
	}
	if len(query) > 200 {
		query = query[:200]
	}
	query = strings.TrimSpace(query)
	if len(states) == 0 {
		files, total, err := s.repo.ListProjectFiles(ctx, p.ID, query, versionIDs, ProjectFilePageSize, offset)
		if err == nil {
			s.AttachUpstream(ctx, p, files)
		}
		return files, total, err
	}
	all, _, err := s.repo.ListProjectFiles(ctx, p.ID, query, versionIDs, 1<<30, 0)
	if err != nil {
		return nil, 0, err
	}
	s.AttachUpstream(ctx, p, all)
	var kept []*models.ProjectFile
	for _, f := range all {
		if states[f.UpstreamClass()] {
			kept = append(kept, f)
		}
	}
	if offset > len(kept) {
		offset = len(kept)
	}
	return kept[offset:min(offset+ProjectFilePageSize, len(kept))], len(kept), nil
}

// ProjectFileVersions lists the versions that have project files, newest first
func (s *GlossaryService) ProjectFileVersions(ctx context.Context, p *models.Project) ([]*models.ProjectFileVersion, error) {
	list, err := s.repo.ProjectFileVersions(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return naturalLess(b.Version, a.Version)
	})
	return list, nil
}

// ProjectFile returns one source PO file with its uncompressed contents
func (s *GlossaryService) ProjectFile(ctx context.Context, p *models.Project, id int64) (*models.ProjectFile, []byte, error) {
	f, err := s.repo.GetProjectFile(ctx, p.ID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(f.Data))
	if err != nil {
		return nil, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(zr, 50<<20))
	if err != nil {
		return nil, nil, err
	}
	return f, data, nil
}

// UpstreamFor returns the project file a translation can be updated from: same
// language, the same file name, else the same module from the newest version (nil = none).
func (s *GlossaryService) UpstreamFor(ctx context.Context, p *models.Project, c *models.Catalog) (*models.ProjectFile, error) {
	if c.IsTemplate {
		return nil, nil
	}
	files, err := s.repo.ProjectFilesForLanguage(ctx, p.ID, c.Language)
	if err != nil || len(files) == 0 {
		return nil, err
	}
	versions, err := s.repo.ListVersions(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	SortVersions(versions)
	rank := make(map[int64]int, len(versions))
	for i, v := range versions {
		rank[v.ID] = i
	}
	var best *models.ProjectFile
	better := func(f *models.ProjectFile) bool {
		return best == nil || rank[f.VersionID] < rank[best.VersionID]
	}
	for _, f := range files {
		if f.Filename == c.Filename {
			return f, nil
		}
		if moduleKey(f.Filename) == moduleKey(c.Filename) && better(f) {
			best = f
		}
	}
	return best, nil
}

// ModuleGroups maps each module (file name before the first dot) to the module groups
// (glossary versions with project files, e.g. "gnome-51") whose project files include it
func (s *GlossaryService) ModuleGroups(ctx context.Context, p *models.Project) (map[string][]int64, error) {
	names, err := s.repo.ProjectFileNames(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := map[string][]int64{}
	for vid, files := range names {
		seen := map[string]bool{}
		for _, f := range files {
			if m := moduleKey(f); !seen[m] {
				seen[m] = true
				out[m] = append(out[m], vid)
			}
		}
	}
	return out, nil
}

// StatsInterval is how often each module group gets a point on the progress chart
// (a changed total is recorded at the next check, at most 15 minutes later)
const StatsInterval = 55 * time.Minute

// RecordStats stores the current totals of every module group for the progress chart
func (s *GlossaryService) RecordStats(ctx context.Context) {
	if err := s.repo.RecordGroupStats(ctx, 0, StatsInterval); err != nil {
		log.Printf("glossary: record group stats: %v", err)
	}
}

// StatsRange is how far back the progress chart looks
type StatsRange struct {
	Key  string
	Days int // 0 = everything
}

// StatsRanges are the choices above the chart
var StatsRanges = []StatsRange{{"1", 1}, {"7", 7}, {"30", 30}, {"all", 0}}

// GroupStats returns a module group's recorded totals for a language within the last days
// (0 = all), oldest first
func (s *GlossaryService) GroupStats(ctx context.Context, p *models.Project, versionID int64, language string, days int) ([]models.GroupStat, error) {
	if _, err := s.projectVersion(ctx, p, versionID); err != nil {
		return nil, err
	}
	since := time.Time{}
	if days > 0 {
		since = time.Now().AddDate(0, 0, -days)
	}
	return s.repo.GroupStats(ctx, versionID, language, since)
}
