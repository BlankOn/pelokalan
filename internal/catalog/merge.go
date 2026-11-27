package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/pkg/po"
)

// Merging a member's file with a newer upstream copy ("Gabungkan dengan upstream"), a three-way merge per string:
// the member's starting point (what Pelokalan last committed for the string, else the file's reference version),
// the member's current text and the upstream text.
//   - only upstream changed it → upstream's
//   - only the member changed it → the member's
//   - both changed it, differently → a conflict, decided string by string by the person merging
// Upstream decides which strings exist (new ones come in, removed ones go). The result keeps the member's unsent
// work, so nothing is lost the way a plain replace loses it.

// CommitMsgBeforeUpstreamMerge and CommitMsgUpstreamMerged are the versions recorded around a merge
const (
	CommitMsgBeforeUpstreamMerge = "commit.before_upstream_merge"
	CommitMsgUpstreamMerged      = "commit.upstream_merged"
)

// MergeConflict is a string both the member and upstream changed since the member's starting point
type MergeConflict struct {
	Key       string   `json:"key"` // models.KeyHashOf(entry key): the choice's form name
	Context   string   `json:"context,omitempty"`
	Source    string   `json:"source"`
	Mine      []string `json:"mine"`
	Upstream  []string `json:"upstream"`
	MineFuzzy bool     `json:"mine_fuzzy,omitempty"`
	UpFuzzy   bool     `json:"upstream_fuzzy,omitempty"`
}

// MergePlan is what merging with upstream would do; nothing is changed yet
type MergePlan struct {
	Version   string          `json:"version"`   // hash of the upstream file: the merge is applied only to this one
	Label     string          `json:"label"`     // where upstream came from ("python/python-docs-id@3.14", "gnome-51" …)
	Updated   int             `json:"updated"`   // strings taken from upstream (changed there, not by the member)
	Kept      int             `json:"kept"`      // the member's changes kept (upstream did not touch them)
	Added     int             `json:"added"`     // strings new upstream
	Removed   int             `json:"removed"`   // strings no longer upstream
	Same      int             `json:"same"`      // already equal
	Conflicts []MergeConflict `json:"conflicts"` // changed on both sides
}

type mergeChoice int

const (
	takeUpstream mergeChoice = iota
	keepMine
	conflict
)

// UpstreamVersion is the version token of an upstream file (a content hash)
func UpstreamVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// mergeStart is the member's starting point per string: what Pelokalan last committed (sent), else the reference
func (s *Service) mergeStart(ctx context.Context, c *models.Catalog, sent []byte) map[string]poState {
	start := s.reference(ctx, c)
	if sent == nil {
		return start
	}
	sc, err := po.ParseBytes(sent)
	if err != nil {
		return start
	}
	if start == nil {
		start = map[string]poState{}
	}
	for _, e := range sc.Entries {
		start[e.Key()] = poState{e.Translation, e.Fuzzy}
	}
	return start
}

// decide merges one string
func decide(start poState, known bool, mine, up poState) mergeChoice {
	if sameState(mine, up) {
		return takeUpstream
	}
	if known {
		mineChanged, upChanged := !sameState(start, mine), !sameState(start, up)
		switch {
		case !mineChanged:
			return takeUpstream
		case !upChanged:
			return keepMine
		}
		return conflict
	}
	// no starting point: an empty side never wins over a translation
	switch {
	case !translated(mine.tr):
		return takeUpstream
	case !translated(up.tr):
		return keepMine
	}
	return conflict
}

// PlanUpstreamMerge works out what merging the file with an upstream copy would do (sent: the repository file as
// Pelokalan last committed it, nil = none)
func (s *Service) PlanUpstreamMerge(ctx context.Context, c *models.Catalog, upstream []byte, label string, sent []byte) (*MergePlan, error) {
	plan, _, err := s.mergeUpstream(ctx, c, upstream, sent, nil)
	if err != nil {
		return nil, err
	}
	plan.Label = label
	return plan, nil
}

