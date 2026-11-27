// Package spelling checks Indonesian text against a word list taken from KBBI (Kamus Besar
// Bahasa Indonesia, CSV extract). Only the words are kept, never meanings or examples. KBBI lists
// root words with many derived forms; the forms it leaves out (passive di-, imperative -kan/-i,
// -nya and other clitics, doubled words) are recognised by affix rules.
package spelling

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Parse reads the KBBI CSV (columns kata, turunan, varian, gabungan_kata, …) and returns its
// single words, lowercase and sorted. Multi-word entries are skipped; hyphenated words are kept.
func Parse(data []byte) ([]string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF"))] = i
	}
	if _, ok := col["kata"]; !ok {
		return nil, errors.New("not a KBBI CSV: no kata column")
	}
	set := map[string]bool{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"kata", "turunan", "varian", "gabungan_kata"} {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				continue
			}
			for _, part := range strings.Split(rec[i], "|") {
				if w := strings.ToLower(strings.TrimSpace(part)); validWord(w) {
					set[w] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for w := range set {
		out = append(out, w)
	}
	sort.Strings(out)
	return out, nil
}

// validWord accepts a dictionary word: letters (and a hyphen inside), 2–40 characters
func validWord(w string) bool {
	n := utf8.RuneCountInString(w)
	if n < 2 || n > 40 || strings.HasPrefix(w, "-") || strings.HasSuffix(w, "-") {
		return false
	}
	for _, r := range w {
		if r != '-' && !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// Dict is the word list in memory; safe for concurrent use
type Dict struct {
	mu    sync.RWMutex
	words map[string]bool
	list  []string // for suggestions
}

// Set replaces the words
func (d *Dict) Set(words []string) {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	d.mu.Lock()
	d.words, d.list = m, words
	d.mu.Unlock()
}

// Len is the number of words
func (d *Dict) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.words)
}

func (d *Dict) has(w string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.words[w]
}

// Known reports whether a word (any case) is Indonesian according to the list, with its affixes
// taken into account; extra holds more accepted words (a project's own dictionary), may be nil
func (d *Dict) Known(word string, extra map[string]bool) bool {
	w := strings.ToLower(word)
	in := func(s string) bool { return d.has(s) || extra[s] }
	if in(w) {
		return true
	}
	// doubled and hyphenated words: every part known ("kata-kata", "sehari-hari")
	if strings.Contains(w, "-") {
		parts := strings.Split(w, "-")
		for i, p := range parts {
			if p == "" {
				return false
			}
			if !in(p) && !d.knownForm(p, in) && !(i > 0 && strings.HasPrefix(parts[0], p)) {
				return false
			}
		}
		return true
	}
	return d.knownForm(w, in)
}

// knownForm tries the forms KBBI does not list
func (d *Dict) knownForm(w string, in func(string) bool) bool {
	if in(w) {
		return true
	}
	// clitics: berkasnya, bukalah, apakah, itupun, bukuku, rumahmu; kubuka, kaubaca
	for _, suf := range []string{"nya", "lah", "kah", "tah", "pun", "ku", "mu"} {
		if b, ok := strings.CutSuffix(w, suf); ok && len(b) >= 3 && (in(b) || d.verbForm(b, in)) {
			return true
		}
	}
	for _, pre := range []string{"ku", "kau"} {
		if b, ok := strings.CutPrefix(w, pre); ok && len(b) >= 3 && (in(b) || d.verbForm(b, in)) {
			return true
		}
	}
	return d.verbForm(w, in)
}

// verbForm: the passive di- and the bare imperative of verbs KBBI lists with meN-
// ("ditampilkan", "tampilkan" ← "menampilkan"), and -kan/-i/-an on a known root
func (d *Dict) verbForm(w string, in func(string) bool) bool {
	bases := []string{w}
	if b, ok := strings.CutPrefix(w, "di"); ok && len(b) >= 3 {
		if in(b) {
			return true
		}
		bases = append(bases, b)
	}
	for _, b := range bases {
		for _, m := range meN(b) {
			if in(m) {
				return true
			}
		}
		for _, suf := range []string{"kan", "i", "an"} {
			if root, ok := strings.CutSuffix(b, suf); ok && len(root) >= 3 && in(root) {
				return true
			}
		}
	}
	return false
}

// meN returns the active forms of a root following the sound rules (t→men, p→mem, k→meng,
// s→meny; vowels, g, h take meng-; b, f, v mem-; c, d, j, z men-; l, m, n, r, w, y me-)
func meN(b string) []string {
	if b == "" {
		return nil
	}
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

// Words returns the words of a text worth checking: letters (with inner hyphens or
// apostrophes), skipping placeholders (%s, %1$d, {name}, ${x}), markup tags (<gui>), entities
// (&amp;), addresses (http…, a@b), keyboard accelerators (_File), words with digits, acronyms
// (two or more capitals) and words with inner capitals (GNOME, macOS, iPhone)
func Words(text string) []string {
	var out []string
	rs := []rune(text)
	n := len(rs)
	for i := 0; i < n; {
		r := rs[i]
		if url := string(rs[i:min(n, i+8)]); strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") ||
			strings.HasPrefix(url, "www.") {
			for i < n && !unicode.IsSpace(rs[i]) { // the whole address
				i++
			}
			continue
		}
		switch {
		case r == '%': // printf placeholder
			i++
			for i < n && (unicode.IsDigit(rs[i]) || strings.ContainsRune("$#-+ .lhzjtLqI'", rs[i])) {
				i++
			}
			if i < n && unicode.IsLetter(rs[i]) {
				i++
			}
			continue
		case r == '{' || r == '<': // {name}, ${x}, <tag attr="…">
			end := map[rune]rune{'{': '}', '<': '>'}[r]
			j := i + 1
			for j < n && rs[j] != end && j-i < 200 {
				j++
			}
			if j < n && rs[j] == end {
				i = j + 1
				continue
			}
			i++
			continue
		case r == '&': // &amp;
			j := i + 1
			for j < n && j-i < 12 && (unicode.IsLetter(rs[j]) || rs[j] == '#' || unicode.IsDigit(rs[j])) {
				j++
			}
			if j < n && rs[j] == ';' {
				i = j + 1
				continue
			}
			i++
			continue
		case !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_':
			i++
			continue
		}
		// a run of letters, digits, _ and inner - ' . @ / : (addresses and paths stay one token)
		j := i
		for j < n && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_' ||
			(strings.ContainsRune("-'’.@/:", rs[j]) && j+1 < n && (unicode.IsLetter(rs[j+1]) || unicode.IsDigit(rs[j+1])))) {
			j++
		}
		tok := string(rs[i:j])
		i = j
		if t, ok := strings.CutPrefix(tok, "_"); ok { // mnemonic: _Berkas
			tok = t
		}
		if checkable(tok) {
			out = append(out, tok)
		}
	}
	return out
}

func checkable(tok string) bool {
	if utf8.RuneCountInString(tok) < 2 || strings.ContainsAny(tok, "_.@/:'’") {
		return false
	}
	upper := 0
	for i, r := range tok {
		if unicode.IsDigit(r) {
			return false
		}
		if unicode.IsUpper(r) {
			upper++
			if i > 0 && upper >= 1 && !strings.ContainsRune(tok[:i], '-') {
				return false // inner capital: GNOME, macOS, iPhone, LibreOffice
			}
		}
	}
	return true
}

// Misspelled returns the words of text the dictionary does not know (lowercase, each once, in
// order). Words that also appear in source are left alone: names and terms kept as they are.
func (d *Dict) Misspelled(text, source string, extra map[string]bool) []string {
	if d.Len() == 0 {
		return nil
	}
	skip := map[string]bool{}
	for _, w := range Words(source) {
		skip[strings.ToLower(w)] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, w := range Words(text) {
		lw := strings.ToLower(w)
		if seen[lw] || skip[lw] {
			continue
		}
		seen[lw] = true
		if !d.Known(lw, extra) {
			out = append(out, lw)
		}
	}
	return out
}

// Suggest returns up to max dictionary words close to word (edit distance 1, then 2; letters
// swapped count as one edit), closest and most similar first
func (d *Dict) Suggest(word string, max int) []string {
	w := []rune(strings.ToLower(word))
	if len(w) < 2 {
		return nil
	}
	d.mu.RLock()
	list := d.list
	d.mu.RUnlock()
	type cand struct {
		w      string
		dist   int
		prefix int // letters in common at the start
	}
	var found []cand
	for _, s := range list {
		rs := []rune(s)
		if diff := len(rs) - len(w); diff > 2 || diff < -2 || strings.Contains(s, "-") {
			continue
		}
		if dist := editDistance(w, rs, 2); dist <= 2 {
			p := 0
			for p < len(rs) && p < len(w) && rs[p] == w[p] {
				p++
			}
			found = append(found, cand{s, dist, p})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if a.prefix != b.prefix {
			return a.prefix > b.prefix
		}
		return a.w < b.w
	})
	var out []string
	for _, c := range found {
		if len(out) == max {
			break
		}
		out = append(out, c.w)
	}
	return out
}

// editDistance is the optimal string alignment distance (insert, delete, change, swap of two
// neighbours), giving up above limit
func editDistance(a, b []rune, limit int) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			v := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				v = min(v, prev2[j-2]+1)
			}
			cur[j] = v
			best = min(best, v)
		}
		if best > limit {
			return limit + 1
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}
