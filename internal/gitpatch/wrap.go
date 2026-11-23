package gitpatch

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// width is the line width gettext tools wrap to (msgcat/msgmerge default)
const width = 79

// writeString writes `keyword "value"` like gettext: on one line when it fits and has no inner
// line break, else an empty first string followed by wrapped pieces, split after each "\n" and at
// line-break opportunities so every line (quotes included) stays within 79 columns when possible
func writeString(b *bytes.Buffer, keyword, value string) {
	esc := escape(value)
	inner := strings.TrimSuffix(value, "\n")
	if !strings.Contains(inner, "\n") && columns(keyword)+1+columns(esc)+2 <= width {
		b.WriteString(keyword + ` "` + esc + "\"\n")
		return
	}
	b.WriteString(keyword + " \"\"\n")
	for _, piece := range wrap(esc, width-2) {
		b.WriteString(`"` + piece + "\"\n")
	}
}

// escape writes a string the way gettext does inside quotes
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// columns is the display width of a string (one per character; good enough for PO text)
func columns(s string) int { return utf8.RuneCountInString(s) }

// wrap splits an escaped string after every "\n" and then into pieces of at most max columns,
// breaking at the last opportunity that fits; a piece without any opportunity stays long
func wrap(esc string, max int) []string {
	var parts []string
	rs := []rune(esc)
	start := 0
	for i := 0; i+1 < len(rs); i++ {
		if rs[i] == '\\' {
			if rs[i+1] == 'n' && i+2 < len(rs) {
				parts = append(parts, string(rs[start:i+2]))
				start = i + 2
			}
			i++ // skip the escaped character
		}
	}
	parts = append(parts, string(rs[start:]))

	var out []string
	for _, p := range parts {
		r := []rune(p)
		for len(r) > max {
			cut := -1
			for i := 1; i <= max && i < len(r); i++ {
				if breakAfter(r, i) {
					cut = i
				}
			}
			if cut <= 0 { // nothing fits: break at the first opportunity after the width
				for i := max + 1; i < len(r); i++ {
					if breakAfter(r, i) {
						cut = i
						break
					}
				}
			}
			if cut <= 0 {
				break // nowhere to break: keep the long piece
			}
			out = append(out, string(r[:cut]))
			r = r[cut:]
		}
		if len(r) > 0 || len(out) == 0 {
			out = append(out, string(r))
		}
	}
	return out
}

// breakAfter reports whether a line may break between r[i-1] and r[i], following the Unicode
// line-breaking rules gettext uses closely enough for documentation text: after spaces (never
// before closing punctuation or more spaces), after "/" and after a hyphen between letters
func breakAfter(r []rune, i int) bool {
	prev, next := r[i-1], r[i]
	if i >= 2 && r[i-2] == '\\' && !(i >= 3 && r[i-3] == '\\') {
		return false // inside an escape such as \"
	}
	switch {
	case prev == ' ':
		if next == ' ' || strings.ContainsRune("!%),.:;?]}/", next) {
			return false
		}
		return true
	case prev == '/':
		return (unicode.IsLetter(next) || unicode.IsDigit(next)) && i >= 2 && r[i-2] != '/' && r[i-2] != ':'
	case prev == '!':
		return unicode.IsLetter(next)
	case prev == '-':
		return unicode.IsLetter(next) && i >= 2 && r[i-2] != '-' && r[i-2] != ' ' && r[i-2] != '`' && !unicode.IsSpace(r[i-2])
	}
	return false
}
