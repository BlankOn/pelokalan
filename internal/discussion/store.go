package discussion

import (
	"context"

	"pelokalan/internal/models"
)

// DiscussionFilter narrows a discussion listing
type DiscussionFilter struct {
	MentionedUserID int64 // only discussions mentioning this user
	SubjectType     string
	Query           string // every word must appear in the title or a visible comment
	Limit, Offset   int
}

// DiscussionRepository stores generic discussions and their comments
type Repository interface {
	Get(ctx context.Context, projectID int64, subjectType, subjectKey string) (*models.Discussion, error)
	GetByID(ctx context.Context, id int64) (*models.Discussion, error)
	// GetOrCreate returns the discussion of a subject, creating it when missing
	GetOrCreate(ctx context.Context, d *models.Discussion) error
	List(ctx context.Context, projectID int64, f DiscussionFilter) ([]*models.Discussion, int, error)
	// Counts returns comment counts per subject key
	Counts(ctx context.Context, projectID int64, subjectType string, keys []string) (map[string]int, error)

	// AddComment stores a comment with its mentions and attachments (blobs must exist)
	AddComment(ctx context.Context, c *models.Comment, mentions []int64, attachments []*models.Attachment) error
	// Attachment returns an attachment with the project of its discussion
	Attachment(ctx context.Context, id int64) (*models.Attachment, int64, error)
	Comments(ctx context.Context, discussionID int64) ([]*models.Comment, error)
	GetComment(ctx context.Context, id int64) (*models.Comment, error)
	// ResolveComment marks an open review comment resolved
	ResolveComment(ctx context.Context, id, byUserID int64) error
	// OpenReviewComments counts the open review comments on a file's entries, by entry key hash
	OpenReviewComments(ctx context.Context, projectID, catalogID int64) (map[string]int, error)
	// ResolvedReviewEntries counts the resolved review comments on a file's entries, by entry key hash
	ResolvedReviewEntries(ctx context.Context, projectID, catalogID int64) (map[string]int, error)
	// ReviewAnchors lists the passages review comments on a file are about (open ones first), by entry key hash
	ReviewAnchors(ctx context.Context, projectID, catalogID int64) (map[string][]models.CommentAnchor, error)
	// ResolvedReviewComments counts the resolved (not deleted) comments tied to a review request
	ResolvedReviewComments(ctx context.Context, reviewID int64) (int, error)
	UpdateComment(ctx context.Context, id int64, body string, mentions []int64) error
	DeleteComment(ctx context.Context, id, byUserID int64) error
	// DeleteDiscussion removes a discussion with its comments, mentions and attachment links
	DeleteDiscussion(ctx context.Context, id int64) error
}
