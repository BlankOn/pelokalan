package activity

import (
	"context"
	"time"

	"pelokalan/internal/models"
)

// Badge details for the share page: when a tier was earned and the work behind it (per project and
// module). Streak badges show the run of days, weeks or months that earned the tier; counting badges
// show the work up to the day the count was reached; the rest show all work of their kind.

// BadgeDetail is what earned one badge tier
type BadgeDetail struct {
	EarnedOn *time.Time // the day the tier was reached (site time zone date), when known
	From, To *time.Time // the period the work below comes from (dates, inclusive), nil = all
	Count    string     // what the Count column means: badges.count_<Count> (strings, words, comments, …)
	Work     []*models.ModuleWork
	Modules  bool // Work is per module (else per project)
}

// badgeKind maps badges to how their details are found
var badgeKind = map[string]struct {
	work  string // UserModuleWork kind, or "landed" / a community kind
	count string // the Count column
	day   func(d models.ProfileDay) int
}{
	"streak_day": {"contrib", "strings", nil}, "streak_week": {"contrib", "strings", nil},
	"streak_month": {"contrib", "strings", nil}, "veteran": {"contrib", "strings", nil},
	"active_days": {"contrib", "strings", nil}, "weekend": {"weekend", "strings", nil},
	"translated": {"new", "strings", func(d models.ProfileDay) int { return d.Translated }},
	"words":      {"words", "words", func(d models.ProfileDay) int { return d.Words }},
	"reviewed":   {"reviewed", "strings", func(d models.ProfileDay) int { return d.Reviewed }},
	"edited":     {"edited", "strings", func(d models.ProfileDay) int { return d.Edited }},
	"manual":     {"manual", "strings", func(d models.ProfileDay) int { return d.Manual }},
	"ai":         {"ai", "strings", func(d models.ProfileDay) int { return d.AI }},
	"night":      {"night", "strings", func(d models.ProfileDay) int { return d.Night }},
	"early":      {"early", "strings", func(d models.ProfileDay) int { return d.Early }},
	"best_day":   {"contrib", "strings", nil},
	"completed":  {"contrib", "strings", nil}, "files": {"contrib", "strings", nil},
	"projects": {"contrib", "strings", nil}, "languages": {"contrib", "strings", nil},
	"landed": {"landed", "strings", nil}, "landed_modules": {"landed", "strings", nil},
	"comments": {"comments", "comments", nil}, "topics": {"topics", "topics", nil},
	"wiki": {"wiki", "edits", nil}, "context": {"context", "edits", nil}, "commits": {"commits", "commits", nil},
}

func datePtr(t time.Time) *time.Time { return &t }

// runReaching finds the first run of consecutive numbers in sorted ns that reaches length n, returning its
// first number and the number at which it reached n
func runReaching(ns []int, n int) (start, reached int, ok bool) {
	run := 0
	for i, v := range ns {
		if i > 0 && v == ns[i-1]+1 {
			run++
		} else {
			run, start = 1, v
		}
		if run >= n {
			return start, v, true
		}
	}
	return 0, 0, false
}

func fromDayNum(n int) time.Time { return time.Unix(int64(n)*86400, 0).UTC() }

