package models

import "time"

// Commit kinds
const (
	CommitManual  = "manual"  // saved by the user with a message
	CommitAuto    = "auto"    // safety snapshot before a risky operation
	CommitRestore = "restore" // result of restoring an earlier version
	CommitInitial = "initial" // first version after upload or creation
)

// Commit is one saved version of a catalog
type Commit struct {
	ID           int64
	CatalogID    int64
	ParentID     *int64
	Kind         string
	Message      string
	AuthorID     *int64
	AuthorName   string
	ContentHash  string
	Total        int
	Translated   int
	Fuzzy        int
	Added        int
	Changed      int
	Removed      int
	Reviewed     int // machine translations approved by a person
	RestoredFrom *int64
	CreatedAt    time.Time
}

// Percent returns the translated share at this version
func (c *Commit) Percent() int {
	if c.Total == 0 {
		return 0
	}
	return c.Translated * 100 / c.Total
}

// FuzzyPercent returns the needs-work share at this version
func (c *Commit) FuzzyPercent() int {
	if c.Total == 0 {
		return 0
	}
	return c.Fuzzy * 100 / c.Total
}

// ShortID is a compact label such as "#12"
func (c *Commit) ShortID() string { return "#" + itoa(c.ID) }

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
