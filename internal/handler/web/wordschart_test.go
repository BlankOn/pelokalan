package web

import (
	"testing"
	"time"

	"pelokalan/internal/models"
)

func TestBuildWordsChart(t *testing.T) {
	d0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	days := []models.ActivityDay{
		{Day: d0, Strings: 2, Words: 10},
		{Day: d0.AddDate(0, 0, 2), Strings: 1, Words: 5}, // nothing on the 2nd
	}
	c := buildWordsChart("id", days, d0.AddDate(0, 0, 3).Add(5*time.Hour))
	if c.Words != 15 || c.Strings != 3 {
		t.Fatalf("totals %d words %d strings", c.Words, c.Strings)
	}
	want := []int{10, 10, 15, 15} // Nov 1..4: flat on the 2nd and on today
	if len(c.Points) != len(want) {
		t.Fatalf("points %d, want %d", len(c.Points), len(want))
	}
	for i, w := range want {
		if c.Points[i].Words != w {
			t.Errorf("day %d: %d words, want %d", i, c.Points[i].Words, w)
		}
	}
	if c.Points[1].DayWords != 0 || c.Points[2].DayWords != 5 {
		t.Errorf("per-day words wrong: %+v", c.Points)
	}
	if buildWordsChart("id", nil, d0) != nil {
		t.Error("no data should give no chart")
	}
}
