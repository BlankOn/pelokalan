package catalog

import (
	"sort"
	"strings"
	"testing"
)

func TestCompareProgressRemaining(t *testing.T) {
	files := []FileProgress{
		{Name: "done", Total: 10, Translated: 10},
		{Name: "empty-small", Total: 5},
		{Name: "half", Total: 100, Translated: 50},
		{Name: "empty-big", Total: 300},
		{Name: "nearly", Total: 20, Translated: 19},
		{Name: "a-done", Total: 3, Translated: 3},
	}
	sort.SliceStable(files, func(i, j int) bool { return CompareProgress("", files[i], files[j]) < 0 })
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	// work left (most left first), then files nobody started, then complete files
	if got, want := strings.Join(names, " "), "half nearly empty-big empty-small a-done done"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}
