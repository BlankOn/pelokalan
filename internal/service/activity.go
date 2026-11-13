package service

import (
	"context"
	"log"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// ActivityService records what people do (activity_events) and reports it on the Insight tab.
// Recording never fails the action itself: errors are only logged.
type ActivityService struct {
	repo repository.ActivityRepository
}

// NewActivityService creates the activity service
func NewActivityService(repo repository.ActivityRepository) *ActivityService {
	return &ActivityService{repo: repo}
}

// ActivityRanges are the periods the Insight tab offers (days; 0 = all time)
var ActivityRanges = []struct {
	Key  string
	Days int
}{{"7", 7}, {"30", 30}, {"90", 90}, {"all", 0}}

// Record stores an event; a nil service (tests, tools) records nothing
func (s *ActivityService) Record(ctx context.Context, e *models.ActivityEvent) {
	if s == nil || s.repo == nil {
		return
	}
	if err := s.repo.Record(context.WithoutCancel(ctx), e); err != nil {
		log.Printf("activity: record %s: %v", e.Kind, err)
	}
}

// EntrySaved records a person saving an entry: before is the entry as it was, after as saved
func (s *ActivityService) EntrySaved(ctx context.Context, user *models.User, c *models.Catalog, before, after *models.Entry) {
	if before == nil || after == nil {
		return
	}
	src := before.MsgID
	if before.MsgIDPlural != "" {
		src += " " + before.MsgIDPlural
	}
	s.Record(ctx, &models.ActivityEvent{
		Kind: models.ActivityEntrySaved, UserID: &user.ID, ProjectID: &c.ProjectID, CatalogID: &c.ID, EntryID: &before.ID,
		Language: c.Language, StatusBefore: before.Status(), StatusAfter: after.Status(), Origin: before.Origin,
		TextChanged: strings.Join(before.MsgStr, "\x00") != strings.Join(after.MsgStr, "\x00"),
		Words:       len(strings.Fields(src)),
	})
}

// Summary returns each member's activity in the project over the range (ActivityRanges key)
// and when recording started for it
func (s *ActivityService) Summary(ctx context.Context, p *models.Project, rangeKey string) ([]*models.ActivitySummary, *time.Time, error) {
	var since *time.Time
	for _, r := range ActivityRanges {
		if r.Key == rangeKey && r.Days > 0 {
			t := time.Now().AddDate(0, 0, -r.Days)
			since = &t
		}
	}
	rows, err := s.repo.Summary(ctx, p.ID, since)
	if err != nil {
		return nil, nil, err
	}
	first, err := s.repo.Since(ctx, p.ID)
	return rows, first, err
}

// HourlyTranslated returns the project's translation output per hour, oldest first
func (s *ActivityService) HourlyTranslated(ctx context.Context, p *models.Project) ([]models.ActivityDay, error) {
	return s.repo.HourlyTranslated(ctx, p.ID)
}
