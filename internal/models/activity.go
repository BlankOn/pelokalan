package models

import "time"

// Activity event kinds (activity_events.kind)
const (
	ActivityEntrySaved     = "entry.saved"
	ActivityCommentPosted  = "comment.posted"
	ActivityCommitCreated  = "commit.created"
	ActivityUserRegistered = "user.registered"
)

// ActivityEvent is one thing a person did, recorded for insight and analytics
type ActivityEvent struct {
	Kind         string
	UserID       *int64
	ProjectID    *int64
	CatalogID    *int64
	EntryID      *int64
	Language     string
	StatusBefore string
	StatusAfter  string
	Origin       string // machine origin of the text before the save
	TextChanged  bool
	Words        int            // words in the source string
	Data         map[string]any // anything kind-specific
}

// ActivitySummary adds up a member's activity in a project over a period
type ActivitySummary struct {
	UserID          int64
	Username        string
	Name            string
	Translated      int // untranslated entries given a translation
	TranslatedWords int
	Reviewed        int // fuzzy entries confirmed (flag cleared)
	Edited          int // translated entries whose text changed
	DraftsApproved  int // machine drafts saved unchanged
	DraftsFixed     int // machine drafts corrected before saving
	Comments        int
	Commits         int
	LastActive      time.Time
}

// ActivityDay is one period (an hour for the growth chart) of translation work in a project: strings
// people saved with a translation, split by where the text came from. Strings is the total.
type ActivityDay struct {
	Day     time.Time
	Strings int // Manual + AIKept + AIEdited + Edited

	Manual   int // typed by hand: an empty string saved with a translation
	AIKept   int // an AI draft saved unchanged
	AIEdited int // an AI draft changed before saving
	Edited   int // a translated string (no machine draft) whose text was changed
}
