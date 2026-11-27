package models

import "time"

// Review states of a module sent to a repository Pelokalan writes to
const (
	ReviewRequested = "requested" // waiting for an owner/manager
	ReviewRejected  = "rejected"  // sent back to the translator with a note
	ReviewWithdrawn = "withdrawn" // the translator took the request back
	ReviewPushed    = "pushed"    // approved and committed to the repository
)

// ModuleReview is a request to review a member's file (one module) before Pelokalan commits it to the
// module group's repository, and its outcome
type ModuleReview struct {
	ID          int64
	ProjectID   int64
	CatalogID   int64
	GroupID     *int64
	Module      string
	RepoPath    string
	Status      string
	RequestedBy int64
	Note        string // from the translator
	Entries     int    // strings to send (when asked) or sent (when pushed)
	CreatedAt   time.Time
	ReviewedBy  *int64
	ReviewedAt  *time.Time
	ReviewNote  string // from the reviewer (why it was sent back)
	CommitSHA   string
	CommitURL   string
	PushedData  []byte // gzip of the repository file as committed (only set when storing)
	Error       string // last failed attempt to commit (the request stays open)

	// Filled by queries
	OpenComments      int // open review comments on the file (ReviewService.Attach/Open)
	ResolvedComments  int // its resolved review comments: with none open, the translator fixed everything
	RequesterUsername string
	RequesterName     string
	ReviewerUsername  string
	Filename          string
	OwnerUsername     string
	ProjectSlug       string
	FileUpdatedAt     time.Time // the file's last change (the translator's fixes after the request)
}

// UpdatedAfter reports whether the file changed more than a minute after the request was made (the queue then says
// when it was updated too)
func (r *ModuleReview) UpdatedAfter() bool { return r.FileUpdatedAt.Sub(r.CreatedAt) > time.Minute }

// ShortSHA is the commit's short id
func (r *ModuleReview) ShortSHA() string {
	if len(r.CommitSHA) > 7 {
		return r.CommitSHA[:7]
	}
	return r.CommitSHA
}

// FileURL is the reviewed file's editor page
func (r *ModuleReview) FileURL() string {
	return (&Catalog{ID: r.CatalogID, ProjectSlug: r.ProjectSlug, OwnerUsername: r.OwnerUsername, Filename: r.Filename}).URL()
}
