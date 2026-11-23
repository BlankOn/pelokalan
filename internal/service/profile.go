package service

import (
	"context"
	"sort"
	"time"

	"pelokalan/internal/models"
)

// Public profile pages (/users/{username}): a person's translation work across all projects, a
// calendar of it, streaks and badges. Everything comes from the activity log (activity_events) plus a
// few counts of community work; the log starts when it was deployed, so earlier work is not counted.
// Days are calendar days in the site's time zone (SITE_TIMEZONE).

// Profile is what the profile page shows
type Profile struct {
	User   *models.User
	Days   []models.ProfileDay
	Totals *models.ProfileTotals
	Stats  ProfileStats
	Badges []BadgeGroup
	Earned int // badge tiers earned
	Tiers  int // badge tiers there are
	Today  time.Time
}

// ProfileStats are the numbers the badges are measured on
type ProfileStats struct {
	Contributions int
	Translated    int
	Words         int
	Reviewed      int
	Edited        int
	ActiveDays    int
	BestDay       int
	BestDayAt     time.Time

	StreakDays, BestStreakDays     int // calendar days in a row with work
	StreakWeeks, BestStreakWeeks   int // weeks (Monday to Sunday) in a row
	StreakMonths, BestStreakMonths int // calendar months in a row
	StreakFrom                     time.Time

	Night, Early int // contributions late at night / early in the morning
	Weekends     int // weekends with work
	MemberDays   int // days since signing up

	Files, Projects, Languages, CompletedFiles        int
	Comments, Topics, WikiEdits, ContextEdits, Commits int
}

// BadgeDef is one badge with its tiers: thresholds of one number, lowest first
type BadgeDef struct {
	Key   string // badge.<key> (name), badge.<key>_desc (what it takes, with the target)
	Icon  string // badge_icon partial
	Tiers []int
	value func(*ProfileStats) int
}

// BadgeGroupDef is a row of related badges
type BadgeGroupDef struct {
	Key    string // badges.group_<key>
	Badges []BadgeDef
}

// BadgeTierNames are the tier names (badges.tier_<n>), lowest first; a badge has at most this many tiers
var BadgeTierNames = []string{"bronze", "silver", "gold", "platinum", "diamond"}

// Badges is the whole catalog. Tiers are spaced so the first is reached in a first session or week and
// the last takes a long time of steady work.
var Badges = []BadgeGroupDef{
	{"streak", []BadgeDef{
		{"streak_day", "flame", []int{3, 7, 30, 100, 365}, func(s *ProfileStats) int { return s.BestStreakDays }},
		{"streak_week", "week", []int{4, 12, 26, 52, 104}, func(s *ProfileStats) int { return s.BestStreakWeeks }},
		{"streak_month", "month", []int{3, 6, 12, 24, 48}, func(s *ProfileStats) int { return s.BestStreakMonths }},
		{"active_days", "calendar", []int{10, 50, 100, 365, 1000}, func(s *ProfileStats) int { return s.ActiveDays }},
	}},
	{"work", []BadgeDef{
		{"translated", "pen", []int{100, 1000, 5000, 20000, 100000}, func(s *ProfileStats) int { return s.Translated }},
		{"words", "type", []int{1000, 10000, 50000, 250000, 1000000}, func(s *ProfileStats) int { return s.Words }},
		{"reviewed", "check", []int{50, 500, 2500, 10000, 50000}, func(s *ProfileStats) int { return s.Reviewed }},
		{"edited", "polish", []int{50, 250, 1000, 5000, 20000}, func(s *ProfileStats) int { return s.Edited }},
		{"best_day", "bolt", []int{100, 300, 600, 1000, 2000}, func(s *ProfileStats) int { return s.BestDay }},
	}},
	{"reach", []BadgeDef{
		{"completed", "flag", []int{1, 5, 25, 100, 250}, func(s *ProfileStats) int { return s.CompletedFiles }},
		{"files", "layers", []int{5, 25, 100, 300, 1000}, func(s *ProfileStats) int { return s.Files }},
		{"projects", "grid", []int{1, 2, 3, 5, 10}, func(s *ProfileStats) int { return s.Projects }},
		{"languages", "globe", []int{2, 3, 5}, func(s *ProfileStats) int { return s.Languages }},
	}},
	{"community", []BadgeDef{
		{"comments", "chat", []int{10, 50, 200, 1000, 5000}, func(s *ProfileStats) int { return s.Comments }},
		{"topics", "topic", []int{1, 10, 50, 200}, func(s *ProfileStats) int { return s.Topics }},
		{"wiki", "book", []int{1, 10, 50, 200}, func(s *ProfileStats) int { return s.WikiEdits }},
		{"context", "note", []int{1, 10, 50, 200}, func(s *ProfileStats) int { return s.ContextEdits }},
		{"commits", "commit", []int{1, 10, 50, 200, 1000}, func(s *ProfileStats) int { return s.Commits }},
	}},
	{"habits", []BadgeDef{
		{"night", "moon", []int{50, 500, 2000}, func(s *ProfileStats) int { return s.Night }},
		{"early", "sun", []int{50, 500, 2000}, func(s *ProfileStats) int { return s.Early }},
		{"weekend", "weekend", []int{4, 12, 52}, func(s *ProfileStats) int { return s.Weekends }},
		{"veteran", "medal", []int{30, 365, 730, 1825}, func(s *ProfileStats) int { return s.MemberDays }},
	}},
}

