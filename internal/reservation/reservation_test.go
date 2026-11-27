package reservation

import (
	"context"
	"testing"
	"time"

	"pelokalan/internal/account"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/mailer"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/project"
)

// memReservations keeps reservations in memory with the same rules as the table (one holder per module)
type memReservations struct {
	next     int64
	active   map[string]*models.ModuleReservation // project/module
	released map[int64]string                     // id → reason
	reminded map[int64]bool
	lastSave map[string]time.Time
}

func newMemReservations() *memReservations {
	return &memReservations{active: map[string]*models.ModuleReservation{}, released: map[int64]string{},
		reminded: map[int64]bool{}, lastSave: map[string]time.Time{}}
}

func key(projectID int64, module string) string { return string(rune(projectID)) + "/" + module }

func (m *memReservations) Active(_ context.Context, projectID int64) ([]*models.ModuleReservation, error) {
	var out []*models.ModuleReservation
	for _, r := range m.active {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memReservations) ActiveFor(_ context.Context, projectID int64, module string) (*models.ModuleReservation, error) {
	if r, ok := m.active[key(projectID, module)]; ok {
		if t, ok := m.lastSave[module]; ok && t.After(r.ReservedAt) {
			r.LastSave = t
		}
		return r, nil
	}
	return nil, store.ErrNotFound
}
func (m *memReservations) Create(_ context.Context, projectID int64, module string, userID int64) error {
	if _, ok := m.active[key(projectID, module)]; ok {
		return store.ErrConflict
	}
	m.next++
	now := time.Now()
	m.active[key(projectID, module)] = &models.ModuleReservation{ID: m.next, ProjectID: projectID, Module: module, UserID: userID,
		Username: map[int64]string{1: "ani", 2: "budi", 3: "citra"}[userID], ReservedAt: now, LastSave: now}
	return nil
}
func (m *memReservations) Release(_ context.Context, id int64, _ *int64, reason string) error {
	for k, r := range m.active {
		if r.ID == id {
			delete(m.active, k)
			m.released[id] = reason
		}
	}
	return nil
}
func (m *memReservations) Expiring(ctx context.Context) ([]*models.ModuleReservation, error) {
	var out []*models.ModuleReservation
	for _, r := range m.active {
		if t, ok := m.lastSave[r.Module]; ok {
			r.LastSave = t
		}
		out = append(out, r)
	}
	return out, nil
}
func (m *memReservations) SetReminded(_ context.Context, id int64) error {
	m.reminded[id] = true
	for _, r := range m.active {
		if r.ID == id {
			now := time.Now()
			r.RemindedAt = &now
		}
	}
	return nil
}

type oneProject struct {
	project.Repository
	p *models.Project
}

func (o oneProject) GetByID(context.Context, int64) (*models.Project, error) { return o.p, nil }

type someUsers struct{ account.UserRepository }

func (someUsers) GetByID(_ context.Context, id int64) (*models.User, error) {
	return &models.User{ID: id, Email: "u@example.com", Name: "U", Lang: "id"}, nil
}

func TestReservations(t *testing.T) {
	ctx := context.Background()
	ani, budi := &models.User{ID: 1}, &models.User{ID: 2}
	p := &models.Project{ID: 9, Name: "GNOME", Slug: "gnome-id", Reservations: true, ReservationDays: 30}
	mem := newMemReservations()
	mail := &sentMail{}
	s := New(mem, oneProject{p: p}, someUsers{}, mail, config.SiteConfig{BaseURL: "https://x", Name: "Pelokalan"})

	// off: nothing is held or refused
	p.Reservations = false
	if err := s.Claim(ctx, ani, p, "gtk"); err != nil || len(mem.active) != 0 {
		t.Fatalf("off: %v %d", err, len(mem.active))
	}
	p.Reservations = true

	// Ani claims; claiming again is fine; Budi is refused, also by Check
	if err := s.Claim(ctx, ani, p, "gtk"); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, ani, p, "gtk"); err != nil || len(mem.active) != 1 {
		t.Fatalf("again: %v %d", err, len(mem.active))
	}
	if err := s.Claim(ctx, budi, p, "gtk"); err != ErrModuleReserved {
		t.Fatalf("budi claim: %v", err)
	}
	if err := s.Check(ctx, budi, p, "gtk"); err != ErrModuleReserved {
		t.Fatalf("budi check: %v", err)
	}
	if err := s.Check(ctx, ani, p, "gtk"); err != nil {
		t.Fatalf("ani check: %v", err)
	}
	// a translator may not release someone else's; a manager may, and the holder is emailed
	p.Role = models.RoleTranslator
	if err := s.Release(ctx, budi, p, "gtk"); err != apperr.ErrForbidden {
		t.Fatalf("translator release: %v", err)
	}
	p.Role = models.RoleManager
	if err := s.Release(ctx, budi, p, "gtk"); err != nil || len(mem.active) != 0 || mem.released[1] != models.ReleaseManager || len(mail.msgs) != 1 {
		t.Fatalf("manager release: %v active=%d reason=%q mails=%d", err, len(mem.active), mem.released[1], len(mail.msgs))
	}
	// the holder releasing their own: no email
	_ = s.Claim(ctx, budi, p, "gtk")
	if err := s.Release(ctx, budi, p, "gtk"); err != nil || mem.released[2] != models.ReleaseHolder || len(mail.msgs) != 1 {
		t.Fatalf("holder release: %v %q %d", err, mem.released[2], len(mail.msgs))
	}

	// merged: only the holder's module is released
	_ = s.Claim(ctx, ani, p, "glib")
	s.ReleaseMerged(ctx, p.ID, "glib", budi.ID) // not Budi's
	if len(mem.active) != 1 {
		t.Fatal("released for the wrong holder")
	}
	s.ReleaseMerged(ctx, p.ID, "glib", ani.ID)
	if len(mem.active) != 0 || mem.released[3] != models.ReleaseMerged || len(mail.msgs) != 2 {
		t.Fatalf("merged: active=%d reason=%q mails=%d", len(mem.active), mem.released[3], len(mail.msgs))
	}

	// expiry: reminded 3 days before (once until a new save), released after 30 days without saves
	_ = s.Claim(ctx, ani, p, "orca")
	now := time.Now()
	mem.lastSave["orca"] = now.AddDate(0, 0, -20)
	if n, _ := s.Expire(ctx, now); n != 0 || len(mail.msgs) != 2 {
		t.Fatalf("20 days: released %d, mails %d", n, len(mail.msgs))
	}
	mem.lastSave["orca"] = now.AddDate(0, 0, -28)
	s.Expire(ctx, now)
	s.Expire(ctx, now)
	if len(mail.msgs) != 3 {
		t.Fatalf("28 days: want one reminder, mails %d", len(mail.msgs))
	}
	mem.lastSave["orca"] = now.AddDate(0, 0, -31)
	if n, _ := s.Expire(ctx, now); n != 1 || len(mem.active) != 0 || len(mail.msgs) != 4 {
		t.Fatalf("31 days: released %d active %d mails %d", n, len(mem.active), len(mail.msgs))
	}
	// never expires when the project says so
	p.ReservationDays = 0
	_ = s.Claim(ctx, ani, p, "orca")
	mem.lastSave["orca"] = now.AddDate(-1, 0, 0)
	if n, _ := s.Expire(ctx, now); n != 0 {
		t.Fatal("expired although the project never expires reservations")
	}
}

// sentMail keeps the emails a test sends
type sentMail struct{ msgs []*mailer.Message }

func (m *sentMail) Send(_ context.Context, msg *mailer.Message) error {
	m.msgs = append(m.msgs, msg)
	return nil
}
