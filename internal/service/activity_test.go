package service

import (
	"context"
	"testing"
	"time"

	"pelokalan/internal/models"
)

type fakeActivityRepo struct{ events []*models.ActivityEvent }

func (f *fakeActivityRepo) Record(_ context.Context, e *models.ActivityEvent) error {
	f.events = append(f.events, e)
	return nil
}
func (f *fakeActivityRepo) Summary(context.Context, int64, *time.Time) ([]*models.ActivitySummary, error) {
	return nil, nil
}
func (f *fakeActivityRepo) Since(context.Context, int64) (*time.Time, error) { return nil, nil }
func (f *fakeActivityRepo) HourlyTranslated(context.Context, int64) ([]models.ActivityDay, error) {
	return nil, nil
}

func TestEntrySavedEvent(t *testing.T) {
	repo := &fakeActivityRepo{}
	a := NewActivityService(repo)
	u := &models.User{ID: 7}
	c := &models.Catalog{ID: 3, ProjectID: 2, Language: "id"}
	before := &models.Entry{ID: 9, MsgID: "Open the file", MsgStr: []string{"Buka berkas"}, Fuzzy: true, Origin: models.OriginAI}
	after := &models.Entry{ID: 9, MsgID: "Open the file", MsgStr: []string{"Buka berkas"}}
	a.EntrySaved(context.Background(), u, c, before, after)
	e := repo.events[0]
	if e.Kind != models.ActivityEntrySaved || e.StatusBefore != "fuzzy" || e.StatusAfter != "translated" ||
		e.Origin != "ai" || e.TextChanged || e.Words != 3 || *e.ProjectID != 2 || e.Language != "id" {
		t.Fatalf("unexpected event: %+v", e)
	}
	// a nil service records nothing and does not panic
	var none *ActivityService
	none.EntrySaved(context.Background(), u, c, before, after)
}