// Badge is one badge as earned by a person
type Badge struct {
	Key, Icon string
	Tier      int // tiers earned (0 = none yet)
	Tiers     int // tiers the badge has
	TierName  string
	Value     int
	Target    int // the next tier's threshold, or the top one when all are earned
	Percent   int // progress towards Target
	Max       bool
	Steps     []int // all thresholds
}

// BadgeGroup is a row of badges on the page
type BadgeGroup struct {
	Key    string
	Badges []Badge
	Earned int
}

func evalBadge(d BadgeDef, s *ProfileStats) Badge {
	v := d.value(s)
	b := Badge{Key: d.Key, Icon: d.Icon, Value: v, Tiers: len(d.Tiers), Steps: d.Tiers}
	for _, t := range d.Tiers {
		if v >= t {
			b.Tier++
		}
	}
	if b.Tier > 0 {
		b.TierName = BadgeTierNames[b.Tier-1]
	}
	if b.Tier == len(d.Tiers) {
		b.Max, b.Target, b.Percent = true, d.Tiers[len(d.Tiers)-1], 100
		return b
	}
	b.Target = d.Tiers[b.Tier]
	from := 0
	if b.Tier > 0 {
		from = d.Tiers[b.Tier-1]
	}
	b.Percent = min(99, max(0, (v-from)*100/max(1, b.Target-from)))
	return b
}

