package activity

import (
	"context"
	"time"

	"pelokalan/internal/models"
)

// ActivityRepository stores the activity log (activity_events)
type Repository interface {
	Record(ctx context.Context, e *models.ActivityEvent) error
	// Summary adds up each user's activity in a project since the given time (nil = all time),
	// most active first
	Summary(ctx context.Context, projectID int64, since *time.Time) ([]*models.ActivitySummary, error)
	// HourlyTranslated returns the strings people saved with a translation per hour in a project (typed by
	// hand, AI drafts saved unchanged or edited, existing translations edited), oldest first (Day = the hour)
	HourlyTranslated(ctx context.Context, projectID int64) ([]models.ActivityDay, error)
	// Since returns the time of the project's first recorded event (nil when there is none)
	Since(ctx context.Context, projectID int64) (*time.Time, error)
	// UserDays returns a person's work per calendar day in time zone tz (an IANA name), oldest first
	UserDays(ctx context.Context, userID int64, tz string) ([]models.ProfileDay, error)
	// UserTotals counts a person's projects, files, languages and community work
	UserTotals(ctx context.Context, userID int64) (*models.ProfileTotals, error)
	// UserModuleWork adds up a person's translation work of one kind ("contrib", "new", "words", "reviewed",
	// "edited", "night", "early", "weekend"; days in time zone tz) per project and module between from and to
	// (nil = no bound), most first
	UserModuleWork(ctx context.Context, userID int64, tz, kind string, from, to *time.Time, limit int) ([]*models.ModuleWork, error)
	// UserLandingWork lists a person's strings accepted upstream per project and module, most first
	UserLandingWork(ctx context.Context, userID int64, limit int) ([]*models.ModuleWork, error)
	// UserCommunityWork counts a person's community work of one kind ("comments", "topics", "wiki", "context",
	// "commits") per project, most first
	UserCommunityWork(ctx context.Context, userID int64, kind string, limit int) ([]*models.ModuleWork, error)
	// NthTime is when a person's nth item of a kind ("landed", "landed_modules", or a community kind) happened
	NthTime(ctx context.Context, userID int64, kind string, n int) (*time.Time, error)
	// BadgeUsers lists the people who have done any work that badges count (translation, upstream, community)
	BadgeUsers(ctx context.Context) ([]int64, error)
	// AwardedTiers returns the highest tier awarded per badge to a person
	AwardedTiers(ctx context.Context, userID int64) (map[string]int, error)
	// AddAwards records awarded badge tiers (already recorded ones are skipped)
	AddAwards(ctx context.Context, userID int64, awards []models.BadgeAward, emailed bool) error
	// ClaimOneTimeJob marks a one-time job as done; false when it already was (one_time_jobs)
	ClaimOneTimeJob(ctx context.Context, name string) (bool, error)
	// AddLandings stores strings accepted upstream; ones already stored are skipped. It returns how many are new.
	AddLandings(ctx context.Context, landings []models.UpstreamLanding) (int, error)
}
