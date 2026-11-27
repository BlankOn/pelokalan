// Package i18n holds the user interface strings. Bahasa Indonesia is the
// primary language; English is the secondary one.
package i18n

import (
	"fmt"
	"log"
)

// Supported UI languages
const (
	ID = "id"
	EN = "en"
)

// Default is the UI language used when nothing else was chosen
var Default = ID

// Languages lists the selectable UI languages
var Languages = []struct{ Code, Name string }{
	{ID, "Bahasa Indonesia"},
	{EN, "English"},
}

// IsSupported reports whether lang is a supported UI language
func IsSupported(lang string) bool { return lang == ID || lang == EN }

// Normalize returns lang when supported, else the default
func Normalize(lang string) string {
	if IsSupported(lang) {
		return lang
	}
	return Default
}

// T translates key into lang, formatting args with fmt.Sprintf when given.
// Missing strings fall back to Indonesian, then to the key itself.
func T(lang, key string, args ...any) string {
	s, ok := messages[Normalize(lang)][key]
	if !ok {
		s, ok = messages[ID][key]
		if !ok {
			log.Printf("i18n: missing key %q", key)
			s = key
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Keys returns all keys of a language (used by tests)
func Keys(lang string) []string {
	var out []string
	for k := range messages[lang] {
		out = append(out, k)
	}
	return out
}

// Has reports whether key exists
func Has(key string) bool {
	_, ok := messages[ID][key]
	return ok
}
