package models

import "time"

// ModuleContext is the Konteks note of a module (all PO files whose name starts with the
// same word, e.g. "tecla.po-ui.gnome-51.id.po" and "tecla.gnome-51.id.po"). It is shared
// by the project's members and does not depend on any one file.
type ModuleContext struct {
	ID        int64
	ProjectID int64
	Module    string // file name before the first dot; "~…" for an item that belongs to no module
	Title     string // an item without a module is named by its title
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

// IsGeneral reports whether the item belongs to no module
func (c *ModuleContext) IsGeneral() bool { return len(c.Module) > 0 && c.Module[0] == '~' }

// GeneralKey is the address part of an item without a module (its module name without the "~")
func (c *ModuleContext) GeneralKey() string {
	if !c.IsGeneral() {
		return ""
	}
	return c.Module[1:]
}
