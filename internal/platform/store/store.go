// Package store holds what every domain's Postgres code shares: the sentinel errors repositories return and small
// SQL helpers
package store

import (
	"errors"
	"strconv"
	"strings"
)

// ErrNotFound is returned when a record does not exist
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a record was changed by someone else
var ErrConflict = errors.New("conflict")

// EscapeLike escapes the wildcards of a LIKE pattern (used with the default backslash escape)
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Arg is the number of a query placeholder ($n) as text
func Arg(n int) string { return strconv.Itoa(n) }

// NonNil turns a nil slice into an empty one (stored as an empty array, not NULL)
func NonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
