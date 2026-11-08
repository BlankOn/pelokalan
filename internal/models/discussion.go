package models

import "time"

// Discussion subject types. A discussion is attached to one subject of a
// project, identified by its type and a key; new types only need a resolver
// in the discussion service.
const (
	SubjectGlossaryTerm = "glossary_term" // key: glossary term hash
	SubjectPOEntry      = "po_entry"      // key: "<catalog id>-<entry key hash>"
)

// Discussion is a comment thread attached to a subject
type Discussion struct {
	ID             int64
	ProjectID      int64
	SubjectType    string
	SubjectKey     string
	Title          string // snapshot of the subject's name, for listings
	CreatedBy      *int64
	CommentCount   int
	LastActivityAt time.Time
	CreatedAt      time.Time

	// Filled by listing queries
	LastAuthor   string
	Participants int
}

// Comment is one message of a discussion
type Comment struct {
	ID             int64
	DiscussionID   int64
	AuthorID       *int64
	AuthorName     string
	AuthorUsername string
	Body           string
	CreatedAt      time.Time
	EditedAt       *time.Time
	DeletedAt      *time.Time

	Attachments []*Attachment
}

// IsDeleted reports whether the comment was removed
func (c *Comment) IsDeleted() bool { return c.DeletedAt != nil }

// Attachment is a file attached to a comment
type Attachment struct {
	ID          int64
	CommentID   int64
	BlobKey     string
	Filename    string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

// inlineTypes are the only types shown inside pages; everything else is downloaded
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// IsImage reports whether the attachment can be shown inline
func (a *Attachment) IsImage() bool { return inlineTypes[a.ContentType] }

// IsInlineType reports whether a content type may be rendered inline
func IsInlineType(contentType string) bool { return inlineTypes[contentType] }
