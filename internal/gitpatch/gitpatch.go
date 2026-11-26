// Package gitpatch turns translations edited in Pelokalan into a patch for a git repository of PO
// files. It starts from the repository's own file text and rewrites only the entries whose
// translation or fuzzy flag changed, wrapped the way gettext tools wrap them, so the diff a reviewer
// sees holds the real changes and nothing else (no header, comment or re-wrapping noise). The result
// is in `git format-patch` form, ready for `git am`.
package gitpatch

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/po"
)

// Target is the wanted state of one entry (matched by context + source text)
type Target struct {
	Translation []string
	Fuzzy       bool
}

// change replaces old lines [start, end) with lines
type change struct {
	start, end int
	lines      []string
}

// block is one entry of the base file: its line range and parsed content
type block struct {
	start, end int // [start, end) in the file's lines
	entry      *po.Entry
}

// splitLines splits text into lines without their "\n"; ended reports a final newline
func splitLines(text []byte) (lines []string, ended bool) {
	s := string(text)
	ended = strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil, ended
	}
	return strings.Split(s, "\n"), ended
}

// blocks finds the entries of a PO file (separated by blank lines), skipping the header and
// obsolete entries
func blocks(lines []string) []block {
	var out []block
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		text := strings.Join(lines[start:end], "\n") + "\n"
		if cat, err := po.ParseBytes([]byte(text)); err == nil && len(cat.Entries) == 1 {
			if e := cat.Entries[0]; !e.Obsolete && !(e.ID == "" && !e.HasContext) {
				out = append(out, block{start: start, end: end, entry: e})
			}
		}
		start = -1
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(lines))
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rewrite returns the new lines of a block for the wanted translation and fuzzy flag: comments,
// references, context and source stay as they are; the flags line gains or loses "fuzzy" (and
// "#|" previous-source lines go when the entry is no longer fuzzy); the msgstr lines are
// written anew
func rewrite(old []string, e *po.Entry, t Target) []string {
	var out []string
	flagsDone := false
	msgstrAt := -1
	for i, l := range old {
		if strings.HasPrefix(l, "msgstr") {
			msgstrAt = i
			break
		}
		switch {
		case strings.HasPrefix(l, "#,"):
			flagsDone = true
			if fl := setFuzzy(l, t.Fuzzy); fl != "" {
				out = append(out, fl)
			}
			continue
		case strings.HasPrefix(l, "#|") && !t.Fuzzy:
			continue
		case !flagsDone && t.Fuzzy && (strings.HasPrefix(l, "#|") || strings.HasPrefix(l, "msgctxt") || strings.HasPrefix(l, "msgid")):
			out = append(out, "#, fuzzy") // no flags line yet: it goes before #| and the strings
			flagsDone = true
		}
		out = append(out, l)
	}
	if msgstrAt < 0 {
		return old
	}
	var b bytes.Buffer
	if e.IsPlural() {
		for i, s := range t.Translation {
			writeString(&b, fmt.Sprintf("msgstr[%d]", i), s)
		}
	} else {
		s := ""
		if len(t.Translation) > 0 {
			s = t.Translation[0]
		}
		writeString(&b, "msgstr", s)
	}
	return append(out, strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")...)
}

// setFuzzy adds or removes "fuzzy" in a "#, a, b" flags line ("" when no flag is left)
func setFuzzy(line string, fuzzy bool) string {
	var flags []string
	for _, f := range strings.Split(strings.TrimPrefix(line, "#,"), ",") {
		if f = strings.TrimSpace(f); f != "" && f != "fuzzy" {
			flags = append(flags, f)
		}
	}
	if fuzzy {
		flags = append([]string{"fuzzy"}, flags...)
	}
	if len(flags) == 0 {
		return ""
	}
	return "#, " + strings.Join(flags, ", ")
}

// FileDiff is the patch of one file
type FileDiff struct {
	Path    string // in the repository, e.g. "library/os.po"
	Hunks   string // unified diff hunks ("@@ … @@" and lines)
	Changed int    // entries changed
	Added   int    // lines added
	Removed int    // lines removed
}

// Diff compares a repository file with the wanted entries (key = po.Entry.Key()) and returns the
// patch of the entries that differ; nil when nothing changes. Entries the file does not have are
// ignored: the repository decides which strings exist.
func Diff(path string, base []byte, want map[string]Target) *FileDiff {
	return DiffCredit(path, base, want, nil)
}

// DiffCredit is Diff that also credits the translator in the header (Credit) when any entry changes
func DiffCredit(path string, base []byte, want map[string]Target, credit *Credit) *FileDiff {
	lines, ended := splitLines(base)
	var changes []change
	entries := 0
	for _, b := range blocks(lines) {
		t, ok := want[b.entry.Key()]
		if !ok {
			continue
		}
		if sameStrings(normalize(t.Translation), normalize(b.entry.Translation)) && t.Fuzzy == b.entry.Fuzzy {
			continue
		}
		nl := rewrite(lines[b.start:b.end], b.entry, t)
		if sameStrings(nl, lines[b.start:b.end]) {
			continue
		}
		// only the lines that differ: unchanged comments and source lines stay context
		changes = append(changes, lineChanges(lines[b.start:b.end], nl, b.start)...)
		entries++
	}
	if len(changes) == 0 {
		return nil
	}
	if hc := headerChange(lines, credit); hc != nil { // the header comes first in the file
		changes = append(lineChanges(lines[hc.start:hc.end], hc.lines, hc.start), changes...)
	}
	d := &FileDiff{Path: path, Changed: entries}
	d.Hunks, d.Added, d.Removed = hunks(lines, ended, changes)
	return d
}

// Apply rewrites a repository file like Diff and returns the whole new file and how many entries
// changed (0 = the file already has every wanted translation; data is then base itself)
func Apply(base []byte, want map[string]Target) ([]byte, int) {
	return ApplyCredit(base, want, nil)
}

// ApplyCredit is Apply that also credits the translator in the header (Credit) when any entry changes
func ApplyCredit(base []byte, want map[string]Target, credit *Credit) ([]byte, int) {
	lines, ended := splitLines(base)
	var out []string
	last, entries := 0, 0
	for _, b := range blocks(lines) {
		t, ok := want[b.entry.Key()]
		if !ok || sameStrings(normalize(t.Translation), normalize(b.entry.Translation)) && t.Fuzzy == b.entry.Fuzzy {
			continue
		}
		nl := rewrite(lines[b.start:b.end], b.entry, t)
		if sameStrings(nl, lines[b.start:b.end]) {
			continue
		}
		out = append(append(out, lines[last:b.start]...), nl...)
		last = b.end
		entries++
	}
	if entries == 0 {
		return base, 0
	}
	out = append(out, lines[last:]...)
	if hc := headerChange(lines, credit); hc != nil { // the header block precedes every entry: same line numbers in out
		out = append(append(append([]string(nil), out[:hc.start]...), hc.lines...), out[hc.end:]...)
	}
	text := strings.Join(out, "\n")
	if ended {
		text += "\n"
	}
	return []byte(text), entries
}

// lineChanges diffs two short line lists (one entry) with a longest common subsequence and
// returns the differing runs as changes on the old lines (offset = the block's first line)
func lineChanges(old, new []string, offset int) []change {
	n, m := len(old), len(new)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if old[i] == new[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []change
	var cur *change
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && old[i] == new[j]:
			flush()
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			if cur == nil {
				cur = &change{start: offset + i, end: offset + i}
			}
			cur.lines = append(cur.lines, new[j])
			j++
		default:
			if cur == nil {
				cur = &change{start: offset + i, end: offset + i}
			}
			cur.end = offset + i + 1
			i++
		}
	}
	flush()
	return out
}