// mergeUpstream computes the plan and, when choices is not nil, the merged file (every conflict must have a choice:
// key → "mine" or "upstream")
func (s *Service) mergeUpstream(ctx context.Context, c *models.Catalog, upstream, sent []byte, choices map[string]string) (*MergePlan, []byte, error) {
	up, err := po.ParseBytes(upstream)
	if err != nil {
		return nil, nil, apperr.ErrInvalidFile
	}
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return nil, nil, err
	}
	mine := make(map[string]*models.Entry, len(entries))
	for _, e := range entries {
		if !e.Obsolete {
			mine[e.Key()] = e
		}
	}
	start := s.mergeStart(ctx, c, sent)
	plan := &MergePlan{Version: UpstreamVersion(upstream), Conflicts: []MergeConflict{}}
	out := &po.Catalog{Header: up.Header, HeaderComments: up.HeaderComments, HeaderFuzzy: up.HeaderFuzzy}
	seen := map[string]bool{}
	nplurals := po.NPlurals(up.HeaderField("Plural-Forms"))
	for _, ue := range up.Entries {
		if ue.Obsolete {
			out.Entries = append(out.Entries, ue)
			continue
		}
		k := ue.Key()
		seen[k] = true
		m, has := mine[k]
		if !has {
			plan.Added++
			out.Entries = append(out.Entries, ue)
			continue
		}
		mineSt, upSt := poState{m.MsgStr, m.Fuzzy}, poState{ue.Translation, ue.Fuzzy}
		st, known := start[k]
		choice := decide(st, known, mineSt, upSt)
		if sameState(mineSt, upSt) {
			plan.Same++
		}
		switch choice {
		case takeUpstream:
			if !sameState(mineSt, upSt) {
				plan.Updated++
			}
		case keepMine:
			plan.Kept++
		case conflict:
			key := models.KeyHashOf(k)
			plan.Conflicts = append(plan.Conflicts, MergeConflict{Key: key, Context: ue.Context, Source: ue.ID,
				Mine: m.MsgStr, Upstream: ue.Translation, MineFuzzy: m.Fuzzy, UpFuzzy: ue.Fuzzy})
			if choices != nil {
				switch choices[key] {
				case "mine":
					choice = keepMine
				case "upstream":
					choice = takeUpstream
				default:
					return nil, nil, apperr.New("err.merge_choice_missing")
				}
			}
		}
		if choice == keepMine { // upstream's comments and references, the member's text, state and origin
			pe := *ue
			pe.Translation = FitForms(m.MsgStr, ue.IsPlural(), nplurals)
			pe.Fuzzy = m.Fuzzy
			pe.Flags = append([]string(nil), ue.Flags...)
			if m.Origin != "" {
				pe.Flags = append(pe.Flags, originFlagPrefix+m.Origin)
			}
			out.Entries = append(out.Entries, &pe)
			continue
		}
		out.Entries = append(out.Entries, ue)
	}
	for k := range mine {
		if !seen[k] {
			plan.Removed++
		}
	}
	if choices == nil {
		return plan, nil, nil
	}
	var buf bytes.Buffer
	if err := out.Write(&buf); err != nil {
		return nil, nil, err
	}
	return plan, buf.Bytes(), nil
}

// ApplyUpstreamMerge merges the file with the upstream copy the plan was made for (version must still match), with
// the person's choice for every conflict. History: the current state first, then the upstream copy (the member's new
// starting point), then the merged result, so every step can be restored. actor is the file's owner or a
// coordinator (the caller checks); manage marks the latter.
func (s *Service) ApplyUpstreamMerge(ctx context.Context, actor *models.User, manage bool, c *models.Catalog, upstream []byte, label, version string, sent []byte, choices map[string]string) (*MergePlan, error) {
	if c.IsTemplate || c.IsDeleted() || !(c.OwnerID == actor.ID || manage) {
		return nil, apperr.ErrForbidden
	}
	if version != UpstreamVersion(upstream) {
		return nil, apperr.New("err.merge_stale") // upstream changed since the plan: look again
	}
	if choices == nil {
		choices = map[string]string{}
	}
	plan, merged, err := s.mergeUpstream(ctx, c, upstream, sent, choices)
	if err != nil {
		return nil, err
	}
	plan.Label = label
	s.SafetyCommit(ctx, actor.ID, c, CommitMsgBeforeUpstreamMerge)
	if err := s.replaceContent(ctx, actor, c, upstream); err != nil {
		return nil, err
	}
	s.SafetyCommit(ctx, actor.ID, c, CommitMsgFromUpstream+"|"+label) // the member's new starting point
	if err := s.replaceContent(ctx, actor, c, merged); err != nil {
		return nil, err
	}
	s.SafetyCommit(ctx, actor.ID, c, fmt.Sprintf("%s|%s|%d|%d", CommitMsgUpstreamMerged,
		strings.ReplaceAll(actor.Username, "|", ""), plan.Kept, len(plan.Conflicts)))
	return plan, nil
}
