package web

import (
	"testing"
	"time"

	"pelokalan/internal/models"
)

func TestBuildWordsChart(t *testing.T) {
	h0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	hours := []models.ActivityDay{
		{Day: h0, Strings: 5, Manual: 2, AIKept: 3},
		{Day: h0.Add(2 * time.Hour), Strings: 4, AIEdited: 1, Edited: 3}, // nothing at 10:00
	}
	c := buildWordsChart("id", hours, h0.Add(3*time.Hour+20*time.Minute))
	if c.Strings != 9 {
		t.Fatalf("total %d strings, want 9", c.Strings)
	}
	want := []int{5, 5, 9, 9} // 09:00..12:00: flat at 10:00 and in the current hour
	if len(c.Points) != len(want) {
		t.Fatalf("points %d, want %d", len(c.Points), len(want))
	}
	for i, w := range want {
		if c.Points[i].Strings != w {
			t.Errorf("hour %d: %d strings, want %d", i, c.Points[i].Strings, w)
		}
		if !c.Points[i].Hour.Equal(h0.Add(time.Duration(i) * time.Hour)) {
			t.Errorf("hour %d starts at %v", i, c.Points[i].Hour)
		}
	}
	if c.Points[1].HourStrings != 0 || c.Points[2].HourStrings != 4 {
		t.Errorf("per-hour strings wrong: %+v", c.Points)
	}
	// the origin lines add up to the total, edits of existing translations included
	last := c.Points[3]
	if last.Manual != 2 || last.AIKept != 3 || last.AIEdited != 1 || last.Edited != 3 ||
		last.Manual+last.AIKept+last.AIEdited+last.Edited != last.Strings {
		t.Errorf("origin totals at the end: %+v", last)
	}
	if len(c.Series) != 4 || c.Series[0].Kind != "manual" || c.Series[3].Kind != "edited" || c.Series[3].Total != 3 {
		t.Errorf("series: %+v", c.Series)
	}
	if c.Points[0].YEdit <= c.Points[3].YEdit { // the line rises (smaller y) once a translation is edited
		t.Errorf("edited line does not rise: %v → %v", c.Points[0].YEdit, c.Points[3].YEdit)
	}
	if !c.XLabels[0].WithTime {
		t.Error("a short span should label the hours")
	}
	if buildWordsChart("id", nil, h0) != nil {
		t.Error("no data should give no chart")
	}
}
