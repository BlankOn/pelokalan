package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/repository"
)

// Messages of automatic commits are i18n keys, optionally followed by
// "|argument" (rendered by the history page).
const (
	CommitMsgInitialUpload     = "commit.initial_upload"
	CommitMsgInitialTemplate   = "commit.initial_template"
	CommitMsgBeforeAI          = "commit.before_ai"
	CommitMsgAfterAI           = "commit.after_ai"
	CommitMsgAfterPretranslate = "commit.after_pretranslate"
	CommitMsgBeforeRaw         = "commit.before_raw"
	CommitMsgBeforeReplace     = "commit.before_replace"
	CommitMsgUploaded          = "commit.uploaded"
	CommitMsgFromUpstream      = "commit.from_upstream"
	CommitMsgBeforeMerge       = "commit.before_merge"
	CommitMsgBeforeMemory      = "commit.before_memory"
	CommitMsgBeforeRestore     = "commit.before_restore"
	CommitMsgRestore           = "commit.restore"
	CommitMsgBaseline          = "commit.baseline"
	CommitMsgDiscard           = "commit.discard"
)

// snapshot serializes the catalog state (header and all entries) to PO text.
// Unlike Build it adds nothing time-dependent, so equal content hashes equal.
func (s *CatalogService) snapshot(ctx context.Context, c *models.Catalog) ([]byte, *models.Commit, error) {
	entries, err := s.catalogs.ListEntries(ctx, c.ID, true)
	if err != nil {
		return nil, nil, err
	}
	pc := &po.Catalog{Header: c.Header}
	if c.HeaderComments != "" {
		pc.HeaderComments = strings.Split(c.HeaderComments, "\n")
	}
	stats := &models.Commit{}
	for _, e := range entries {
		pe := e.ToPO()
		if e.Origin != "" {
			// kept in snapshots so a restore brings back machine-translation markers
			pe.Flags = append(pe.Flags, originFlagPrefix+e.Origin)
		}
		pc.Entries = append(pc.Entries, pe)
		if e.Obsolete {
			continue
		}
		stats.Total++
		switch e.Status() {
		case "translated":
			stats.Translated++
		case "fuzzy":
			stats.Fuzzy++
		}
	}
	data := pc.Bytes()
	sum := sha256.Sum256(data)
	stats.ContentHash = hex.EncodeToString(sum[:])
	return data, stats, nil
}

func (s *CatalogService) latestCommit(ctx context.Context, c *models.Catalog) (*models.Commit, error) {
	l, err := s.commits.Latest(ctx, c.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return l, err
}

// commit stores a new version. Unless force is set, nothing is stored when the
// content equals the latest version (created reports whether one was made).
func (s *CatalogService) commit(ctx context.Context, authorID *int64, c *models.Catalog, kind, message string, restoredFrom *int64, force bool) (*models.Commit, bool, error) {
	data, cm, err := s.snapshot(ctx, c)
	if err != nil {
		return nil, false, err
	}
	latest, err := s.latestCommit(ctx, c)
	if err != nil {
		return nil, false, err
	}
	if latest != nil && latest.ContentHash == cm.ContentHash && !force {
		return latest, false, nil
	}
	var old []byte
	if latest != nil {
		if old, err = s.commits.Snapshot(ctx, latest.ID); err != nil {
			return nil, false, err
		}
		cm.ParentID = &latest.ID
	}
	d := DiffSnapshots(old, data)
	cm.Added, cm.Changed, cm.Removed, cm.Reviewed = d.Added, d.Changed, d.Removed, d.Reviewed
	cm.CatalogID, cm.Kind, cm.Message, cm.AuthorID, cm.RestoredFrom = c.ID, kind, message, authorID, restoredFrom
	if err := s.commits.Create(ctx, cm, data); err != nil {
		return nil, false, err
	}
	return cm, true, nil
}

// Commit saves the current state of the user's catalog with a message
func (s *CatalogService) Commit(ctx context.Context, user *models.User, c *models.Catalog, message string) (*models.Commit, error) {
	if !c.WritableBy(user.ID) {
		return nil, ErrForbidden
	}
	message = strings.TrimSpace(message)
	if message == "" || len([]rune(message)) > 500 {
		return nil, userErr("err.commit_message")
	}
	cm, created, err := s.commit(ctx, &user.ID, c, models.CommitManual, message, nil, false)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, userErr("err.nothing_to_commit")
	}
	return cm, nil
}

// SafetyCommit records the current state before a risky operation when it
// differs from the latest version. Failures are logged, not returned: the
// operation itself should not be blocked by history bookkeeping.
func (s *CatalogService) SafetyCommit(ctx context.Context, authorID int64, c *models.Catalog, message string) {
	kind := models.CommitAuto
	if latest, err := s.latestCommit(ctx, c); err == nil && latest == nil {
		kind = models.CommitInitial
	}
	if _, _, err := s.commit(ctx, &authorID, c, kind, message, nil, false); err != nil {
		log.Printf("history: safety commit of catalog %d: %v", c.ID, err)
	}
}

