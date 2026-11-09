package po

import (
	"regexp"
	"sort"
	"strings"
)

// Issue codes returned by Validate
const (
	IssuePlaceholders = "placeholders"
	IssueNewlineStart = "newline_start"
	IssueNewlineEnd   = "newline_end"
	IssuePluralCount  = "plural_count"
	IssueControlChars = "control_chars" // invisible control characters (e.g. mangled markup)
	IssueMarkup       = "markup"        // XML-like tags (<gui>, <key>…) differ from the source
)

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

// Validate checks a translated entry for common problems and returns issue codes
func Validate(e *Entry, nplurals int) []string {
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
		if hasControlChars(tr) && !hasControlChars(src) {
			add(IssueControlChars)
		}
		if want := markupTags(src); len(want) > 0 && !equal(want, markupTags(tr)) {
			add(IssueMarkup)
		}
		if re != nil {
			want := placeholders(re, src)
			got := placeholders(re, tr)
			// a plural form may legitimately drop the count placeholder (e.g. "one file")
			if e.IsPlural() && len(got) < len(want) && subset(got, want) {
				continue
			}
			if !equal(want, got) {
				add(IssuePlaceholders)
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
