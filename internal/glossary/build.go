package glossary

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"pelokalan/internal/po"
)

// TermMaxWords is the longest source text (in words) treated as a term rather than a sentence
const TermMaxWords = 4

// maxSourceLen skips very long strings (paragraphs of documentation)
const maxSourceLen = 400

// maxModules caps how many module names are stored per term
const maxModules = 30

// Term is a distinct source/translation pair with where it was found
type Term struct {
	Language        string
	Source          string
	SourceNorm      string
	Translation     string
	TranslationNorm string
	WordCount       int
	Occurrences     int
	Modules         []string
	Contexts        []string
}

// FileError reports a file that could not be parsed
type FileError struct {
	Name string
	Err  error
}

// Result is the outcome of building a glossary
type Result struct {
	Files     int
	Entries   int // translated entries that were considered
	Terms     []*Term
	Languages []string // languages found, most common first
	Skipped   []FileError
}

// Build parses the files and aggregates their translations. When language is
// not empty it is used for every file; otherwise it is read from the file
// header or the file name (e.g. "gtk.master.id.po").
func Build(files []File, language string) *Result {
	res := &Result{}
	terms := map[string]*Term{}
	langCount := map[string]int{}

	for _, f := range files {
		cat, err := po.ParseBytes(f.Data)
		if err != nil {
			res.Skipped = append(res.Skipped, FileError{Name: f.Name, Err: err})
			continue
		}
		lang := language
		if lang == "" {
			lang = detectLanguage(cat, f.Name)
		}
		if lang == "" {
			res.Skipped = append(res.Skipped, FileError{Name: f.Name, Err: fmt.Errorf("unknown language")})
			continue
		}
		res.Files++
		langCount[lang]++
		module := ModuleName(f.Name, lang)

		for _, e := range cat.Entries {
			if e.Obsolete || e.Fuzzy || !e.IsTranslated() {
				continue
			}
			if isCredits(e.ID) {
				continue
			}
			src, tr := CleanPair(e.ID, e.Translation[0])
			if src == "" || tr == "" || len(src) > maxSourceLen || !hasLetter(src) {
				continue
			}
			res.Entries++
			sn, tn := Normalize(src), Normalize(tr)
			key := lang + "\x00" + sn + "\x00" + tn
			t := terms[key]
			if t == nil {
				t = &Term{
					Language:        lang,
					Source:          src,
					SourceNorm:      sn,
					Translation:     tr,
					TranslationNorm: tn,
					WordCount:       len(strings.Fields(src)),
				}
				terms[key] = t
			}
			t.Occurrences++
			t.Modules = addUnique(t.Modules, module, maxModules)
			if e.HasContext && e.Context != "" {
				t.Contexts = addUnique(t.Contexts, e.Context, 10)
			}
		}
	}

	for _, t := range terms {
		res.Terms = append(res.Terms, t)
	}
	sort.Slice(res.Terms, func(i, j int) bool {
		a, b := res.Terms[i], res.Terms[j]
		if a.SourceNorm != b.SourceNorm {
			return a.SourceNorm < b.SourceNorm
		}
		return a.TranslationNorm < b.TranslationNorm
	})
	for l := range langCount {
		res.Languages = append(res.Languages, l)
	}
	sort.Slice(res.Languages, func(i, j int) bool {
		return langCount[res.Languages[i]] > langCount[res.Languages[j]]
	})
	return res
}

func addUnique(list []string, v string, max int) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	if len(list) >= max {
		return list
	}
	return append(list, v)
}

func detectLanguage(cat *po.Catalog, name string) string {
	if l := strings.TrimSpace(cat.HeaderField("Language")); l != "" {
		return l
	}
	// "module.branch.id.po" or "id.po"
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".po"), ".PO")
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	if len(base) >= 2 && len(base) <= 12 && po.LookupLanguage(base).Name != base {
		return base
	}
	return ""
}

// ModuleName derives a module label from a file name ("gtk.gtk-4-24.id.po" -> "gtk.gtk-4-24")
func ModuleName(name, lang string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".po"), ".PO")
	base = strings.TrimSuffix(base, "."+lang)
	if base == "" || base == lang {
		return name
	}
	return base
}

// Clean removes keyboard accelerators, trailing ellipsis/colon and extra
// whitespace so "_Open…" and "Open" become the same term.
func Clean(s string) string {
	s, _ = stripAccelerator(s, false)
	return tidy(s)
}

// CleanPair cleans a source string and its translation. When the source has
// a keyboard accelerator, the translation's marker is removed wherever it is
// (translators often move it, e.g. "_Open" -> "B_uka").
func CleanPair(src, tr string) (string, string) {
	src, had := stripAccelerator(src, false)
	tr, _ = stripAccelerator(tr, had)
	return tidy(src), tidy(tr)
}

func tidy(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for {
		t := strings.TrimRight(s, " ")
		t = strings.TrimSuffix(t, "…")
		t = strings.TrimSuffix(t, "...")
		t = strings.TrimSuffix(t, ":")
		t = strings.TrimRight(t, " ")
		if t == s {
			return s
		}
		s = t
	}
}

// Normalize returns the comparison key of a cleaned string
func Normalize(s string) string { return strings.ToLower(s) }

var identifierRe = regexp.MustCompile(`^[a-z0-9_.\-]+$`)

// stripAccelerator removes a single mnemonic marker ("_" or "&") placed before
// a letter or digit. Unless force is set, lowercase identifiers such as
// "file_name" are left untouched. It reports whether a marker was removed.
func stripAccelerator(s string, force bool) (string, bool) {
	for _, marker := range []string{"_", "&"} {
		if strings.Count(s, marker) != 1 {
			continue
		}
		i := strings.Index(s, marker)
		next, _ := utf8.DecodeRuneInString(s[i+1:])
		if next == utf8.RuneError || !(unicode.IsLetter(next) || unicode.IsDigit(next)) {
			continue
		}
		if marker == "_" && !force && identifierRe.MatchString(s) {
			continue
		}
		return s[:i] + s[i+1:], true
	}
	return s, false
}

// isCredits reports entries that hold translator names rather than UI text
func isCredits(id string) bool {
	switch strings.ToLower(id) {
	case "translator-credits", "translator_credits", "translators", "your names", "your emails":
		return true
	}
	return false
}

var placeholderRe = regexp.MustCompile(`%(?:\d+\$)?[-+ 0#']*\d*(?:\.\d+)?(?:hh|h|ll|l|L|z|j|t)?[a-zA-Z%]|%\([A-Za-z0-9_]+\)[a-z]|\{[^{}]*\}`)

// hasLetter reports whether s has a letter outside format placeholders
func hasLetter(s string) bool {
	for _, r := range placeholderRe.ReplaceAllString(s, "") {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// StripPlaceholders replaces format placeholders (%s, %1$d, {name}) with spaces
func StripPlaceholders(s string) string { return placeholderRe.ReplaceAllString(s, " ") }

// TermHash is the stable id of a glossary string: the hex SHA-256 of its
// normalized source (so "Search" and "search" share one id)
func TermHash(sourceNorm string) string {
	sum := sha256.Sum256([]byte(sourceNorm))
	return hex.EncodeToString(sum[:])
}
