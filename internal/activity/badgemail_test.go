package activity

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/mailer"
)

// badgeRepo: one person with 12 comments (Teman Diskusi, bronze at 10), awards kept in memory
type badgeRepo struct {
	fakeActivityRepo
	comments int
	awards   map[string]int
	emailed  []bool
}

func (f *badgeRepo) BadgeUsers(context.Context) ([]int64, error) { return []int64{7}, nil }
func (f *badgeRepo) UserTotals(context.Context, int64) (*models.ProfileTotals, error) {
	return &models.ProfileTotals{Comments: f.comments}, nil
}
func (f *badgeRepo) AwardedTiers(context.Context, int64) (map[string]int, error) {
	out := map[string]int{}
	for k, v := range f.awards {
		out[k] = v
	}
	return out, nil
}
func (f *badgeRepo) AddAwards(_ context.Context, _ int64, a []models.BadgeAward, emailed bool) error {
	for _, x := range a {
		f.awards[x.Badge] = max(f.awards[x.Badge], x.Tier)
	}
	f.emailed = append(f.emailed, emailed)
	return nil
}

// oneUser is a UserGetter that knows one person
type oneUser struct {
	u *models.User
}

func (o oneUser) GetByID(context.Context, int64) (*models.User, error) { return o.u, nil }

type sentMail struct{ msgs []*mailer.Message }

func (m *sentMail) Send(_ context.Context, msg *mailer.Message) error {
	m.msgs = append(m.msgs, msg)
	return nil
}

func TestAwardBadges(t *testing.T) {
	repo := &badgeRepo{comments: 12, awards: map[string]int{}}
	user := &models.User{ID: 7, Email: "ani@example.com", Username: "ani", Name: "Ani", Lang: "id"}
	mail := &sentMail{}
	s := New(repo)
	s.SetBadgeMail(oneUser{u: user}, mail, config.SiteConfig{BaseURL: "https://pelokalan.example", Name: "Pelokalan"}, nil)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	// the baseline records what is there without email
	if n, err := s.AwardBadges(context.Background(), time.UTC, now, false); err != nil || n != 0 || len(mail.msgs) != 0 {
		t.Fatalf("baseline: n=%d err=%v mails=%d", n, err, len(mail.msgs))
	}
	if repo.awards["comments"] != 1 || repo.emailed[0] {
		t.Fatalf("baseline awards %v emailed %v", repo.awards, repo.emailed)
	}
	// nothing new: no email
	if n, _ := s.AwardBadges(context.Background(), time.UTC, now, true); n != 0 || len(mail.msgs) != 0 {
		t.Fatalf("nothing new but %d emails", len(mail.msgs))
	}
	// 50 comments reach silver: one email with the badge and share links
	repo.comments = 50
	if n, _ := s.AwardBadges(context.Background(), time.UTC, now, true); n != 1 || len(mail.msgs) != 1 {
		t.Fatalf("silver: n=%d mails=%d", n, len(mail.msgs))
	}
	m := mail.msgs[0]
	for _, want := range []string{"Teman Diskusi", "Perak", "/users/ani/badges/comments/2", "linkedin.com/sharing", "twitter.com/intent/tweet",
		"facebook.com/sharer", "/profile#badge-emails"} {
		if !strings.Contains(m.Subject+m.Text+m.HTML, want) {
			t.Errorf("email lacks %q", want)
		}
	}
	if m.To != "ani@example.com" || m.Category != "badge" {
		t.Errorf("email to %q category %q", m.To, m.Category)
	}
	if out := os.Getenv("BADGEMAIL_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(m.HTML), 0o644)
	}
	// the same tier is never announced twice, even after the count drops and rises again
	repo.comments = 40
	s.AwardBadges(context.Background(), time.UTC, now, true)
	repo.comments = 60
	if s.AwardBadges(context.Background(), time.UTC, now, true); len(mail.msgs) != 1 {
		t.Errorf("silver announced again: %d emails", len(mail.msgs))
	}
	// switched off: gold is recorded but not emailed
	user.BadgeEmailsOff = true
	repo.comments = 200
	s.AwardBadges(context.Background(), time.UTC, now, true)
	if len(mail.msgs) != 1 || repo.awards["comments"] != 3 || repo.emailed[len(repo.emailed)-1] {
		t.Errorf("emails off: mails=%d awards=%v emailed=%v", len(mail.msgs), repo.awards, repo.emailed)
	}
}