// dayNum counts days since 1970-01-01 for a date at midnight UTC
func dayNum(t time.Time) int {
	return int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// runs returns the longest run of consecutive numbers in sorted, distinct ns, and the run that ends at
// last or last-1 (the current one: the period that is not over yet does not break it), with its start
func runs(ns []int, last int) (best, current, currentFrom int) {
	run, start := 0, 0
	for i, n := range ns {
		if i > 0 && n == ns[i-1]+1 {
			run++
		} else {
			run, start = 1, n
		}
		best = max(best, run)
		if n == last || n == last-1 {
			current, currentFrom = run, start
		}
	}
	return
}

// ComputeProfileStats derives streaks and counts from the days (oldest first) as of today (a date in
// the site's time zone)
func ComputeProfileStats(days []models.ProfileDay, totals *models.ProfileTotals, joined, today time.Time) ProfileStats {
	s := ProfileStats{}
	var dayNs, weekNs, monthNs []int
	weekends := map[int]bool{}
	for _, d := range days {
		if d.Contributions == 0 {
			continue
		}
		s.Contributions += d.Contributions
		s.Translated += d.Translated
		s.Words += d.Words
		s.Reviewed += d.Reviewed
		s.Edited += d.Edited
		s.Night += d.Night
		s.Early += d.Early
		s.ActiveDays++
		if d.Contributions > s.BestDay {
			s.BestDay, s.BestDayAt = d.Contributions, d.Day
		}
		n := dayNum(d.Day)
		dayNs = append(dayNs, n)
		week := (n + 3) / 7 // 1970-01-01 was a Thursday: weeks start on Monday
		if len(weekNs) == 0 || weekNs[len(weekNs)-1] != week {
			weekNs = append(weekNs, week)
		}
		month := d.Day.Year()*12 + int(d.Day.Month()) - 1
		if len(monthNs) == 0 || monthNs[len(monthNs)-1] != month {
			monthNs = append(monthNs, month)
		}
		if wd := d.Day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			weekends[week] = true
		}
	}
	s.Weekends = len(weekends)
	tn := dayNum(today)
	var from int
	s.BestStreakDays, s.StreakDays, from = runs(dayNs, tn)
	if s.StreakDays > 0 {
		s.StreakFrom = time.Unix(int64(from)*86400, 0).UTC()
	}
	s.BestStreakWeeks, s.StreakWeeks, _ = runs(weekNs, (tn+3)/7)
	s.BestStreakMonths, s.StreakMonths, _ = runs(monthNs, today.Year()*12+int(today.Month())-1)
	if !joined.IsZero() {
		s.MemberDays = max(0, tn-dayNum(joined))
	}
	if totals != nil {
		s.Files, s.Projects, s.Languages, s.CompletedFiles = totals.Files, len(totals.Projects), totals.Languages, totals.CompletedFiles
		s.Comments, s.Topics, s.WikiEdits, s.ContextEdits, s.Commits = totals.Comments, totals.Topics, totals.WikiEdits, totals.ContextEdits, totals.Commits
	}
	return s
}

// EvalBadges measures every badge on s
func EvalBadges(s *ProfileStats) (groups []BadgeGroup, earned, tiers int) {
	for _, g := range Badges {
		bg := BadgeGroup{Key: g.Key}
		for _, d := range g.Badges {
			b := evalBadge(d, s)
			bg.Badges = append(bg.Badges, b)
			bg.Earned += b.Tier
			earned += b.Tier
			tiers += b.Tiers
		}
		// earned badges first, highest tier first; the catalog order otherwise
		sort.SliceStable(bg.Badges, func(i, j int) bool {
			return bg.Badges[i].Tier > 0 && bg.Badges[j].Tier == 0
		})
		groups = append(groups, bg)
	}
	return
}

// Profile gathers a person's public profile; loc is the site's time zone
func (s *ActivityService) Profile(ctx context.Context, user *models.User, loc *time.Location, now time.Time) (*Profile, error) {
	days, err := s.repo.UserDays(ctx, user.ID, loc.String())
	if err != nil {
		return nil, err
	}
	totals, err := s.repo.UserTotals(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	joined := user.CreatedAt.In(loc)
	joined = time.Date(joined.Year(), joined.Month(), joined.Day(), 0, 0, 0, 0, time.UTC)
	p := &Profile{User: user, Days: days, Totals: totals, Today: today}
	p.Stats = ComputeProfileStats(days, totals, joined, today)
	p.Badges, p.Earned, p.Tiers = EvalBadges(&p.Stats)
	return p, nil
}

// FindBadge returns a badge of the catalog by key
func (p *Profile) FindBadge(key string) (Badge, bool) {
	for _, g := range p.Badges {
		for _, b := range g.Badges {
			if b.Key == key {
				return b, true
			}
		}
	}
	return Badge{}, false
}

// TierName is the name key of tier n (1-based)
func TierName(n int) string {
	if n < 1 || n > len(BadgeTierNames) {
		return ""
	}
	return BadgeTierNames[n-1]
}
