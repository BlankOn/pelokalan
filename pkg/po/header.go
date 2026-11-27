package po

import (
	"regexp"
	"strconv"
	"strings"
)

// AddContributor records a translator in the header comments using the
// gettext convention "Name <email>, 2024, 2026.". An existing line for the same
// email gets the year added (AddYear: "2025" becomes "2025-2026", "2024" becomes "2024, 2026"); the template placeholder
// "FIRST AUTHOR <EMAIL@ADDRESS>, YEAR." is replaced.
func AddContributor(comments []string, name, email string, year int) []string {
	y := strconv.Itoa(year)
	out := append([]string(nil), comments...)
	needle := "<" + strings.ToLower(email) + ">"
	for i, line := range out {
		if !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		line = withYear(line, year)
		if !strings.HasSuffix(line, ".") {
			line += "."
		}
		out[i] = line
		// an earlier export may have put the line below a bare "#" after the other credits:
		// move it back next to them
		if separatedFromCredits(out, i) {
			rest := append(append([]string(nil), out[:i]...), out[i+1:]...)
			return insertContributor(rest, line)
		}
		return out
	}
	entry := name + " <" + email + ">, " + y + "."
	for i, line := range out {
		if strings.HasPrefix(strings.TrimSpace(line), "FIRST AUTHOR <EMAIL@ADDRESS>") {
			out[i] = entry
			return out
		}
	}
	return insertContributor(out, entry)
}

// contributorLineRe matches a translator credit such as "Jane <jane@example.org>, 2022, 2024."
var contributorLineRe = regexp.MustCompile(`<[^<>\s]+@[^<>\s]+>.*\b(19|20)\d\d\b`)

// insertContributor puts a new credit right after the last existing one, so the list stays
// together; without credits it goes before trailing empty comment lines (a bare "#").
func insertContributor(lines []string, entry string) []string {
	at := -1
	for i, line := range lines {
		if contributorLineRe.MatchString(line) {
			at = i + 1
		}
	}
	if at < 0 {
		at = len(lines)
		for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, entry)
	return append(out, lines[at:]...)
}

// separatedFromCredits reports whether line i is a credit that sits after an empty line
// following other credits (e.g. "A <a@x>, 2022.", "", "B <b@x>, 2026.")
func separatedFromCredits(lines []string, i int) bool {
	blank := false
	for k := i - 1; k >= 0; k-- {
		if strings.TrimSpace(lines[k]) == "" {
			blank = true
			continue
		}
		if contributorLineRe.MatchString(lines[k]) {
			return blank
		}
		return false
	}
	return false
}