// AutoCommitByID records an automatic version of a catalog by id (used by
// background jobs that do not hold the catalog)
func (s *CatalogService) AutoCommitByID(ctx context.Context, authorID, catalogID int64, message string) {
	c, err := s.catalogs.GetByID(ctx, catalogID)
	if err != nil {
		log.Printf("history: load catalog %d: %v", catalogID, err)
		return
	}
	s.SafetyCommit(ctx, authorID, c, message)
}

// EnsureBaseline gives a catalog without history its first version, so files
// created before version control can always be returned to this point.
func (s *CatalogService) EnsureBaseline(ctx context.Context, c *models.Catalog) {
	latest, err := s.latestCommit(ctx, c)
	if err != nil || latest != nil {
		return
	}
	owner := c.OwnerID
	if _, _, err := s.commit(ctx, &owner, c, models.CommitInitial, CommitMsgBaseline, nil, false); err != nil {
		log.Printf("history: baseline of catalog %d: %v", c.ID, err)
	}
}

// Discard throws away uncommitted changes by restoring the latest version.
// The discarded state is still saved as an automatic version first.
func (s *CatalogService) Discard(ctx context.Context, user *models.User, c *models.Catalog) (*models.Commit, error) {
	latest, err := s.latestCommit(ctx, c)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	return s.Restore(ctx, user, c, latest.ID)
}

// History lists the versions of a catalog, newest first
func (s *CatalogService) History(ctx context.Context, c *models.Catalog) ([]*models.Commit, error) {
	return s.commits.List(ctx, c.ID)
}

// WorkingDiff compares the current state with the latest version
func (s *CatalogService) WorkingDiff(ctx context.Context, c *models.Catalog) (*Diff, error) {
	data, _, err := s.snapshot(ctx, c)
	if err != nil {
		return nil, err
	}
	latest, err := s.latestCommit(ctx, c)
	if err != nil {
		return nil, err
	}
	var old []byte
	if latest != nil {
		if old, err = s.commits.Snapshot(ctx, latest.ID); err != nil {
			return nil, err
		}
	}
	return DiffSnapshots(old, data), nil
}

func (s *CatalogService) ownCommit(ctx context.Context, c *models.Catalog, id int64) (*models.Commit, error) {
	cm, err := s.commits.Get(ctx, id)
	if err != nil || cm.CatalogID != c.ID {
		return nil, ErrNotFound
	}
	return cm, nil
}

// CommitDiff compares a version with the one before it
func (s *CatalogService) CommitDiff(ctx context.Context, c *models.Catalog, id int64) (*models.Commit, *Diff, error) {
	cm, err := s.ownCommit(ctx, c, id)
	if err != nil {
		return nil, nil, err
	}
	data, err := s.commits.Snapshot(ctx, cm.ID)
	if err != nil {
		return nil, nil, err
	}
	var old []byte
	if cm.ParentID != nil {
		if old, err = s.commits.Snapshot(ctx, *cm.ParentID); err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, nil, err
		}
	}
	return cm, DiffSnapshots(old, data), nil
}

// originFlagPrefix marks an entry's origin inside snapshots (never in downloads)
const originFlagPrefix = "x-origin-"

// CommitSnapshot returns the PO text of a version as stored (with origin flags)
func (s *CatalogService) commitSnapshot(ctx context.Context, c *models.Catalog, id int64) (*models.Commit, []byte, error) {
	cm, err := s.ownCommit(ctx, c, id)
	if err != nil {
		return nil, nil, err
	}
	data, err := s.commits.Snapshot(ctx, cm.ID)
	return cm, data, err
}

// CommitSnapshot returns the PO file of a version for download
func (s *CatalogService) CommitSnapshot(ctx context.Context, c *models.Catalog, id int64) (*models.Commit, []byte, error) {
	cm, data, err := s.commitSnapshot(ctx, c, id)
	if err != nil {
		return nil, nil, err
	}
	pc, err := po.ParseBytes(data)
	if err != nil {
		return cm, data, nil
	}
	for _, e := range pc.Entries {
		e.Flags, _ = splitOriginFlag(e.Flags)
	}
	return cm, pc.Bytes(), nil
}

// splitOriginFlag removes the origin flag from a flag list and returns the origin
func splitOriginFlag(flags []string) ([]string, string) {
	origin := ""
	out := flags[:0:0]
	for _, f := range flags {
		if strings.HasPrefix(f, originFlagPrefix) {
			switch o := strings.TrimPrefix(f, originFlagPrefix); o {
			case models.OriginAI, models.OriginGlossary, models.OriginMemory:
				origin = o
			}
			continue
		}
		out = append(out, f)
	}
	return out, origin
}

