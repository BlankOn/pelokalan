package models

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pelokalan/internal/po"
)

// Catalog is one translation file of a project
type Catalog struct {
	ID            int64
	ProjectID     int64
	OwnerID       int64
	OwnerUsername string
	ProjectSlug   string
	// Owned is true when the viewing user owns the file and may change it (set by the
	// service, not stored; false for a deleted file)
	Owned bool
	// DeletedAt is set when the file was deleted: it stays readable, but nobody changes it
	DeletedAt      *time.Time
	DeletedBy      *int64
	DeleterName    string
	IsTemplate     bool
	Language       string
	Filename       string
	Header         string
	HeaderComments string
	PluralForms    string
	NPlurals       int
	CreatedBy      *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time

	Stats CatalogStats
}

// IsDeleted reports whether the file was deleted (kept read-only)
func (c *Catalog) IsDeleted() bool { return c.DeletedAt != nil }

// DeletedTime is when the file was deleted (zero when it was not)
func (c *Catalog) DeletedTime() time.Time {
	if c.DeletedAt == nil {
		return time.Time{}
	}
	return *c.DeletedAt
}

// WritableBy reports whether a user may change the file: its owner, while it is not deleted
func (c *Catalog) WritableBy(userID int64) bool { return c.OwnerID == userID && c.DeletedAt == nil }

// URL is the address of the file in the owner's workspace,
// e.g. /projects/1/ani/po/orca.id.po
func (c *Catalog) URL() string {
	if c.DeletedAt != nil {
		// a newer file may use the same name; the id always finds this one
		return "/catalogs/" + strconv.FormatInt(c.ID, 10)
	}
	project := c.ProjectSlug
	if project == "" {
		project = strconv.FormatInt(c.ProjectID, 10)
	}
	return "/projects/" + project + "/" + url.PathEscape(c.OwnerUsername) + "/po/" + url.PathEscape(c.Filename)
}

// LanguageName returns the display name of the catalog language
func (c *Catalog) LanguageName() string { return po.LanguageName(c.Language) }

// CatalogStats summarizes translation progress
type CatalogStats struct {
	Total        int
	Translated   int
	Fuzzy        int
	Untranslated int
}

// Percent returns the share of translated, non-fuzzy entries
func (s CatalogStats) Percent() int {
	if s.Total == 0 {
		return 0
	}
	return s.Translated * 100 / s.Total
}

// FuzzyPercent returns the share of fuzzy entries
func (s CatalogStats) FuzzyPercent() int {
	if s.Total == 0 {
		return 0
	}
	return s.Fuzzy * 100 / s.Total
}

// Entry is one message of a catalog
type Entry struct {
	ID                 int64
	CatalogID          int64
	Position           int
	Context            *string
	MsgID              string
	MsgIDPlural        string
	MsgStr             []string
	Fuzzy              bool
	Flags              string // comma separated, without fuzzy
	TranslatorComments string
	ExtractedComments  string
	References         string
	Previous           string
	Obsolete           bool
	Version            int
	UpdatedBy          *int64
	UpdatedByName      string
	UpdatedAt          time.Time
	// Comments in the entry's discussion (filled by handlers)
	Comments int
	// Origin of a machine translation not yet saved by a person: "ai", "glossary", "memory"
	Origin string
	// ReplacedMsgStr is the translation a machine replaced, kept until a person saves (nil = none)
	ReplacedMsgStr []string

	// Computed by the service
	Issues []string
}

// IsPlural reports whether the entry has plural forms
func (e *Entry) IsPlural() bool { return e.MsgIDPlural != "" }

// IsTranslated reports whether every form has a translation
func (e *Entry) IsTranslated() bool { return e.ToPO().IsTranslated() }

// Status returns one of "untranslated", "fuzzy", "translated"
func (e *Entry) Status() string {
	switch {
	case !e.IsTranslated():
		return "untranslated"
	case e.Fuzzy:
		return "fuzzy"
	default:
		return "translated"
	}
}

// Entry origins
const (
	OriginAI       = "ai"
	OriginGlossary = "glossary"
	OriginMemory   = "memory"
)

// KeyHash is a stable short id of the entry inside its file (context + source),
// used to attach discussions; it survives restores and template merges
func (e *Entry) KeyHash() string {
	sum := sha256.Sum256([]byte(e.Key()))
	return hex.EncodeToString(sum[:8])
}

// IsAI reports whether the translation was produced by a language model and not yet saved by a person
func (e *Entry) IsAI() bool { return e.Origin == OriginAI }

// HasIssues reports whether validation found problems
func (e *Entry) HasIssues() bool { return len(e.Issues) > 0 }

// FirstTranslation returns the first translated form
func (e *Entry) FirstTranslation() string {
	if len(e.MsgStr) == 0 {
		return ""
	}
	return e.MsgStr[0]
}

// ContextValue returns the context or an empty string
func (e *Entry) ContextValue() string {
	if e.Context == nil {
		return ""
	}
	return *e.Context
}

// FlagList returns the flags as a slice
func (e *Entry) FlagList() []string { return splitNonEmpty(e.Flags, ",") }

// ReferenceList returns source references, one per item
func (e *Entry) ReferenceList() []string {
	var out []string
	for _, line := range splitNonEmpty(e.References, "\n") {
		out = append(out, strings.Fields(line)...)
	}
	return out
}

// PreviousID returns the previous source text recorded for fuzzy entries
func (e *Entry) PreviousID() string {
	if e.Previous == "" {
		return ""
	}
	// The previous strings are stored as raw PO lines; parse them as an entry.
	c, err := po.ParseBytes([]byte("msgid \"\"\nmsgstr \"\"\n\n" + e.Previous + "\nmsgstr \"\"\n"))
	if err != nil || len(c.Entries) == 0 {
		return ""
	}
	return c.Entries[0].ID
}

// ToPO converts the entry to the file representation
func (e *Entry) ToPO() *po.Entry {
	pe := &po.Entry{
		TranslatorComments: splitLines(e.TranslatorComments),
		ExtractedComments:  splitLines(e.ExtractedComments),
		References:         splitNonEmpty(e.References, "\n"),
		Flags:              e.FlagList(),
		Fuzzy:              e.Fuzzy,
		Previous:           splitNonEmpty(e.Previous, "\n"),
		ID:                 e.MsgID,
		IDPlural:           e.MsgIDPlural,
		Translation:        append([]string(nil), e.MsgStr...),
		Obsolete:           e.Obsolete,
	}
	if e.Context != nil {
		pe.HasContext = true
		pe.Context = *e.Context
	}
	return pe
}

// EntryFromPO converts a parsed entry to the storage representation
func EntryFromPO(pe *po.Entry, position int) *Entry {
	e := &Entry{
		Position:           position,
		MsgID:              pe.ID,
		MsgIDPlural:        pe.IDPlural,
		MsgStr:             append([]string(nil), pe.Translation...),
		Fuzzy:              pe.Fuzzy,
		Flags:              strings.Join(pe.Flags, ","),
		TranslatorComments: strings.Join(pe.TranslatorComments, "\n"),
		ExtractedComments:  strings.Join(pe.ExtractedComments, "\n"),
		References:         strings.Join(pe.References, "\n"),
		Previous:           strings.Join(pe.Previous, "\n"),
		Obsolete:           pe.Obsolete,
	}
	if pe.HasContext {
		ctx := pe.Context
		e.Context = &ctx
	}
	if len(e.MsgStr) == 0 {
		e.MsgStr = []string{""}
	}
	return e
}

// Key identifies the entry inside its catalog
func (e *Entry) Key() string { return e.ToPO().Key() }

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
