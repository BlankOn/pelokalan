package models

import "time"

// CatalogContext is the Konteks note of a PO file
type CatalogContext struct {
	CatalogID int64
	Note      string
	UpdatedBy *int64
	UpdatedAt time.Time

	// Filled by queries
	UpdaterName   string
	Filename      string
	OwnerUsername string
	Language      string
	IsTemplate    bool
}

// FileURL opens the PO file in the editor
func (c *CatalogContext) FileURL(projectPath string) string {
	return projectPath + "/" + c.OwnerUsername + "/po/" + c.Filename
}
