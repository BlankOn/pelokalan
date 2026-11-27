package web

import (
	"html"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"pelokalan/internal/models"
)

// anchorMarks shows a translation (its first form) with the passages open review comments are about marked
// (<mark class="rv-anchor">, pink with a red underline). A passage is looked for where it was selected; when the
// text moved, the nearest occurrence is marked, and a passage no longer in the text is not marked at all.
// Overlapping passages keep the first one. Everything is escaped.
func anchorMarks(text string, anchors []models.CommentAnchor) template.HTML {
	type span struct {
		start, end int // byte offsets
		a          models.CommentAnchor
	}
	var spans []span
	for _, a := range anchors {
		if a.Form != 0 || a.Text == "" {
			continue
		}
		best, bestDist := -1, 0
		for off := 0; off <= len(text); {
			i := strings.Index(text[off:], a.Text)
			if i < 0 {
				break
			}
			at := off + i
			d := utf8.RuneCountInString(text[:at]) - a.Start
			if d < 0 {
				d = -d
			}
			if best < 0 || d < bestDist {
				best, bestDist = at, d
			}
			off = at + 1
		}
		if best >= 0 {
			spans = append(spans, span{best, best + len(a.Text), a})
		}
	}
	if len(spans) == 0 {
		return template.HTML(html.EscapeString(text))
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if s.start < last {
			continue // overlaps the previous mark
		}
		b.WriteString(html.EscapeString(text[last:s.start]))
		title := s.a.Author
		if s.a.Body != "" {
			title += ": " + s.a.Body
		}
		if s.a.Resolved {
			b.WriteString(`<mark class="rv-anchor rv-done" data-comment="`)
		} else {
			b.WriteString(`<mark class="rv-anchor" data-comment="`)
		}
		b.WriteString(strconv.FormatInt(s.a.CommentID, 10))
		b.WriteString(`" title="`)
		b.WriteString(html.EscapeString(title))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(text[s.start:s.end]))
		b.WriteString(`</mark>`)
		last = s.end
	}
	b.WriteString(html.EscapeString(text[last:]))
	return template.HTML(b.String())
}
