package po

import (
	"strconv"
	"strings"
)

// AddContributor records a translator in the header comments using the
// gettext convention "Name <email>, 2025, 2026.". An existing line for the same
// email gets the year appended; the template placeholder
// "FIRST AUTHOR <EMAIL@ADDRESS>, YEAR." is replaced.
func AddContributor(comments []string, name, email string, year int) []string {
	y := strconv.Itoa(year)
	out := append([]string(nil), comments...)
	needle := "<" + strings.ToLower(email) + ">"
	for i, line := range out {
		if !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		if strings.Contains(line, y) {
			return out
		}
		trimmed := strings.TrimRight(line, " ")
		if strings.HasSuffix(trimmed, ".") {
			out[i] = strings.TrimSuffix(trimmed, ".") + ", " + y + "."
		} else {
			out[i] = trimmed + ", " + y + "."
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
	return append(out, entry)
}
