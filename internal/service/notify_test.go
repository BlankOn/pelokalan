package service

import (
	"testing"

	"pelokalan/internal/models"
)

func TestNotifyReasonsAndSettings(t *testing.T) {
	s := &DiscussionService{}
	me := &models.User{ID: 7}
	starter := int64(7)
	d := &models.Discussion{CreatedBy: &starter}
	entry := &Subject{Type: models.SubjectPOEntry}

	got := s.reasons(me, entry, d, 7, map[int64]bool{7: true}, false, true)
	want := []string{models.NotifyMention, models.NotifyMyFile, models.NotifyStarted, models.NotifyParticipated, models.NotifyOtherEntry}
	if len(got) != len(want) {
		t.Fatalf("reasons = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reasons = %v, want %v", got, want)
		}
	}

	topic := &Subject{Type: models.SubjectTopic}
	other := &models.Discussion{}
	if r := s.reasons(me, topic, other, 0, nil, true, false); len(r) != 2 || r[0] != models.NotifyNewTopic || r[1] != models.NotifyOtherTopic {
		t.Errorf("new topic reasons = %v", r)
	}

	// defaults: everything on; switching one off keeps the others; muted stops everything
	var none *models.NotificationSettings
	if !none.Wants(models.NotifyOtherGlossary) {
		t.Error("default should send")
	}
	n := &models.NotificationSettings{Off: map[string]bool{models.NotifyOtherGlossary: true}}
	if n.Wants(models.NotifyOtherGlossary) || !n.Wants(models.NotifyMention) {
		t.Error("only the switched-off kind stops")
	}
	n.Muted = true
	if n.Wants(models.NotifyMention) {
		t.Error("muted sends nothing")
	}
}
