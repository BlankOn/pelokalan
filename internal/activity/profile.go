package activity

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
	AI            int // AI drafts reviewed and saved
	Manual        int // empty strings translated by hand
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
	MemberMonths int // whole months since signing up

	Files, Projects, Languages, CompletedFiles         int
	Comments, Topics, WikiEdits, ContextEdits, Commits int
	Landed, LandedModules                              int // accepted upstream
}

// BadgeDef is one badge with its tiers: thresholds of one number, lowest first
type BadgeDef struct {
	ID    string // badge.<id> (name), badge.<key>_desc (what it takes, with the target)
	Icon  string // badge_icon partial
	Tiers []int
	value func(*ProfileStats) int
	Div   int    // descriptions show thresholds divided by this (months → years); 0 = as they are
	Unit  string // badges.unit_<unit> after the progress count ("7 / 12 bulan"); "" = none
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
		{ID: "streak_day", Icon: "flame", Tiers: []int{3, 7, 30, 100, 365}, value: func(s *ProfileStats) int { return s.BestStreakDays }},
		{ID: "streak_week", Icon: "week", Tiers: []int{4, 12, 26, 52, 104}, value: func(s *ProfileStats) int { return s.BestStreakWeeks }},
		{ID: "streak_month", Icon: "month", Tiers: []int{3, 6, 12, 24, 48}, value: func(s *ProfileStats) int { return s.BestStreakMonths }},
		{ID: "active_days", Icon: "calendar", Tiers: []int{10, 50, 100, 365, 1000}, value: func(s *ProfileStats) int { return s.ActiveDays }},
	}},
	{"work", []BadgeDef{
		{ID: "translated", Icon: "pen", Tiers: []int{100, 1000, 5000, 20000, 100000}, value: func(s *ProfileStats) int { return s.Translated }},
		{ID: "words", Icon: "type", Tiers: []int{1000, 10000, 50000, 250000, 1000000}, value: func(s *ProfileStats) int { return s.Words }},
		{ID: "reviewed", Icon: "check", Tiers: []int{50, 500, 2500, 10000, 50000}, value: func(s *ProfileStats) int { return s.Reviewed }},
		{ID: "edited", Icon: "polish", Tiers: []int{50, 250, 1000, 5000, 20000}, value: func(s *ProfileStats) int { return s.Edited }},
		{ID: "manual", Icon: "keyboard", Tiers: []int{50, 500, 2500, 10000, 50000}, value: func(s *ProfileStats) int { return s.Manual }},
		{ID: "ai", Icon: "robot", Tiers: []int{25, 250, 1000, 5000, 20000}, value: func(s *ProfileStats) int { return s.AI }},
		{ID: "best_day", Icon: "bolt", Tiers: []int{100, 300, 600, 1000, 2000}, value: func(s *ProfileStats) int { return s.BestDay }},
	}},
	{"upstream", []BadgeDef{
		{ID: "landed", Icon: "merge", Tiers: []int{10, 100, 1000, 5000, 20000}, value: func(s *ProfileStats) int { return s.Landed }},
		{ID: "landed_modules", Icon: "upload", Tiers: []int{1, 5, 25, 100, 250}, value: func(s *ProfileStats) int { return s.LandedModules }},
	}},
	{"reach", []BadgeDef{
		{ID: "completed", Icon: "flag", Tiers: []int{1, 5, 25, 100, 250}, value: func(s *ProfileStats) int { return s.CompletedFiles }},
		{ID: "files", Icon: "layers", Tiers: []int{5, 25, 100, 300, 1000}, value: func(s *ProfileStats) int { return s.Files }},
		{ID: "projects", Icon: "grid", Tiers: []int{1, 2, 3, 5, 10}, value: func(s *ProfileStats) int { return s.Projects }},
		{ID: "languages", Icon: "globe", Tiers: []int{2, 3, 5}, value: func(s *ProfileStats) int { return s.Languages }},
	}},
	{"community", []BadgeDef{
		{ID: "comments", Icon: "chat", Tiers: []int{10, 50, 200, 1000, 5000}, value: func(s *ProfileStats) int { return s.Comments }},
		{ID: "topics", Icon: "topic", Tiers: []int{1, 10, 50, 200}, value: func(s *ProfileStats) int { return s.Topics }},
		{ID: "wiki", Icon: "book", Tiers: []int{1, 10, 50, 200}, value: func(s *ProfileStats) int { return s.WikiEdits }},
		{ID: "context", Icon: "note", Tiers: []int{1, 10, 50, 200}, value: func(s *ProfileStats) int { return s.ContextEdits }},
		{ID: "commits", Icon: "commit", Tiers: []int{1, 10, 50, 200, 1000}, value: func(s *ProfileStats) int { return s.Commits }},
	}},
	{"habits", []BadgeDef{
		{ID: "night", Icon: "moon", Tiers: []int{50, 500, 2000}, value: func(s *ProfileStats) int { return s.Night }},
		{ID: "early", Icon: "sun", Tiers: []int{50, 500, 2000}, value: func(s *ProfileStats) int { return s.Early }},
		{ID: "weekend", Icon: "weekend", Tiers: []int{4, 12, 52}, value: func(s *ProfileStats) int { return s.Weekends }},
		// a member for N years with work in every month of an unbroken run of N years (counted in months)
		{ID: "veteran", Icon: "medal", Tiers: []int{12, 24, 36, 60}, Div: 12, Unit: "months", value: func(s *ProfileStats) int {
			return min(s.BestStreakMonths, s.MemberMonths)
		}},
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
	Steps     []int  // all thresholds
	Div       int    // see BadgeDef.Div (at least 1)
	Unit      string // see BadgeDef.Unit
}

// Shown is a threshold as the descriptions say it (BadgeDef.Div)
func (b Badge) Shown(v int) int { return v / max(1, b.Div) }

// ShownTarget is the next (or top) threshold as the descriptions say it
func (b Badge) ShownTarget() int { return b.Shown(b.Target) }

// BadgeGroup is a row of badges on the page
type BadgeGroup struct {
	Key    string
	Badges []Badge
	Earned int
}

func evalBadge(d BadgeDef, s *ProfileStats) Badge {
	v := d.value(s)
	b := Badge{Key: d.ID, Icon: d.Icon, Value: v, Tiers: len(d.Tiers), Steps: d.Tiers, Div: max(1, d.Div), Unit: d.Unit}
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
		s.AI += d.AI
		s.Manual += d.Manual
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
		m := (today.Year()-joined.Year())*12 + int(today.Month()) - int(joined.Month())
		if today.Day() < joined.Day() {
			m--
		}
		s.MemberMonths = max(0, m)
	}
	if totals != nil {
		s.Files, s.Projects, s.Languages, s.CompletedFiles = totals.Files, len(totals.Projects), totals.Languages, totals.CompletedFiles
		s.Comments, s.Topics, s.WikiEdits, s.ContextEdits, s.Commits = totals.Comments, totals.Topics, totals.WikiEdits, totals.ContextEdits, totals.Commits
		s.Landed, s.LandedModules = totals.Landed, totals.LandedModules
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
func (s *Service) Profile(ctx context.Context, user *models.User, loc *time.Location, now time.Time) (*Profile, error) {
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
