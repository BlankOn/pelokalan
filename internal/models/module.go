package models

import (
	"path"
	"path/filepath"
	"strings"
)

// ModuleKey names the module a PO file belongs to: the part before the first dot, lowercase, so
// "tecla.po-ui.gnome-51.id.po" and "tecla.gnome-51.id.po" are the same module
func ModuleKey(filename string) string {
	name := strings.ToLower(path.Base(filename))
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return name
}

// ModuleOf returns the module a PO file belongs to
func ModuleOf(c *Catalog) string { return ModuleKey(c.Filename) }

// CleanFilename is the last element of an uploaded file name (no directories, either slash), or fallback when
// nothing is left
func CleanFilename(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return fallback
	}
	return name
}
