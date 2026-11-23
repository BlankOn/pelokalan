package models

import "time"

// ProfileDay is one calendar day (in the site's time zone) of a person's work across all projects
type ProfileDay struct {
	Day           time.Time // the date, at midnight UTC
	Contributions int       // strings worked on: Translated + Reviewed + Edited + machine drafts saved
	Translated    int       // empty strings given a translation
	Words         int       // source words of Translated
	Reviewed      int       // strings that needed review confirmed (AI drafts too)
	Edited        int       // translated strings whose text was changed
	Night         int       // contributions between 00:00 and 04:59
	Early         int       // contributions between 05:00 and 06:59
}

// ProfileProject is a project a person worked on
type ProfileProject struct {
	ID            int64
	Slug          string
	Name          string
	Contributions int
	Translated    int
	LastActive    time.Time
}

// Path is the project's address
func (p *ProfileProject) Path() string { return "/projects/" + p.Slug }

// ProfileTotals are the counts of a person's work that do not come per day
type ProfileTotals struct {
	Projects       []*ProfileProject // most contributions first
	Files          int               // PO files worked on
	Languages      int               // languages worked in
	CompletedFiles int               // own files worked on that are fully translated (deleted ones too)
	Comments       int               // comments not deleted
	Topics         int               // discussions started
	WikiEdits      int
	ContextEdits   int // Konteks notes saved
	Commits        int // versions committed by hand
	Landed         int // strings accepted upstream (UpstreamLanding)
	LandedWords    int
	LandedModules  int // modules (per project) with a string accepted upstream
	FirstActive    *time.Time
}

// UpstreamLanding is a string a member translated in Pelokalan that later showed up, word for word, in
// the upstream file at a sync: their work was accepted upstream
type UpstreamLanding struct {
	UserID, ProjectID, CatalogID, GroupID int64
	EntryKey, Module, Filename            string
	Words                                 int
	LandedAt                              *time.Time // nil = now
}

// ModuleWork is a person's work on one module of a project over a period, for a badge's details
type ModuleWork struct {
	ProjectSlug, ProjectName string
	Module                   string // file name before the first dot; "" = the project as a whole
	Filename                 string // the most recent file name of the module
	Count                    int
	First, Last              time.Time
}

// ProjectPath is the project's address
func (m *ModuleWork) ProjectPath() string { return "/projects/" + m.ProjectSlug }

// SiteTotals are the platform-wide numbers on the home page
type SiteTotals struct {
	Contributions int // strings worked on
	Translators   int // people who worked on strings
	Projects      int
	Landed        int // strings accepted upstream
}
