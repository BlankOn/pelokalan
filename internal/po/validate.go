package po

import (
	"regexp"
	"sort"
	"strings"
)

// Issue codes returned by Validate
const (
	IssuePlaceholders = "placeholders"
	// a plural translation lacks the placeholders of msgid_plural (msgfmt -c rejects the file)
	IssuePluralPlaceholders = "plural_placeholders"
	IssueNewlineStart       = "newline_start"
	IssueNewlineEnd         = "newline_end"
	IssuePluralCount        = "plural_count"
	IssueControlChars       = "control_chars" // invisible control characters (e.g. mangled markup)
	IssueMarkup             = "markup"        // XML-like tags (<gui>, <key>…) differ from the source
	IssueSpaceStart         = "space_start"   // leading space/tab present in one of source/translation but not the other
	IssueSpaceEnd           = "space_end"     // trailing space/tab present in one of source/translation but not the other
)

// hasLeadingBlank reports whether s begins with a space or tab
func hasLeadingBlank(s string) bool { return strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") }

// hasTrailingBlank reports whether s ends with a space or tab
func hasTrailingBlank(s string) bool { return strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\t") }

// markupTagRe matches XML-like tags such as <gui>, </key>, <link href="…"> or <br/>
var markupTagRe = regexp.MustCompile(`<(/?)([A-Za-z][\w:.-]*)(?:\s[^<>]*)?(/?)>`)

// markupTags returns the tags of s (name with "/" marking closing tags), sorted
func markupTags(s string) []string {
	var out []string
	for _, m := range markupTagRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+m[2]+m[3])
	}
	sort.Strings(out)
	return out
}

// hasControlChars reports C0 control characters other than tab and newlines
func hasControlChars(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0x7f {
			return true
		}
	}
	return false
}

var (
	cFormatRe      = regexp.MustCompile(`%(?:\d+\$)?[-+ 0#']*(?:\d+|\*)?(?:\.(?:\d+|\*))?(?:hh|h|ll|l|L|q|j|z|t|I64|I32)?[diouxXeEfFgGaAcspn%]`)
	pythonFormatRe = regexp.MustCompile(`%(?:\([A-Za-z0-9_]+\))?[-+ 0#]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[diouxXeEfFgGcrsa%]`)
	braceFormatRe  = regexp.MustCompile(`\{[A-Za-z0-9_.\[\]]*(?:![rsa])?(?::[^{}]*)?\}`)
)

// Validate checks a translated entry for common problems and returns issue codes.
// Without the file's Plural-Forms it assumes gettext's default plural rule; use
// ValidateWith to check plural placeholders exactly like msgfmt -c does.
func Validate(e *Entry, nplurals int) []string {
	return ValidateWith(e, PluralFor("", nplurals))
}

// ValidateWith is Validate with the file's plural rule (PluralFor of its Plural-Forms)
func ValidateWith(e *Entry, plural Plural) []string {
	nplurals := plural.N
	if !e.IsTranslated() {
		return nil
	}
	var issues []string
	add := func(code string) {
		for _, c := range issues {
			if c == code {
				return
			}
		}
		issues = append(issues, code)
	}

	if e.IsPlural() && nplurals > 0 && len(e.Translation) != nplurals {
		add(IssuePluralCount)
	}

	re := formatRegexp(e)
	for i, tr := range e.Translation {
		src := e.ID
		if i > 0 && e.IsPlural() {
			src = e.IDPlural
		}
		if strings.HasPrefix(src, "\n") != strings.HasPrefix(tr, "\n") {
			add(IssueNewlineStart)
		}
		if strings.HasSuffix(src, "\n") != strings.HasSuffix(tr, "\n") {
			add(IssueNewlineEnd)
		}
		if hasLeadingBlank(src) != hasLeadingBlank(tr) {
			add(IssueSpaceStart)
		}
		if hasTrailingBlank(src) != hasTrailingBlank(tr) {
			add(IssueSpaceEnd)
		}
		if hasControlChars(tr) && !hasControlChars(src) {
			add(IssueControlChars)
		}
		if want := markupTags(src); len(want) > 0 && !equal(want, markupTags(tr)) {
			add(IssueMarkup)
		}
		if re != nil {
			// like msgfmt -c: every plural form is checked against msgid_plural. Only a
			// form used for at most one number (e.g. "one file" for n == 1), in a file with
			// several forms, may leave out a placeholder. With a single form (Indonesian,
			// Japanese…) msgstr[0] stands for every number and must match msgid_plural.
			ref := src
			if e.IsPlural() {
				ref = e.IDPlural
			}
			want := placeholders(re, ref)
			got := placeholders(re, tr)
			relaxed := e.IsPlural() && len(e.Translation) > 1 && plural.SingleValue(i)
			if relaxed && len(got) < len(want) && subset(got, want) {
				continue
			}
			if !equal(want, got) {
				if e.IsPlural() {
					add(IssuePluralPlaceholders)
				} else {
					add(IssuePlaceholders)
				}
			}
		}
	}
	return issues
}

func formatRegexp(e *Entry) *regexp.Regexp {
	switch {
	case e.HasFlag("python-brace-format"):
		return braceFormatRe
	case e.HasFlag("python-format"):
		return pythonFormatRe
	}
	for _, f := range e.Flags {
		if strings.HasSuffix(f, "-format") && !strings.HasPrefix(f, "no-") {
			return cFormatRe
		}
	}
	return nil
}

func placeholders(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllString(s, -1) {
		if m == "%%" {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
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

func subset(small, big []string) bool {
	counts := map[string]int{}
	for _, s := range big {
		counts[s]++
	}
	for _, s := range small {
		if counts[s] == 0 {
			return false
		}
		counts[s]--
	}
	return true
}

// Checks lists every issue code an editor can report, in the order the settings show them
var Checks = []string{
	IssuePlaceholders, IssuePluralPlaceholders, IssuePluralCount, IssueMarkup, IssueControlChars,
	IssueNewlineStart, IssueNewlineEnd, IssueSpaceStart, IssueSpaceEnd,
	IssueCreditsMissing, IssueCreditsYear,
}
