package activity

import (
	"testing"
	"time"

	"pelokalan/internal/models"
)

func day(m time.Month, d, n int) models.ProfileDay {
	return models.ProfileDay{Day: time.Date(2026, m, d, 0, 0, 0, 0, time.UTC), Contributions: n, Translated: n}
}

func TestComputeProfileStats(t *testing.T) {
	days := []models.ProfileDay{
		day(9, 1, 5), day(9, 2, 5), day(9, 3, 5), day(9, 4, 5), // 4 days (Tue-Fri)
		day(10, 3, 1),                                     // a Saturday
		day(11, 20, 10), day(11, 21, 300), day(11, 22, 2), // Fri, Sat, Sun: current until today = Mon 23
	}
	today := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	s := ComputeProfileStats(days, nil, time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), today)
	if s.BestStreakDays != 4 || s.StreakDays != 3 || !s.StreakFrom.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("days: best %d current %d from %v", s.BestStreakDays, s.StreakDays, s.StreakFrom)
	}
	// weeks: Sep 1 (wk A), Oct 3 (wk B), Nov 20-22 (wk C, last week; today starts a new one)
	if s.StreakWeeks != 1 || s.BestStreakWeeks != 1 {
		t.Errorf("weeks: best %d current %d", s.BestStreakWeeks, s.StreakWeeks)
	}
	// months: Sep, Oct, Nov in a row, Nov is this month
	if s.StreakMonths != 3 || s.BestStreakMonths != 3 {
		t.Errorf("months: best %d current %d", s.BestStreakMonths, s.StreakMonths)
	}
	if s.ActiveDays != 8 || s.BestDay != 300 || s.Weekends != 2 || s.MemberDays != 90 || s.Translated != 333 {
		t.Errorf("counts: %+v", s)
	}
	// a gap of two days ends the current streak
	s = ComputeProfileStats(days, nil, time.Time{}, today.AddDate(0, 0, 2))
	if s.StreakDays != 0 || s.BestStreakDays != 4 {
		t.Errorf("after a gap: %d / %d", s.StreakDays, s.BestStreakDays)
	}
}

func TestEvalBadge(t *testing.T) {
	d := BadgeDef{ID: "x", Tiers: []int{10, 100, 1000}, value: func(s *ProfileStats) int { return s.Translated }}
	for _, c := range []struct{ v, tier, target, pct int }{
		{0, 0, 10, 0}, {5, 0, 10, 50}, {10, 1, 100, 0}, {55, 1, 100, 50}, {999, 2, 1000, 99}, {5000, 3, 1000, 100},
	} {
		b := evalBadge(d, &ProfileStats{Translated: c.v})
		if b.Tier != c.tier || b.Target != c.target || b.Percent != c.pct {
			t.Errorf("%d: tier %d target %d pct %d", c.v, b.Tier, b.Target, b.Percent)
		}
	}
	for _, g := range Badges {
		for _, b := range g.Badges {
			if len(b.Tiers) == 0 || len(b.Tiers) > len(BadgeTierNames) {
				t.Errorf("%s: %d tiers", b.ID, len(b.Tiers))
			}
			for i := 1; i < len(b.Tiers); i++ {
				if b.Tiers[i] <= b.Tiers[i-1] {
					t.Errorf("%s: tiers not increasing", b.ID)
				}
			}
		}
	}
}

func TestVeteranNeedsEveryMonth(t *testing.T) {
	joined := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC) // 14 months a member
	var days []models.ProfileDay
	for m := 0; m < 14; m++ { // one day of work in every month
		days = append(days, models.ProfileDay{Day: time.Date(2025, time.January+time.Month(m), 12, 0, 0, 0, 0, time.UTC), Contributions: 1})
	}
	vet := func(days []models.ProfileDay) Badge {
		s := ComputeProfileStats(days, nil, joined, today)
		for _, g := range Badges {
			for _, d := range g.Badges {
				if d.ID == "veteran" {
					return evalBadge(d, &s)
				}
			}
		}
		t.Fatal("no veteran badge")
		return Badge{}
	}
	if b := vet(days); b.Tier != 1 || b.Value != 14 || b.ShownTarget() != 2 {
		t.Errorf("every month: tier %d value %d next %d years", b.Tier, b.Value, b.ShownTarget())
	}
	// a month without work breaks the run: 6 + 7 months, no full year
	gap := append(append([]models.ProfileDay{}, days[:6]...), days[7:]...)
	if b := vet(gap); b.Tier != 0 || b.Value != 7 {
		t.Errorf("with a gap: tier %d value %d", b.Tier, b.Value)
	}
	// a year of work but only 11 months a member (joined later) is not enough either
	s := ComputeProfileStats(days, nil, time.Date(2025, 4, 20, 0, 0, 0, 0, time.UTC), today)
	if s.MemberMonths != 10 {
		t.Errorf("member months %d", s.MemberMonths)
	}
}
