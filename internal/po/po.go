// Package po reads and writes gettext PO/POT catalogs and compiles MO files.
package po

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Entry is a single message of a catalog
type Entry struct {
	TranslatorComments []string
	ExtractedComments  []string
	References         []string
	Flags              []string // without "fuzzy"
	Fuzzy              bool
	Previous           []string // raw "#|" lines without the prefix

	HasContext  bool
	Context     string
	ID          string
	IDPlural    string
	Translation []string // one item, or nplurals items for plural entries

	Obsolete bool
}

// IsPlural reports whether the entry has plural forms
func (e *Entry) IsPlural() bool { return e.IDPlural != "" }

// IsTranslated reports whether every form has a translation
func (e *Entry) IsTranslated() bool {
	if len(e.Translation) == 0 {
		return false
	}
	for _, s := range e.Translation {
		if s == "" {
			return false
		}
	}
	return true
}

// Key identifies an entry inside a catalog (context + id)
func (e *Entry) Key() string {
	if e.HasContext {
		return e.Context + "\x04" + e.ID
	}
	return e.ID
}

// HasFlag reports whether the entry carries the given flag
func (e *Entry) HasFlag(flag string) bool {
	for _, f := range e.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Catalog is a parsed PO or POT file
type Catalog struct {
	HeaderComments []string // translator comments above the header entry
	Header         string   // msgstr of the header entry (raw, with \n separators)
	HeaderFuzzy    bool
	Entries        []*Entry
}

// HeaderField returns the value of a header field such as "Language"
func (c *Catalog) HeaderField(name string) string {
	for _, line := range strings.Split(c.Header, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// SetHeaderField sets (or appends) a header field
func SetHeaderField(header, name, value string) string {
	lines := strings.Split(strings.TrimRight(header, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	found := false
	for i, line := range lines {
		k, _, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			lines[i] = name + ": " + value
			found = true
		}
	}
	if !found {
		lines = append(lines, name+": "+value)
	}
	return strings.Join(lines, "\n") + "\n"
}

// GetHeaderField returns a header field value from a raw header string
func GetHeaderField(header, name string) string {
	return (&Catalog{Header: header}).HeaderField(name)
}

var npluralsRe = regexp.MustCompile(`nplurals\s*=\s*(\d+)`)

// NPlurals extracts the number of plural forms from a Plural-Forms value (default 2)
func NPlurals(pluralForms string) int {
	m := npluralsRe.FindStringSubmatch(pluralForms)
	if m == nil {
		return 2
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 || n > 10 {
		return 2
	}
	return n
}

// ParseError describes a syntax error in a PO file
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Parse reads a PO or POT file
func Parse(r io.Reader) (*Catalog, error) {
	p := &parser{cat: &Catalog{}}
	return p.parse(r)
}

// ParseBytes reads a PO or POT file from memory
func ParseBytes(b []byte) (*Catalog, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	return Parse(bytes.NewReader(b))
}

type parser struct {
	cat       *Catalog
	cur       *Entry
	sawID     bool
	sawStr    bool
	field     string // msgctxt, msgid, msgid_plural, msgstr
	strIdx    int
	lineNo    int
	hasHeader bool
}

func (p *parser) reset() {
	p.cur = &Entry{}
	p.sawID, p.sawStr, p.field, p.strIdx = false, false, "", 0
}

func (p *parser) flush() {
	if !p.sawID {
		return
	}
	e := p.cur
	if !e.HasContext && e.ID == "" && !e.Obsolete && !p.hasHeader {
		p.hasHeader = true
		p.cat.HeaderComments = e.TranslatorComments
		if len(e.Translation) > 0 {
			p.cat.Header = e.Translation[0]
		}
		p.cat.HeaderFuzzy = e.Fuzzy
	} else {
		if len(e.Translation) == 0 {
			e.Translation = []string{""}
		}
		p.cat.Entries = append(p.cat.Entries, e)
	}
	p.reset()
}

func (p *parser) parse(r io.Reader) (*Catalog, error) {
	p.reset()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		p.lineNo++
		if err := p.line(sc.Text()); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	p.flush()
	return p.cat, nil
}

func (p *parser) errf(format string, args ...any) error {
	return &ParseError{Line: p.lineNo, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) line(raw string) error {
	t := strings.TrimSpace(raw)
	if t == "" {
		p.flush()
		return nil
	}

	obsolete := false
	if strings.HasPrefix(t, "#~") {
		obsolete = true
		t = strings.TrimSpace(t[2:])
		if t == "" {
			return nil
		}
		if strings.HasPrefix(t, "|") { // previous strings of an obsolete entry
			if p.sawID {
				p.flush()
			}
			p.cur.Previous = append(p.cur.Previous, strings.TrimSpace(t[1:]))
			return nil
		}
	}

	if !obsolete && strings.HasPrefix(t, "#") {
		if p.sawID {
			p.flush()
		}
		e := p.cur
		switch {
		case strings.HasPrefix(t, "#."):
			e.ExtractedComments = append(e.ExtractedComments, strings.TrimSpace(t[2:]))
		case strings.HasPrefix(t, "#:"):
			if ref := strings.TrimSpace(t[2:]); ref != "" {
				e.References = append(e.References, ref)
			}
		case strings.HasPrefix(t, "#,"):
			for _, f := range strings.Split(t[2:], ",") {
				f = strings.TrimSpace(f)
				switch f {
				case "":
				case "fuzzy":
					e.Fuzzy = true
				default:
					e.Flags = append(e.Flags, f)
				}
			}
		case strings.HasPrefix(t, "#|"):
			e.Previous = append(e.Previous, strings.TrimSpace(t[2:]))
		default:
			c := strings.TrimPrefix(t, "#")
			c = strings.TrimPrefix(c, " ")
			e.TranslatorComments = append(e.TranslatorComments, c)
		}
		return nil
	}

	if strings.HasPrefix(t, `"`) {
		s, err := unquote(t)
		if err != nil {
			return p.errf("%v", err)
		}
		return p.appendString(s)
	}

	kw, rest, _ := strings.Cut(t, " ")
	rest = strings.TrimSpace(rest)
	val, err := unquote(rest)
	if err != nil {
		return p.errf("%s: %v", kw, err)
	}

	switch {
	case kw == "msgctxt":
		if p.sawStr || p.sawID {
			p.flush()
		}
		p.cur.HasContext = true
		p.cur.Context = val
		p.field = "msgctxt"
	case kw == "msgid":
		if p.sawStr || p.sawID {
			p.flush()
		}
		p.sawID = true
		p.cur.ID = val
		p.field = "msgid"
	case kw == "msgid_plural":
		if !p.sawID {
			return p.errf("msgid_plural without msgid")
		}
		p.cur.IDPlural = val
		p.field = "msgid_plural"
	case kw == "msgstr":
		if !p.sawID {
			return p.errf("msgstr without msgid")
		}
		p.sawStr = true
		p.cur.Translation = []string{val}
		p.field, p.strIdx = "msgstr", 0
	case strings.HasPrefix(kw, "msgstr["):
		if !p.sawID {
			return p.errf("msgstr without msgid")
		}
		idx, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(kw, "msgstr["), "]"))
		if err != nil || idx < 0 || idx > 20 {
			return p.errf("bad plural index %q", kw)
		}
		p.sawStr = true
		for len(p.cur.Translation) <= idx {
			p.cur.Translation = append(p.cur.Translation, "")
		}
		p.cur.Translation[idx] = val
		p.field, p.strIdx = "msgstr", idx
	default:
		return p.errf("unknown keyword %q", kw)
	}
	if obsolete {
		p.cur.Obsolete = true
	}
	return nil
}

func (p *parser) appendString(s string) error {
	switch p.field {
	case "msgctxt":
		p.cur.Context += s
	case "msgid":
		p.cur.ID += s
	case "msgid_plural":
		p.cur.IDPlural += s
	case "msgstr":
		p.cur.Translation[p.strIdx] += s
	default:
		return p.errf("string continuation without keyword")
	}
	return nil
}

func unquote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("expected quoted string, got %q", s)
	}
	s = s[1 : len(s)-1]
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(s[i:j], 8, 8)
			b.WriteByte(byte(v))
			i = j - 1
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

// SortedForMO returns translated, non-fuzzy, non-obsolete entries sorted by key
func (c *Catalog) SortedForMO() []*Entry {
	var out []*Entry
	for _, e := range c.Entries {
		if e.Obsolete || e.Fuzzy || !e.IsTranslated() {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}