// earnedPeriod works out, from the days of work, when threshold t of badge key was reached and the
// period of work that reached it
func earnedPeriod(key string, t int, days []models.ProfileDay) (earned, from, to *time.Time) {
	var active []models.ProfileDay
	for _, d := range days {
		if d.Contributions > 0 {
			active = append(active, d)
		}
	}
	if len(active) == 0 {
		return
	}
	switch key {
	case "streak_day":
		var ns []int
		for _, d := range active {
			ns = append(ns, dayNum(d.Day))
		}
		if s, r, ok := runReaching(ns, t); ok {
			earned, from, to = datePtr(fromDayNum(r)), datePtr(fromDayNum(s)), datePtr(fromDayNum(r))
		}
	case "streak_week":
		var ws []int
		last := map[int]time.Time{}
		for _, d := range active {
			w := (dayNum(d.Day) + 3) / 7
			if len(ws) == 0 || ws[len(ws)-1] != w {
				ws = append(ws, w)
			}
			last[w] = d.Day
		}
		if s, r, ok := runReaching(ws, t); ok {
			earned = datePtr(last[r])
			from, to = datePtr(fromDayNum(s*7-3)), datePtr(fromDayNum(r*7+3)) // Monday … Sunday
		}
	case "streak_month", "veteran":
		var ms []int
		last := map[int]time.Time{}
		for _, d := range active {
			m := d.Day.Year()*12 + int(d.Day.Month()) - 1
			if len(ms) == 0 || ms[len(ms)-1] != m {
				ms = append(ms, m)
			}
			last[m] = d.Day
		}
		if s, r, ok := runReaching(ms, t); ok {
			earned = datePtr(last[r])
			f := time.Date(s/12, time.Month(s%12+1), 1, 0, 0, 0, 0, time.UTC)
			e := time.Date(r/12, time.Month(r%12+1), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
			from, to = &f, &e
		}
	case "active_days":
		if t <= len(active) {
			earned, from, to = datePtr(active[t-1].Day), datePtr(active[0].Day), datePtr(active[t-1].Day)
		}
	case "weekend":
		seen := map[int]bool{}
		for _, d := range active {
			if wd := d.Day.Weekday(); wd == time.Saturday || wd == time.Sunday {
				seen[(dayNum(d.Day)+3)/7] = true
				if len(seen) == t {
					earned, from, to = datePtr(d.Day), datePtr(active[0].Day), datePtr(d.Day)
					return
				}
			}
		}
	case "best_day":
		for _, d := range active {
			if d.Contributions >= t {
				earned, from, to = datePtr(d.Day), datePtr(d.Day), datePtr(d.Day)
				return
			}
		}
	default:
		k, ok := badgeKind[key]
		if !ok || k.day == nil {
			return
		}
		sum := 0
		for _, d := range active {
			sum += k.day(d)
			if sum >= t {
				earned, from, to = datePtr(d.Day), datePtr(active[0].Day), datePtr(d.Day)
				return
			}
		}
	}
	return
}

// BadgeDetail gathers what earned tier (1-based) of badge b for the profile's person; loc is the site's
// time zone (days are dates in it)
func (s *Service) BadgeDetail(ctx context.Context, p *Profile, b Badge, tier int, loc *time.Location) (*BadgeDetail, error) {
	k, ok := badgeKind[b.Key]
	if !ok {
		return &BadgeDetail{}, nil
	}
	t := b.Steps[tier-1]
	d := &BadgeDetail{Count: k.count, Modules: true}
	const limit = 25
	var err error
	switch k.work {
	case "landed":
		d.Work, err = s.repo.UserLandingWork(ctx, p.User.ID, limit)
		if err == nil {
			kind := "landed"
			if b.Key == "landed_modules" {
				kind = "landed_modules"
			}
			var at *time.Time
			if at, err = s.repo.NthTime(ctx, p.User.ID, kind, t); at != nil {
				d.EarnedOn = at
			}
		}
	case "comments", "topics", "wiki", "context", "commits":
		d.Modules = false
		d.Work, err = s.repo.UserCommunityWork(ctx, p.User.ID, k.work, limit)
		if err == nil {
			d.EarnedOn, err = s.repo.NthTime(ctx, p.User.ID, k.work, t)
		}
	default:
		d.EarnedOn, d.From, d.To = earnedPeriod(b.Key, t, p.Days)
		var from, to *time.Time // the period's dates as times in the site's zone
		if d.From != nil {
			f := time.Date(d.From.Year(), d.From.Month(), d.From.Day(), 0, 0, 0, 0, loc)
			from = &f
		}
		if d.To != nil {
			e := time.Date(d.To.Year(), d.To.Month(), d.To.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			to = &e
		}
		d.Work, err = s.repo.UserModuleWork(ctx, p.User.ID, loc.String(), k.work, from, to, limit)
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}
