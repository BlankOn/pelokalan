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
	CompletedFiles int               // own files worked on that are now fully translated
	Comments       int               // comments not deleted
	Topics         int               // discussions started
	WikiEdits      int
	ContextEdits   int // Konteks notes saved
	Commits        int // versions committed by hand
	FirstActive    *time.Time
}
