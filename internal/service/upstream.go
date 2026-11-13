package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/gnomel10n"
	"pelokalan/internal/models"
)

// Upstream states (GNOME translation site). The site's API allows roughly a hundred
// anonymous requests per hour, so pages never ask it: states are stored in
// upstream_states and filled a few at a time by WarmUpstream (every sync round,
// within the client's hourly budget). Pages, filters and "Ambil" read the table.

const (
	// UpstreamFresh is how old a stored state may get before WarmUpstream checks it again.
	// With the request budget about 30 files are checked per hour, so a large project's
	// states are refreshed less often than this.
	UpstreamFresh = 2 * time.Hour
	// upstreamReserve requests per hour are left for people taking a file whose state
	// is not stored yet
	upstreamReserve = 8
	// upstreamWait bounds the release list request a page may need (cached for 12 hours)
	upstreamWait = 3 * time.Second
)

type upstreamKey struct{ release, lang, stem string }

// linkedRelease is the release of states fetched for a file's own page (Catalog.UpstreamPage):
// they belong to no release, so they are stored apart from the release pages' rows
const linkedRelease = ""

// upstreamDue is a file state to refresh
type upstreamDue struct {
	k       upstreamKey
	checked time.Time        // zero = never
	page    *gnomel10n.Entry // set for a file's own page: asked directly, not through a release
}

// UpstreamPageURL checks a file's page on the GNOME translation site as entered by a person and
// returns it in its canonical form ("" stays ""); anything else is refused with err.upstream_page
func (s *GlossaryService) UpstreamPageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if s.upstream == nil {
		return "", userErr("err.upstream_page")
	}
	e, lang, ok := s.upstream.ParsePageURL(raw)
	if !ok {
		return "", userErr("err.upstream_page")
	}
	return s.upstream.PageURL(&gnomel10n.Stats{Module: e.Module, Branch: e.Branch, Domain: e.Domain, Language: lang}), nil
}

// pageKey is the stored state's key of a file's own page
func (s *GlossaryService) pageKey(page string) (upstreamKey, gnomel10n.Entry, bool) {
	if s.upstream == nil || page == "" {
		return upstreamKey{}, gnomel10n.Entry{}, false
	}
	e, lang, ok := s.upstream.ParsePageURL(page)
	if !ok {
		return upstreamKey{}, gnomel10n.Entry{}, false
	}
	return upstreamKey{linkedRelease, lang, e.Stem()}, e, true
}

// CheckUpstreamPage fetches and stores the state of a file's own page now (one request), so the
// badge shows right after the page was entered. Errors are only logged: the background round retries.
func (s *GlossaryService) CheckUpstreamPage(ctx context.Context, page string) {
	k, e, ok := s.pageKey(page)
	if !ok || s.upstream.Remaining() <= 0 {
		return
	}
	if _, err := s.checkPage(ctx, k, e, time.Minute); err != nil {
		log.Printf("upstream: check page %s: %v", page, err)
	}
}

