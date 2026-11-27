package po

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
)

const wrapWidth = 79

// Write serializes the catalog in PO format
func (c *Catalog) Write(w io.Writer) error {
	var b bytes.Buffer

	for _, line := range c.HeaderComments {
		writeComment(&b, "#", line)
	}
	if c.HeaderFuzzy {
		b.WriteString("#, fuzzy\n")
	}
	writeString(&b, "", "msgid", "")
	writeString(&b, "", "msgstr", c.Header)

	for _, e := range c.Entries {
		b.WriteByte('\n')
		writeEntry(&b, e)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// Bytes serializes the catalog in PO format
func (c *Catalog) Bytes() []byte {
	var b bytes.Buffer
	_ = c.Write(&b)
	return b.Bytes()
}

func writeEntry(b *bytes.Buffer, e *Entry) {
	for _, line := range e.TranslatorComments {
		writeComment(b, "#", line)
	}
	for _, line := range e.ExtractedComments {
		writeComment(b, "#.", line)
	}
	if !e.Obsolete {
		for _, line := range e.References {
			writeComment(b, "#:", line)
		}
	}
	flags := e.Flags
	if e.Fuzzy {
		flags = append([]string{"fuzzy"}, flags...)
	}
	if len(flags) > 0 {
		b.WriteString("#, " + strings.Join(flags, ", ") + "\n")
	}
	prevPrefix := "#| "
	prefix := ""
	if e.Obsolete {
		prevPrefix = "#~| "
		prefix = "#~ "
	}
	for _, line := range e.Previous {
		b.WriteString(prevPrefix + line + "\n")
	}
	if e.HasContext {
		writeString(b, prefix, "msgctxt", e.Context)
	}
	writeString(b, prefix, "msgid", e.ID)
	if e.IsPlural() {
		writeString(b, prefix, "msgid_plural", e.IDPlural)
		tr := e.Translation
		if len(tr) == 0 {
			tr = []string{"", ""}
		}
		for i, s := range tr {
			writeString(b, prefix, "msgstr["+itoa(i)+"]", s)
		}
	} else {
		s := ""
		if len(e.Translation) > 0 {
			s = e.Translation[0]
		}
		writeString(b, prefix, "msgstr", s)
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func writeComment(b *bytes.Buffer, marker, text string) {
	if text == "" {
		b.WriteString(marker + "\n")
		return
	}
	b.WriteString(marker + " " + text + "\n")
}

func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
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
			b.WriteByte(c)
		}
	}
	return b.String()
}

// writeString writes `keyword "value"`, wrapping long or multi-line strings the
// way gettext tools do.
func writeString(b *bytes.Buffer, prefix, keyword, value string) {
	esc := escape(value)
	head := prefix + keyword + " "
	inner := strings.TrimSuffix(value, "\n")
	if !strings.Contains(inner, "\n") && len(head)+len(esc)+2 <= wrapWidth {
		b.WriteString(head + `"` + esc + "\"\n")
		return
	}
	b.WriteString(head + "\"\"\n")
	for _, chunk := range wrapEscaped(esc, wrapWidth-len(prefix)-2) {
		b.WriteString(prefix + `"` + chunk + "\"\n")
	}
}

// wrapEscaped splits an escaped string after each "\n" and at spaces so every
// chunk fits in width bytes when possible.
func wrapEscaped(esc string, width int) []string {
	var parts []string
	for {
		i := strings.Index(esc, `\n`)
		// make sure the backslash is not itself escaped
		for i > 0 && countBackslashes(esc[:i])%2 == 1 {
			j := strings.Index(esc[i+2:], `\n`)
			if j < 0 {
				i = -1
				break
			}
			i += 2 + j
		}
		if i < 0 || i+2 == len(esc) {
			parts = append(parts, esc)
			break
		}
		parts = append(parts, esc[:i+2])
		esc = esc[i+2:]
	}

	var out []string
	for _, p := range parts {
		for len(p) > width {
			cut := strings.LastIndex(p[:width], " ")
			if cut <= 0 {
				// no space: cut at width, avoiding a split escape sequence
				cut = width
				for cut > 0 && countBackslashes(p[:cut])%2 == 1 {
					cut--
				}
				if cut == 0 {
					break
				}
				out = append(out, p[:cut])
				p = p[cut:]
				continue
			}
			out = append(out, p[:cut+1])
			p = p[cut+1:]
		}
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

func countBackslashes(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n
}

// CompileMO compiles the catalog into the binary MO format (little endian,
// without a hash table, which every runtime accepts).
func (c *Catalog) CompileMO() []byte {
	type pair struct{ k, v string }
	pairs := []pair{{"", c.Header}}
	if c.HeaderFuzzy {
		pairs = pairs[:0]
	}
	for _, e := range c.SortedForMO() {
		k := e.Key()
		if e.IsPlural() {
			k += "\x00" + e.IDPlural
		}
		pairs = append(pairs, pair{k, strings.Join(e.Translation, "\x00")})
	}

	n := uint32(len(pairs))
	const headerSize = 28
	origTab := uint32(headerSize)
	transTab := origTab + 8*n
	dataStart := transTab + 8*n

	var data bytes.Buffer
	origs := make([][2]uint32, n)
	trans := make([][2]uint32, n)
	for i, p := range pairs {
		origs[i] = [2]uint32{uint32(len(p.k)), dataStart + uint32(data.Len())}
		data.WriteString(p.k)
		data.WriteByte(0)
	}
	for i, p := range pairs {
		trans[i] = [2]uint32{uint32(len(p.v)), dataStart + uint32(data.Len())}
		data.WriteString(p.v)
		data.WriteByte(0)
	}

	var out bytes.Buffer
	le := binary.LittleEndian
	for _, v := range []uint32{0x950412de, 0, n, origTab, transTab, 0, dataStart} {
		_ = binary.Write(&out, le, v)
	}
	for _, o := range origs {
		_ = binary.Write(&out, le, o)
	}
	for _, t := range trans {
		_ = binary.Write(&out, le, t)
	}
	out.Write(data.Bytes())
	return out.Bytes()
}
