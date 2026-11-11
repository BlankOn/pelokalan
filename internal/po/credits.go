package po

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// IsCreditsEntry reports whether msgid is the translator credits string that programs show in
// their About dialog ("translator-credits", "translator_credits", "translator-credit"…)
func IsCreditsEntry(msgid string) bool {
	k := strings.ToLower(strings.TrimSpace(msgid))
	k = strings.NewReplacer("_", "-", " ", "-").Replace(k)
	return k == "translator-credits" || k == "translator-credit"
}

// Credit issue codes (see CheckCredits)
const (
	IssueCreditsMissing = "credits_missing" // the translator is not in the credits yet
	IssueCreditsYear    = "credits_year"    // the translator's credit lists years, but not this one
)

// yearsRe matches a list of years at the end of a credit: "2022", "2022, 2024", "2024-2026",
// "2020–2022, 2025." (the trailing dot is kept outside the match)
var yearsRe = regexp.MustCompile(`((?:19|20)\d\d(?:\s*[-–]\s*(?:19|20)\d\d)?(?:\s*,\s*(?:19|20)\d\d(?:\s*[-–]\s*(?:19|20)\d\d)?)*)\s*(\.?)\s*$`)

// yearSpan is a single year (from == to) or a range
type yearSpan struct{ from, to int }

func parseYears(s string) []yearSpan {
	var out []yearSpan
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		ends := strings.FieldsFunc(part, func(r rune) bool { return r == '-' || r == '–' })
		if len(ends) == 0 {
			continue
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(ends[0]))
		b, err2 := strconv.Atoi(strings.TrimSpace(ends[len(ends)-1]))
		if err1 != nil || err2 != nil {
			continue
		}
		if b < a {
			a, b = b, a
		}
		out = append(out, yearSpan{a, b})
	}
	return out
}

func formatYears(spans []yearSpan) string {
	parts := make([]string, len(spans))
	for i, s := range spans {
		if s.from == s.to {
			parts[i] = strconv.Itoa(s.from)
		} else {
			parts[i] = strconv.Itoa(s.from) + "-" + strconv.Itoa(s.to)
		}
	}
	return strings.Join(parts, ", ")
}

// AddYear adds year to a list of years: a year right after the last one extends it to a
// range ("2025" → "2025-2026", "2022, 2024-2025" → "2022, 2024-2026"), any other year is
// appended ("2024" → "2024, 2026"). A year already covered leaves the list unchanged.
func AddYear(years string, year int) string {
	spans := parseYears(years)
	for _, s := range spans {
		if year >= s.from && year <= s.to {
			return years
		}
	}
	if len(spans) == 0 {
		return strconv.Itoa(year)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	if last := &spans[len(spans)-1]; year == last.to+1 {
		last.to = year
	} else {
		spans = append(spans, yearSpan{year, year})
		sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	}
	return formatYears(spans)
}

// lineYears splits a credit line into the part before its years, the years and the
// trailing dot ("Jane <j@x>, 2022, 2024." → "Jane <j@x>", "2022, 2024", "."); ok is false
// when the line has no years
func lineYears(line string) (head, years, dot string, ok bool) {
	m := yearsRe.FindStringSubmatchIndex(line)
	if m == nil {
		return line, "", "", false
	}
	head = strings.TrimRight(strings.TrimSpace(line[:m[2]]), ", ")
	return head, line[m[2]:m[3]], line[m[4]:m[5]], true
}

// withYear returns a credit line with year added to its years ("Jane <j@x>, 2025." →
// "Jane <j@x>, 2025-2026."); a line without years gets ", 2026" plus its trailing dot
func withYear(line string, year int) string {
	trimmed := strings.TrimRight(line, " ")
	head, years, dot, ok := lineYears(trimmed)
	if !ok {
		if strings.HasSuffix(trimmed, ".") {
			return strings.TrimSuffix(trimmed, ".") + ", " + strconv.Itoa(year) + "."
		}
		return trimmed + ", " + strconv.Itoa(year)
	}
	return head + ", " + AddYear(years, year) + dot
}

// creditLineFor returns the index of the line crediting the translator (by email, else by
// name), or -1
func creditLineFor(lines []string, name, email string) int {
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		for i, l := range lines {
			if strings.Contains(strings.ToLower(l), email) {
				return i
			}
		}
	}
	if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
		for i, l := range lines {
			if strings.Contains(strings.ToLower(l), name) {
				return i
			}
		}
	}
	return -1
}

// CheckCredits checks the translation of the translator credits entry for the translator
// name/email: IssueCreditsMissing when they are not in it, IssueCreditsYear when their line
// lists years but not year. Empty when everything is fine or the entry is not the credits.
func CheckCredits(e *Entry, name, email string, year int) string {
	if !IsCreditsEntry(e.ID) || (name == "" && email == "") {
		return ""
	}
	text := ""
	if len(e.Translation) > 0 {
		text = e.Translation[0]
	}
	lines := strings.Split(text, "\n")
	i := creditLineFor(lines, name, email)
	if i < 0 || strings.TrimSpace(text) == "" {
		return IssueCreditsMissing
	}
	if _, years, _, ok := lineYears(strings.TrimRight(lines[i], " ")); ok && AddYear(years, year) != years {
		return IssueCreditsYear
	}
	return ""
}

// AddCredit returns the credits translation with the translator in it: their line gets
// year added (when it lists years), otherwise "Name <email>, year" is appended, ending
// with a dot when the other credit lines do. An empty or placeholder text is replaced.
func AddCredit(text, name, email string, year int) string {
	lines := strings.Split(text, "\n")
	if i := creditLineFor(lines, name, email); i >= 0 && strings.TrimSpace(text) != "" {
		if _, _, _, ok := lineYears(strings.TrimRight(lines[i], " ")); ok {
			lines[i] = withYear(lines[i], year)
		}
		return strings.Join(lines, "\n")
	}
	entry := name
	if email != "" {
		if entry != "" {
			entry += " "
		}
		entry += "<" + email + ">"
	}
	entry += ", " + strconv.Itoa(year)
	trimmed := strings.TrimRight(text, "\n ")
	if trimmed == "" || IsCreditsEntry(trimmed) {
		return entry
	}
	tl := strings.Split(trimmed, "\n")
	if strings.HasSuffix(strings.TrimSpace(tl[len(tl)-1]), ".") {
		entry += "."
	}
	return trimmed + "\n" + entry
}
