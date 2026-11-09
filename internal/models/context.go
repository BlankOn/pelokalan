package models

import "time"

// ModuleContext is the Konteks note of a module (all PO files whose name starts with the
// same word, e.g. "tecla.po-ui.gnome-51.id.po" and "tecla.gnome-51.id.po"). It is shared
// by the project's members and does not depend on any one file.
type ModuleContext struct {
	ID        int64
	ProjectID int64
	Module    string
	Note      string
	Version   int64 // bumped on every save; a save with an older version is a conflict
	UpdatedBy *int64
	UpdatedAt time.Time

	// Filled by queries
	UpdaterName string
	// Filled by handlers: the project's files of this module
	Files []*Catalog
}

// ContextEdit is one save of a module's Konteks
type ContextEdit struct {
	Username string
	At       time.Time
}
