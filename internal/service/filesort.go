package service

import "strings"

// FileProgress is what the file list orders compare (project files and members' files alike)
type FileProgress struct {
	Total, Translated int
	Name              string
}

// remainingBand puts files with work left first, then files nobody started, then complete ones
func remainingBand(f FileProgress) int {
	switch {
	case f.Translated >= f.Total:
		return 2
	case f.Translated == 0:
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// CompareProgress orders two files by one of ProjectFileSorts in Go, with the same rules as the database
// ("" or unknown = "remaining"); 0 = tie
func CompareProgress(sort string, a, b FileProgress) int {
	byName := func() int { return strings.Compare(a.Name, b.Name) }
	ratio := func(f FileProgress) float64 { return float64(f.Translated) / float64(max(f.Total, 1)) }
	cmpRatio := func() int {
		ra, rb := ratio(a), ratio(b)
		switch {
		case ra < rb:
			return -1
		case ra > rb:
			return 1
		}
		return 0
	}
	var c int
	switch sort {
	case "incomplete":
		if c = cmpRatio(); c == 0 {
			c = cmpInt(b.Total-b.Translated, a.Total-a.Translated)
		}
	case "complete":
		if c = -cmpRatio(); c == 0 {
			c = cmpInt(b.Translated, a.Translated)
		}
	case "most":
		c = cmpInt(b.Total, a.Total)
	case "fewest":
		c = cmpInt(a.Total, b.Total)
	case "name":
	default: // remaining
		if c = cmpInt(remainingBand(a), remainingBand(b)); c == 0 {
			c = cmpInt(b.Total-b.Translated, a.Total-a.Translated)
		}
	}
	if c == 0 {
		c = byName()
	}
	return c
}
