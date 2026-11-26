package gitpatch

import (
	"strings"

	"pelokalan/internal/po"
)

// Credit is the translator a change credits in the file header: their "Name <email>, year." line in the header
// comments (an existing line gains the year, po.AddContributor) and the Last-Translator field
type Credit struct {
	Name, Email string
	Year        int
}

// identity is the Last-Translator value
func (c *Credit) identity() string { return c.Name + " <" + c.Email + ">" }

// headerRange finds the header entry (msgid "") with the comments above it: [start, end) in the file's lines
func headerRange(lines []string) (int, int, bool) {
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			if start >= 0 {
				return start, i, isHeader(lines[start:i])
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		return start, len(lines), isHeader(lines[start:])
	}
	return 0, 0, false
}

func isHeader(block []string) bool {
	cat, err := po.ParseBytes([]byte(strings.Join(block, "\n") + "\n"))
	if err != nil {
		return false
	}
	if len(cat.Entries) == 0 { // the parser keeps the header apart from the entries
		return cat.Header != "" || len(cat.HeaderComments) > 0
	}
	return false
}

// creditHeader returns the header block's new lines: the credit line among the translator comments and the
// Last-Translator value; everything else stays as written
func creditHeader(block []string, c *Credit) []string {
	// translator comments ("# …" or a bare "#") at the top of the block
	n := 0
	for n < len(block) && isTranslatorComment(block[n]) {
		n++
	}
	comments := make([]string, n)
	for i, l := range block[:n] {
		comments[i] = strings.TrimPrefix(strings.TrimPrefix(l, "#"), " ")
	}
	comments = po.AddContributor(comments, c.Name, c.Email, c.Year)
	out := make([]string, 0, len(block)+1)
	for _, t := range comments {
		if t == "" {
			out = append(out, "#")
		} else {
			out = append(out, "# "+t)
		}
	}
	for _, l := range block[n:] {
		if strings.HasPrefix(strings.TrimSpace(l), `"Last-Translator:`) {
			l = `"Last-Translator: ` + escape(c.identity()) + `\n"`
		}
		out = append(out, l)
	}
	return out
}

func isTranslatorComment(l string) bool {
	return l == "#" || strings.HasPrefix(l, "# ") || (strings.HasPrefix(l, "#") && len(l) > 1 && !strings.ContainsRune(",.:|~", rune(l[1])))
}

// headerChange is the header rewrite as a change (nil when the header already credits them)
func headerChange(lines []string, c *Credit) *change {
	if c == nil || c.Email == "" {
		return nil
	}
	start, end, ok := headerRange(lines)
	if !ok {
		return nil
	}
	nl := creditHeader(lines[start:end], c)
	if sameStrings(nl, lines[start:end]) {
		return nil
	}
	return &change{start: start, end: end, lines: nl}
}
