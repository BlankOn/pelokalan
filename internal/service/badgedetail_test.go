package service

import (
	"testing"
	"time"

	"pelokalan/internal/models"
)

func TestEarnedPeriod(t *testing.T) {
	d := func(m time.Month, day, n int) models.ProfileDay {
		return models.ProfileDay{Day: time.Date(2026, m, day, 0, 0, 0, 0, time.UTC), Contributions: n, Translated: n}
	}
	days := []models.ProfileDay{d(11, 2, 50), d(11, 3, 10), d(11, 5, 5), d(11, 6, 400), d(11, 7, 5), d(11, 8, 5)}
	date := func(p *time.Time) string {
		if p == nil {
			return "-"
		}
		return p.Format("01-02")
	}
	for _, c := range []struct {
		key        string
		t          int
		earned, fr string
		to         string
	}{
		{"streak_day", 3, "11-07", "11-05", "11-07"}, // the 2-day run of 2–3 Nov does not reach 3
		{"streak_day", 2, "11-03", "11-02", "11-03"},
		{"translated", 100, "11-06", "11-02", "11-06"}, // 50+10+5+400 passes 100 on 6 Nov
		{"best_day", 300, "11-06", "11-06", "11-06"},
		{"active_days", 4, "11-06", "11-02", "11-06"},
		{"streak_week", 2, "11-08", "11-02", "11-15"}, // weeks of 2 and 9 Nov… only the first has work → none
		{"weekend", 1, "11-07", "11-02", "11-07"},     // Saturday 7 Nov
		{"files", 5, "-", "-", "-"},                   // counted from totals: no date
	} {
		e, f, to := earnedPeriod(c.key, c.t, days)
		if c.key == "streak_week" { // 2–8 Nov is one week; there is no second
			if e != nil {
				t.Errorf("streak_week 2: earned %s", date(e))
			}
			continue
		}
		if date(e) != c.earned || date(f) != c.fr || date(to) != c.to {
			t.Errorf("%s %d: earned %s period %s–%s, want %s %s–%s", c.key, c.t, date(e), date(f), date(to), c.earned, c.fr, c.to)
		}
	}
	// weeks Monday–Sunday: work on Sun 8 Nov and Mon 9 Nov is two weeks in a row
	e, f, to := earnedPeriod("streak_week", 2, []models.ProfileDay{d(11, 8, 1), d(11, 9, 1)})
	if date(e) != "11-09" || date(f) != "11-02" || date(to) != "11-15" {
		t.Errorf("weeks: %s %s–%s", date(e), date(f), date(to))
	}
}
