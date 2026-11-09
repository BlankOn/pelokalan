// Package thesaurus looks up Indonesian synonyms and antonyms. The data (word, word class,
// synonyms, antonyms) is downloaded once into the database and kept in memory.
package thesaurus

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

// Entry is one thesaurus headword
type Entry struct {
	Word     string   `json:"word"`
	Tag      string   `json:"tag"` // word class: n, v, a, adv, num, p, pron
	Synonyms []string `json:"synonyms"`
	Antonyms []string `json:"antonyms,omitempty"`
}

// Parse reads the JSON dictionary format {"word": {"tag": "n", "sinonim": [...], "antonim": [...]}}.
// Values are trimmed, the "ki " (kiasan, figurative) marker is dropped and duplicates removed.
func Parse(data []byte) ([]Entry, error) {
	var raw map[string]struct {
		Tag     string   `json:"tag"`
		Sinonim []string `json:"sinonim"`
		Antonim []string `json:"antonim"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("empty thesaurus")
	}
	// headwords that differ only in case or spacing are merged
	merged := map[string]*Entry{}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic merge order
	for _, k := range keys {
		v := raw[k]
		w := Normalize(k)
		if w == "" {
			continue
		}
		e := merged[w]
		if e == nil {
			e = &Entry{Word: w, Tag: strings.TrimSpace(v.Tag)}
			merged[w] = e
		}
		e.Synonyms = clean(append(e.Synonyms, v.Sinonim...), w)
		e.Antonyms = clean(append(e.Antonyms, v.Antonim...), w)
	}
	out := make([]Entry, 0, len(merged))
	for _, e := range merged {
		if len(e.Synonyms) > 0 || len(e.Antonyms) > 0 {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Word < out[j].Word })
	return out, nil
}

func clean(list []string, self string) []string {
	seen := map[string]bool{self: true}
	var out []string
	for _, s := range list {
		s = strings.Join(strings.Fields(s), " ")
		s = strings.TrimPrefix(s, "ki ")
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

// Normalize is the lookup key of a word or short phrase
func Normalize(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.Trim(s, " .,;:!?\"'“”‘’()[]…")
}

// Index is an in-memory thesaurus safe for concurrent use
type Index struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// Set replaces the contents of the index
func (x *Index) Set(entries []Entry) {
	m := make(map[string]*Entry, len(entries))
	for i := range entries {
		m[entries[i].Word] = &entries[i]
	}
	x.mu.Lock()
	x.entries = m
	x.mu.Unlock()
}

// Len is the number of headwords
func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.entries)
}

// Lookup finds a word or phrase (case-insensitive, surrounding punctuation ignored)
func (x *Index) Lookup(word string) (*Entry, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	e, ok := x.entries[Normalize(word)]
	return e, ok
}

// LookupForms is Lookup with a fallback for verb forms: the thesaurus lists verbs with the
// meN- prefix ("menampilkan") while interface strings often use the bare or di- form
// ("Tampilkan", "ditampilkan"). It returns the entry found, if any.
func (x *Index) LookupForms(word string) (*Entry, bool) {
	if e, ok := x.Lookup(word); ok {
		return e, true
	}
	w := Normalize(word)
	if strings.Contains(w, " ") {
		return nil, false
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, v := range Variants(w) {
		if e, ok := x.entries[v]; ok {
			return e, true
		}
	}
	return nil, false
}

// Variants returns other dictionary forms to try for a single word: without "-nya",
// without the passive "di-", and with the active "meN-" prefix (following the sound rules:
// t→men, p→mem, k→meng, s→meny; vowels and g/h take meng-; b/f/v mem-; c/d/j/z men-).
func Variants(w string) []string {
	var bases []string
	add := func(s string) {
		if len([]rune(s)) >= 3 {
			bases = append(bases, s)
		}
	}
	add(w)
	if b, ok := strings.CutSuffix(w, "nya"); ok {
		add(b)
	}
	for _, b := range append([]string(nil), bases...) {
		if r, ok := strings.CutPrefix(b, "di"); ok {
			add(r)
		}
	}
	seen := map[string]bool{w: true}
	var out []string
	push := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, b := range bases {
		push(b)
		if strings.HasPrefix(b, "me") || strings.HasPrefix(b, "ber") {
			continue
		}
		for _, m := range meN(b) {
			push(m)
		}
	}
	return out
}

func meN(b string) []string {
	first := b[:1]
	switch {
	case strings.ContainsAny(first, "aiueo"):
		return []string{"meng" + b}
	case strings.HasPrefix(b, "sy"):
		return []string{"men" + b}
	case first == "t":
		return []string{"men" + b[1:], "men" + b}
	case first == "p":
		return []string{"mem" + b[1:], "mem" + b, "memper" + b[1:]}
	case first == "k":
		return []string{"meng" + b[1:], "meng" + b}
	case first == "s":
		return []string{"meny" + b[1:], "men" + b}
	case strings.ContainsAny(first, "bfv"):
		return []string{"mem" + b}
	case strings.ContainsAny(first, "cdjz"):
		return []string{"men" + b}
	case strings.ContainsAny(first, "gh"):
		return []string{"meng" + b}
	case strings.ContainsAny(first, "lmnrwy"):
		return []string{"me" + b}
	}
	return []string{"me" + b, "menge" + b}
}
