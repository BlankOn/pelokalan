package models

import "time"

// WikiMaxDepth is how deep wiki pages can be nested (1 = top level)
const WikiMaxDepth = 3

// WikiPage is a Markdown page of a project wiki
type WikiPage struct {
	ID        int64
	ProjectID int64
	ParentID  *int64
	Slug      string
	Title     string
	Body      string
	Position  int
	CreatedBy *int64
	UpdatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time

	// Filled by queries
	UpdaterName string

	// Filled when building the tree
	Depth    int
	Children []*WikiPage
	parent   *WikiPage
}

// Parent is the parent page once the tree is built (nil for top-level pages)
func (p *WikiPage) Parent() *WikiPage { return p.parent }

// SetParent links the page to its parent in a built tree
func (p *WikiPage) SetParent(parent *WikiPage) { p.parent = parent }

// Version identifies the saved state for conflict detection
func (p *WikiPage) Version() int64 { return p.UpdatedAt.UnixNano() }

// WikiFile is a file uploaded for wiki pages
type WikiFile struct {
	ID          int64
	ProjectID   int64
	BlobKey     string
	Filename    string
	ContentType string
	Size        int64
	UploadedBy  *int64
	CreatedAt   time.Time
}

// IsImage reports whether the file can be shown inline
func (f *WikiFile) IsImage() bool { return IsInlineType(f.ContentType) }
