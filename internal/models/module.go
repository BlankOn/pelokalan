package models

import (
	"path"
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