// checkPage asks the site for the state of one entry given by its page and stores it
// (not found = stored as not listed); the client's cached answer is used when younger than
// maxAge. An error means the site could not be asked.
func (s *GlossaryService) checkPage(ctx context.Context, k upstreamKey, e gnomel10n.Entry, maxAge time.Duration) (*models.UpstreamRecord, error) {
	rec := &models.UpstreamRecord{Release: k.release, Language: k.lang, Stem: k.stem, CheckedAt: time.Now()}
	st, err := s.upstream.StatsMaxAge(ctx, e, maxAge)
	switch {
	case errors.Is(err, gnomel10n.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		rec.Listed = true
		rec.Module, rec.Branch, rec.Domain, rec.State = st.Module, st.Branch, st.Domain, st.State
		rec.Translated, rec.Fuzzy, rec.Untranslated, rec.POFile = st.Translated, st.Fuzzy, st.Untranslated, st.POFile
	}
	if err := s.repo.SaveUpstreamState(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// upstreamLang is the language looked up upstream: the project's, else the file's
func upstreamLang(projectLang, fileLang string) string {
	if projectLang != "" {
		return projectLang
	}
	return fileLang
}

// releaseOf is the site's release of a module group: from its sync link on the site
// (…/languages/id/gnome-51/ui.tar.gz), else its label when the site lists a release of
// that name (known is asked lazily, it may cost a request every 12 hours)
func (s *GlossaryService) releaseOf(glossaryURL, label string, known func() map[string]bool) string {
	if r := s.upstream.ReleaseFromURL(glossaryURL); r != "" {
		return r
	}
	if known()[label] {
		return label
	}
	return ""
}

func (s *GlossaryService) knownReleases(ctx context.Context) func() map[string]bool {
	var known map[string]bool
	return func() map[string]bool {
		if known == nil {
			var err error
			if known, err = s.upstream.Releases(ctx); err != nil {
				known = map[string]bool{}
			}
		}
		return known
	}
}

// upstreamLookup reads stored states for one project: groups → releases, and the
// stored rows per release and language (loaded once each)
type upstreamLookup struct {
	s       *GlossaryService
	ctx     context.Context
	p       *models.Project
	groups  []*models.ModuleGroup // newest first
	release map[int64]string
	rows    map[[2]string]map[string]*models.UpstreamRecord
}

func (s *GlossaryService) newUpstreamLookup(ctx context.Context, p *models.Project) *upstreamLookup {
	if s.upstream == nil {
		return nil
	}
	groups, err := s.repo.ListGroups(ctx, p.ID)
	if err != nil {
		return nil
	}
	SortGroups(groups)
	wait, cancel := context.WithTimeout(ctx, upstreamWait)
	defer cancel()
	known := s.knownReleases(wait)
	l := &upstreamLookup{s: s, ctx: ctx, p: p, groups: groups, release: map[int64]string{},
		rows: map[[2]string]map[string]*models.UpstreamRecord{}}
	for _, v := range groups {
		if r := s.releaseOf(v.SyncURL, v.Label(), known); r != "" {
			l.release[v.ID] = r
		}
	}
	return l
}

// HasUpstream reports whether some module group of the project is a release on the GNOME
// translation site (the upstream state filter is shown then)
func (s *GlossaryService) HasUpstream(ctx context.Context, p *models.Project) bool {
	l := s.newUpstreamLookup(ctx, p)
	return l != nil && len(l.release) > 0
}

// key finds the upstream key of a project file name in a module group (ok false = no release upstream)
func (l *upstreamLookup) key(groupID int64, filename, fileLang string) (upstreamKey, bool) {
	r := l.release[groupID]
	lang := upstreamLang(l.p.Language, fileLang)
	stem := gnomel10n.FileStem(filename, lang)
	if r == "" || lang == "" || stem == "" {
		return upstreamKey{}, false
	}
	return upstreamKey{r, lang, stem}, true
}

func (l *upstreamLookup) record(k upstreamKey) *models.UpstreamRecord {
	rk := [2]string{k.release, k.lang}
	rows, ok := l.rows[rk]
	if !ok {
		var err error
		if rows, err = l.s.repo.UpstreamStates(l.ctx, k.release, k.lang); err != nil {
			log.Printf("upstream: load states of %s/%s: %v", k.release, k.lang, err)
		}
		l.rows[rk] = rows
	}
	return rows[k.stem]
}

// info turns a stored row into what pages show. Without a known state the file still gets a
// link to where the site would show it (UpstreamLink), so people can check for themselves.
func (s *GlossaryService) info(k upstreamKey, rec *models.UpstreamRecord) models.UpstreamInfo {
	switch {
	case rec == nil:
		return models.UpstreamInfo{UpstreamLink: s.guessPageURL(k)}
	case !rec.Listed:
		return models.UpstreamInfo{UpstreamChecked: true, UpstreamLink: s.guessPageURL(k)}
	}
	st := &gnomel10n.Stats{Module: rec.Module, Branch: rec.Branch, Domain: rec.Domain, Language: rec.Language}
	return models.UpstreamInfo{UpstreamChecked: true, Upstream: &models.UpstreamStats{State: rec.State,
		Translated: rec.Translated, Fuzzy: rec.Fuzzy, Untranslated: rec.Untranslated,
		PageURL: s.upstream.PageURL(st), POFileURL: s.upstream.FileURL(rec.POFile)}}
}

// AttachUpstream fills each project file's upstream state from the stored states (no
// request to the site, except the release list every 12 hours for groups named after a release)
func (s *GlossaryService) AttachUpstream(ctx context.Context, p *models.Project, files []*models.ProjectFile) {
	l := s.newUpstreamLookup(ctx, p)
	if l == nil {
		return
	}
	for _, f := range files {
		if k, ok := l.key(f.GroupID, f.Filename, f.Language); ok {
			f.UpstreamInfo = s.info(k, l.record(k))
		}
	}
}

// AttachUpstreamToCatalogs fills the upstream state of members' PO files: from the file's own page
// when one was entered (Catalog.UpstreamPage), else through the project file they come from: the
// same file name, else the same module (newest module group first)
func (s *GlossaryService) AttachUpstreamToCatalogs(ctx context.Context, p *models.Project, catalogs []*models.Catalog) {
	l := s.newUpstreamLookup(ctx, p)
	if l == nil {
		return
	}
	var names map[int64][]string
	loaded := false
	for _, c := range catalogs {
		if c.IsTemplate {
			continue
		}
		if k, _, ok := s.pageKey(c.UpstreamPage); ok {
			c.UpstreamInfo = s.info(k, l.record(k))
			if c.Upstream == nil {
				c.UpstreamLink = c.UpstreamPage // not checked yet, or the site does not know it
			}
			continue
		}
		if len(l.release) == 0 {
			continue
		}
		if !loaded {
			loaded = true
			var err error
			if names, err = s.repo.ProjectFileNames(ctx, p.ID); err != nil {
				return
			}
		}
		vid, name := l.projectFileFor(names, c.Filename)
		if name == "" {
			continue
		}
		if k, ok := l.key(vid, name, c.Language); ok {
			c.UpstreamInfo = s.info(k, l.record(k))
		}
	}
}

// projectFileFor finds the project file a member's file belongs to (module group id, file name; "" = none)
func (l *upstreamLookup) projectFileFor(names map[int64][]string, filename string) (int64, string) {
	for _, v := range l.groups {
		if l.release[v.ID] == "" {
			continue
		}
		for _, n := range names[v.ID] {
			if n == filename {
				return v.ID, n
			}
		}
	}
	mod := moduleKey(filename)
	for _, v := range l.groups {
		if l.release[v.ID] == "" {
			continue
		}
		list := append([]string(nil), names[v.ID]...)
		sort.Strings(list) // stable choice when a module has several files
		for _, n := range list {
			if moduleKey(n) == mod {
				return v.ID, n
			}
		}
	}
	return 0, ""
}

// UpstreamPO downloads the current PO file of a project file from the GNOME translation
// site (newer than the release archive). label names the source for the history.
// The file's address comes from the stored state; when none is stored yet it is looked up.
func (s *GlossaryService) UpstreamPO(ctx context.Context, p *models.Project, f *models.ProjectFile, maxBytes int64) ([]byte, string, error) {
	unavailable := userErr("err.upstream_unavailable")
	l := s.newUpstreamLookup(ctx, p)
	if l == nil {
		return nil, "", unavailable
	}
	k, ok := l.key(f.GroupID, f.Filename, f.Language)
	if !ok {
		return nil, "", unavailable
	}
	rec := l.record(k)
	if rec == nil || (rec.Listed && rec.POFile == "") { // never checked, or only read from the release page
		lookup, cancel := context.WithTimeout(ctx, 30*time.Second)
		rec, _ = s.checkUpstream(lookup, k, map[[2]string]map[string]gnomel10n.Entry{})
		cancel()
	}
	if rec == nil || !rec.Listed || rec.POFile == "" {
		return nil, "", unavailable
	}
	data, err := s.upstream.Download(ctx, rec.POFile, maxBytes)
	if err != nil {
		log.Printf("upstream: download %s: %v", rec.POFile, err)
		return nil, "", unavailable
	}
	return data, s.upstream.Host() + " " + rec.Module + "/" + rec.Branch, nil
}

// checkUpstream asks the site for one file's state and stores it. listings caches the
// release lists of this round. An error means the site could not be asked (budget,
// throttled, unreachable): nothing is stored.
func (s *GlossaryService) checkUpstream(ctx context.Context, k upstreamKey, listings map[[2]string]map[string]gnomel10n.Entry) (*models.UpstreamRecord, error) {
	rk := [2]string{k.release, k.lang}
	index, ok := listings[rk]
	if !ok {
		entries, err := s.upstream.Release(ctx, k.release, k.lang)
		if err != nil && !errors.Is(err, gnomel10n.ErrNotFound) {
			return nil, err
		}
		index = map[string]gnomel10n.Entry{}
		for _, e := range entries {
			index[e.Stem()] = e
		}
		listings[rk] = index
	}
	rec := &models.UpstreamRecord{Release: k.release, Language: k.lang, Stem: k.stem, CheckedAt: time.Now()}
	if e, ok := index[k.stem]; ok {
		st, err := s.upstream.Stats(ctx, e)
		switch {
		case errors.Is(err, gnomel10n.ErrNotFound):
		case err != nil:
			return nil, err
		default:
			rec.Listed = true
			rec.Module, rec.Branch, rec.Domain, rec.State = st.Module, st.Branch, st.Domain, st.State
			rec.Translated, rec.Fuzzy, rec.Untranslated, rec.POFile = st.Translated, st.Fuzzy, st.Untranslated, st.POFile
		}
	}
	if err := s.repo.SaveUpstreamState(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// WarmUpstream refreshes the upstream state of project files (all projects). States
// fresher than UpstreamFresh are left alone. For each release and language with something
// due it reads the site's release page ("ui", plus "doc" when a file is documentation):
// one request gives every module's state. When a page cannot be read, the files of that
// release fall back to one API request each: never checked first, then the oldest, until
// the hourly budget (minus a reserve for people) is used up. Runs at start-up and with
// every sync round; one round at a time.
func (s *GlossaryService) WarmUpstream(ctx context.Context) {
	s.warmUpstream(ctx, UpstreamFresh)
}

// RefreshUpstream checks every file state again now, however recently it was checked (the
// "Sinkronkan dengan upstream" button); with the release pages this is one request per release
// (it waits for a background round that is running, instead of skipping)
func (s *GlossaryService) RefreshUpstream(ctx context.Context) {
	if s.upstream == nil {
		return
	}
	s.warming.Lock()
	defer s.warming.Unlock()
	s.warmLocked(ctx, time.Minute)
}

func (s *GlossaryService) warmUpstream(ctx context.Context, fresh time.Duration) {
	if s.upstream == nil || !s.warming.TryLock() {
		return
	}
	defer s.warming.Unlock()
	s.warmLocked(ctx, fresh)
}

// warmLocked is one round of WarmUpstream / RefreshUpstream; the caller holds s.warming
func (s *GlossaryService) warmLocked(ctx context.Context, fresh time.Duration) {
	cands, err := s.repo.UpstreamCandidates(ctx)
	if err != nil {
		log.Printf("upstream: list project files: %v", err)
		return
	}
	known := s.knownReleases(ctx)
	keys := map[upstreamKey]bool{}
	for _, c := range cands {
		r := s.releaseOf(c.SyncURL, models.GroupLabelOf(c.GroupName, c.GroupVersion), known)
		lang := upstreamLang(c.ProjectLanguage, c.FileLanguage)
		stem := gnomel10n.FileStem(c.Filename, lang)
		if r != "" && lang != "" && stem != "" {
			keys[upstreamKey{r, lang, stem}] = true
		}
	}
	type due = upstreamDue
	byRelease := map[[2]string][]due{}
	stored := map[[2]string]map[string]*models.UpstreamRecord{}
	now := time.Now()
	for k := range keys {
		rk := [2]string{k.release, k.lang}
		rows, ok := stored[rk]
		if !ok {
			rows, _ = s.repo.UpstreamStates(ctx, k.release, k.lang)
			stored[rk] = rows
		}
		var checked time.Time
		if rec := rows[k.stem]; rec != nil {
			checked = rec.CheckedAt
		}
		if checked.IsZero() || now.Sub(checked) >= fresh {
			byRelease[rk] = append(byRelease[rk], due{k: k, checked: checked})
		}
	}

	// files with their own page: asked directly (there is no release page for them)
	var list []due // files checked one by one through the API
	if pages, err := s.repo.LinkedUpstreamPages(ctx); err != nil {
		log.Printf("upstream: list file pages: %v", err)
	} else {
		seen := map[upstreamKey]bool{}
		for _, pg := range pages {
			k, e, ok := s.pageKey(pg)
			if !ok || seen[k] {
				continue
			}
			seen[k] = true
			rk := [2]string{k.release, k.lang}
			rows, ok := stored[rk]
			if !ok {
				rows, _ = s.repo.UpstreamStates(ctx, k.release, k.lang)
				stored[rk] = rows
			}
			var checked time.Time
			if rec := rows[k.stem]; rec != nil {
				checked = rec.CheckedAt
			}
			if checked.IsZero() || now.Sub(checked) >= fresh {
				list = append(list, due{k: k, checked: checked, page: &e})
			}
		}
	}

	// one release page per release and language
	pageFailed := false
	for rk, files := range byRelease {
		n, fallback, err := s.warmFromPages(ctx, rk[0], rk[1], files, stored[rk])
		list = append(list, fallback...)
		if err != nil {
			if !pageFailed { // once per round
				log.Printf("upstream: release page %s/%s: %v (checking files one by one)", rk[0], rk[1], err)
			}
			pageFailed = true
			list = append(list, files...)
			continue
		}
		log.Printf("upstream: release page %s/%s: %d file states", rk[0], rk[1], n)
	}
	if len(list) == 0 {
		return
	}

	// fallback: the API, one file at a time
	sort.Slice(list, func(i, j int) bool {
		if !list[i].checked.Equal(list[j].checked) {
			return list[i].checked.Before(list[j].checked)
		}
		a, b := list[i].k, list[j].k
		return a.release+a.lang+a.stem < b.release+b.lang+b.stem
	})
	listings := map[[2]string]map[string]gnomel10n.Entry{}
	n := 0
	for _, d := range list {
		if ctx.Err() != nil || s.upstream.Remaining() <= upstreamReserve {
			break
		}
		var err error
		if d.page != nil {
			_, err = s.checkPage(ctx, d.k, *d.page, min(fresh, gnomel10n.StatsTTL))
		} else {
			_, err = s.checkUpstream(ctx, d.k, listings)
		}
		if err != nil {
			log.Printf("upstream: check %s/%s/%s: %v (stopping this round)", d.k.release, d.k.lang, d.k.stem, err)
			break
		}
		n++
	}
	log.Printf("upstream: checked %d of %d due file states through the API", n, len(list))
}

// warmFromPages reads the release pages of one release and language and stores every
// module they list. Due files the pages do not list are stored as not upstream. The doc
// page is read only when a due file is documentation ("…-help.<branch>"); when it cannot be
// read, those files are returned for the API fallback. An error means the ui page failed.
func (s *GlossaryService) warmFromPages(ctx context.Context, release, lang string, files []upstreamDue, stored map[string]*models.UpstreamRecord) (int, []upstreamDue, error) {
	isDoc := func(stem string) bool { return strings.Contains(stem, "-help.") }
	page := func(kind string) ([]gnomel10n.PageRow, error) {
		if s.upstream.Remaining() <= upstreamReserve {
			return nil, gnomel10n.ErrBudget
		}
		rows, err := s.upstream.ReleasePage(ctx, release, lang, kind)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", kind, err)
		}
		return rows, nil
	}
	rows, err := page("ui")
	if err != nil {
		return 0, nil, err
	}
	docRead := false
	for _, d := range files {
		if isDoc(d.k.stem) {
			if doc, err := page("doc"); err == nil {
				rows = append(rows, doc...)
				docRead = true
			} else {
				log.Printf("upstream: release page %s/%s: %v (documentation checked one by one)", release, lang, err)
			}
			break
		}
	}
	now := time.Now()
	seen := map[string]bool{}
	n := 0
	for _, r := range rows {
		stem := r.Entry().Stem()
		if seen[stem] {
			continue
		}
		seen[stem] = true
		rec := &models.UpstreamRecord{Release: release, Language: lang, Stem: stem, Listed: true,
			Module: r.Module, Branch: r.Branch, Domain: r.Domain, State: r.State, Translated: r.Translated,
			Fuzzy: r.Fuzzy, Untranslated: r.Untranslated, CheckedAt: now}
		if old := stored[stem]; old != nil {
			rec.POFile = old.POFile // the page has no file address; keep the one the API gave
		}
		if err := s.repo.SaveUpstreamState(ctx, rec); err != nil {
			return n, nil, err
		}
		n++
	}
	var fallback []upstreamDue
	for _, d := range files {
		switch {
		case seen[d.k.stem]:
		case isDoc(d.k.stem) && !docRead:
			fallback = append(fallback, d)
		default:
			rec := &models.UpstreamRecord{Release: release, Language: lang, Stem: d.k.stem, CheckedAt: now}
			if err := s.repo.SaveUpstreamState(ctx, rec); err != nil {
				return n, nil, err
			}
		}
	}
	return n, fallback, nil
}

// guessPageURL is the site's page for a file whose state is not known: the workflow page built
// from the file name stem ("gnome-calculator.gnome-51" → module gnome-calculator, branch gnome-51,
// domain po; "<module>-help.<branch>" → domain help)
func (s *GlossaryService) guessPageURL(k upstreamKey) string {
	i := strings.LastIndex(k.stem, ".")
	if s.upstream == nil || i <= 0 || i == len(k.stem)-1 {
		return ""
	}
	module, branch, domain := k.stem[:i], k.stem[i+1:], "po"
	if m, ok := strings.CutSuffix(module, "-help"); ok && m != "" {
		module, domain = m, "help"
	}
	return s.upstream.PageURL(&gnomel10n.Stats{Module: module, Branch: branch, Domain: domain, Language: k.lang})
}