// Restore brings the catalog back to an earlier version. The current state is
// committed first and the restore is itself recorded, so it can be undone.
func (s *CatalogService) Restore(ctx context.Context, user *models.User, c *models.Catalog, id int64) (*models.Commit, error) {
	if !c.WritableBy(user.ID) {
		return nil, ErrForbidden
	}
	target, data, err := s.commitSnapshot(ctx, c, id)
	if err != nil {
		return nil, err
	}
	s.SafetyCommit(ctx, user.ID, c, CommitMsgBeforeRestore+"|"+target.ShortID())
	if err := s.replaceContent(ctx, user, c, data); err != nil {
		return nil, err
	}
	cm, _, err := s.commit(ctx, &user.ID, c, models.CommitRestore, CommitMsgRestore+"|"+target.ShortID(), &target.ID, true)
	return cm, err
}

// DiffItem is one changed string between two versions
type DiffItem struct {
	Kind     string // "added", "removed", "changed"
	Context  string
	Source   string
	Old      []string
	New      []string
	OldFuzzy bool
	NewFuzzy bool
	// OldOrigin and NewOrigin are "ai", "glossary", "memory" or "" (typed by a person)
	OldOrigin string
	NewOrigin string
}

// HasOld reports whether the previous version had a translation
func (d DiffItem) HasOld() bool { return anyText(d.Old) }

// HasNew reports whether the new version has a translation
func (d DiffItem) HasNew() bool { return anyText(d.New) }

func anyText(s []string) bool {
	for _, v := range s {
		if v != "" {
			return true
		}
	}
	return false
}

// Diff lists the differences between two versions
type Diff struct {
	Added, Changed, Removed int
	Reviewed                int // machine translations approved without text changes
	Items                   []DiffItem
	Truncated               bool
	HeaderChanged           bool
}

// Empty reports whether nothing changed
func (d *Diff) Empty() bool { return d.Total() == 0 && !d.HeaderChanged }

// Total counts every changed string
func (d *Diff) Total() int { return d.Added + d.Changed + d.Removed + d.Reviewed }

// maxDiffItems caps the items kept for display
const maxDiffItems = 500

// DiffSnapshots compares two PO snapshots entry by entry. Obsolete entries are
// ignored; nil old means an empty catalog.
func DiffSnapshots(oldData, newData []byte) *Diff {
	d := &Diff{}
	parse := func(b []byte) (*po.Catalog, map[string]*po.Entry, []string) {
		m := map[string]*po.Entry{}
		var order []string
		if len(b) == 0 {
			return &po.Catalog{}, m, nil
		}
		c, err := po.ParseBytes(b)
		if err != nil {
			return &po.Catalog{}, m, nil
		}
		for _, e := range c.Entries {
			if e.Obsolete {
				continue
			}
			k := e.Key()
			if _, dup := m[k]; !dup {
				order = append(order, k)
			}
			m[k] = e
		}
		return c, m, order
	}
	oc, om, _ := parse(oldData)
	nc, nm, norder := parse(newData)
	d.HeaderChanged = oldData != nil && (oc.Header != nc.Header || strings.Join(oc.HeaderComments, "\n") != strings.Join(nc.HeaderComments, "\n"))

	add := func(it DiffItem) {
		if len(d.Items) < maxDiffItems {
			d.Items = append(d.Items, it)
		} else {
			d.Truncated = true
		}
	}
	origin := func(e *po.Entry) string {
		_, o := splitOriginFlag(e.Flags)
		return o
	}
	for _, k := range norder {
		ne := nm[k]
		oe, ok := om[k]
		if !ok {
			d.Added++
			add(DiffItem{Kind: "added", Context: ne.Context, Source: ne.ID, New: ne.Translation, NewFuzzy: ne.Fuzzy,
				NewOrigin: origin(ne)})
			continue
		}
		oo, no := origin(oe), origin(ne)
		textChanged := !equalStrings(oe.Translation, ne.Translation)
		it := DiffItem{Context: ne.Context, Source: ne.ID, Old: oe.Translation, New: ne.Translation,
			OldFuzzy: oe.Fuzzy, NewFuzzy: ne.Fuzzy, OldOrigin: oo, NewOrigin: no}
		switch {
		case !textChanged && oo != "" && no == "":
			// a machine translation approved by a person (text kept)
			d.Reviewed++
			it.Kind = "reviewed"
			add(it)
		case textChanged || oe.Fuzzy != ne.Fuzzy || oo != no:
			d.Changed++
			it.Kind = "changed"
			add(it)
		}
	}
	for k, oe := range om {
		if _, ok := nm[k]; !ok {
			d.Removed++
			add(DiffItem{Kind: "removed", Context: oe.Context, Source: oe.ID, Old: oe.Translation, OldFuzzy: oe.Fuzzy,
				OldOrigin: origin(oe)})
		}
	}
	return d
}

func equalStrings(a, b []string) bool {
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