// normalize treats a missing translation like an empty one
func normalize(s []string) []string {
	if len(s) == 0 {
		return []string{""}
	}
	return s
}

const contextLines = 3

// hunks writes unified diff hunks for the changes (sorted, not overlapping)
func hunks(old []string, ended bool, changes []change) (string, int, int) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].start < changes[j].start })
	var out strings.Builder
	added, removed := 0, 0
	offset := 0 // new line number minus old line number before the current hunk
	for i := 0; i < len(changes); {
		// a hunk takes every change whose context touches the previous one
		j := i
		for j+1 < len(changes) && changes[j+1].start-changes[j].end <= 2*contextLines {
			j++
		}
		from := max(0, changes[i].start-contextLines)
		to := min(len(old), changes[j].end+contextLines)
		var body []string
		oldCount, newCount := 0, 0
		pos := from
		for k := i; k <= j; k++ {
			c := changes[k]
			for ; pos < c.start; pos++ {
				body = append(body, " "+old[pos])
				oldCount++
				newCount++
			}
			for _, l := range old[c.start:c.end] {
				body = append(body, "-"+l)
				oldCount++
				removed++
			}
			for _, l := range c.lines {
				body = append(body, "+"+l)
				newCount++
				added++
			}
			pos = c.end
		}
		for ; pos < to; pos++ {
			body = append(body, " "+old[pos])
			oldCount++
			newCount++
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", rangeOf(from, oldCount), rangeOf(from+offset, newCount))
		for _, l := range body {
			out.WriteString(l + "\n")
		}
		if !ended && to == len(old) {
			out.WriteString("\\ No newline at end of file\n")
		}
		for k := i; k <= j; k++ {
			offset += len(changes[k].lines) - (changes[k].end - changes[k].start)
		}
		i = j + 1
	}
	return out.String(), added, removed
}

// rangeOf is a hunk range "start,count" (1-based; an empty range names the line before it)
func rangeOf(start, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

// Commit describes the commit a patch becomes
type Commit struct {
	AuthorName, AuthorEmail string
	Date                    time.Time
	Subject                 string
	Body                    string
}

// FormatPatch writes one commit with the file diffs in `git format-patch` form (for `git am`)
func FormatPatch(c Commit, files []*FileDiff) []byte {
	var b strings.Builder
	b.WriteString("From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001\n")
	fmt.Fprintf(&b, "From: %s <%s>\n", headerSafe(c.AuthorName), headerSafe(c.AuthorEmail))
	fmt.Fprintf(&b, "Date: %s\n", c.Date.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Subject: [PATCH] %s\n", headerSafe(c.Subject))
	b.WriteString("MIME-Version: 1.0\nContent-Type: text/plain; charset=UTF-8\nContent-Transfer-Encoding: 8bit\n\n")
	if body := strings.TrimSpace(c.Body); body != "" {
		b.WriteString(body + "\n")
	}
	b.WriteString("---\n")
	added, removed := 0, 0
	for _, f := range files {
		fmt.Fprintf(&b, " %s | %d %s\n", f.Path, f.Added+f.Removed, strings.Repeat("+", min(f.Added, 30))+strings.Repeat("-", min(f.Removed, 30)))
		added += f.Added
		removed += f.Removed
	}
	fmt.Fprintf(&b, " %d file%s changed, %d insertion%s(+), %d deletion%s(-)\n\n",
		len(files), plural(len(files)), added, plural(added), removed, plural(removed))
	for _, f := range files {
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", f.Path, f.Path, f.Path, f.Path)
		b.WriteString(f.Hunks)
	}
	b.WriteString("-- \nPelokalan\n")
	return []byte(b.String())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// headerSafe keeps a value on one header line
func headerSafe(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
