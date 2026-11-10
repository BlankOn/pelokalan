package web

import (
	"testing"
	"time"

	"pelokalan/internal/models"
)

func TestSumByHour(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 9, h, m, 0, 0, time.UTC) }
	a := []models.GroupStat{{Total: 100, Translated: 60, Fuzzy: 10, RecordedAt: at(10, 5)}, {Total: 100, Translated: 70, Fuzzy: 5, RecordedAt: at(12, 5)}}
	b := []models.GroupStat{{Total: 50, Translated: 20, Fuzzy: 5, RecordedAt: at(11, 40)}}
	got := sumByHour([][]models.GroupStat{a, b})
	// hours 10, 11, 12: at 10 only a counts; from 11 both (a carried forward)
	want := []struct{ total, tr, fz int }{{100, 60, 10}, {150, 80, 15}, {150, 90, 10}}
	if len(got) != len(want) {
		t.Fatalf("got %d points: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Total != w.total || got[i].Translated != w.tr || got[i].Fuzzy != w.fz {
			t.Errorf("hour %d: %+v, want %+v", i, got[i], w)
		}
	}
	if one := sumByHour([][]models.GroupStat{a}); len(one) != 2 {
		t.Errorf("a single group keeps its own points: %+v", one)
	}
}

func TestLastPerBucket(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.UTC) }
	in := []models.GroupStat{{Translated: 1, RecordedAt: at(8, 10, 5)}, {Translated: 2, RecordedAt: at(8, 10, 50)},
		{Translated: 3, RecordedAt: at(8, 11, 5)}, {Translated: 4, RecordedAt: at(9, 9, 0)}}
	hourly := lastPerBucket(in, time.Hour)
	if len(hourly) != 3 || hourly[0].Translated != 2 {
		t.Errorf("hourly: %+v", hourly)
	}
	daily := lastPerBucket(in, 24*time.Hour)
	if len(daily) != 2 || daily[0].Translated != 3 || daily[1].Translated != 4 {
		t.Errorf("daily: %+v", daily)
	}
}

func TestNiceStep(t *testing.T) {
	for raw, want := range map[int]int{1: 1, 3: 5, 7: 10, 12: 20, 7926: 10000, 4800: 5000, 250: 500} {
		if got := niceStep(raw); got != want {
			t.Errorf("niceStep(%d) = %d, want %d", raw, got, want)
		}
	}
}
