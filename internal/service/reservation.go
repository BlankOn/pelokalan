package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/repository"
)

// Module reservations. When a project turns them on (Settings → Umum), a member holds a module (file name
// before the first dot, ModuleOf) while working on it: the first change to their copy claims it, or "Ambil &
// pesan" in Berkas proyek. Other members cannot change their copies of a held module (hard lock: Check). The
// holder or an owner/manager releases it; it is also released when all the holder's changes are upstream
// (ReleaseMerged, after each sync) and after ReservationDays without saves (Expire, hourly, with a reminder
// ReminderDays before). The holder gets an email whenever someone or something else releases it.

// ErrModuleReserved: another member holds the module
var ErrModuleReserved = userErr("err.module_reserved")

// ReservationDays are the expiry choices in Settings (0 = never)
var ReservationDays = []int{0, 14, 30, 60}

// ReminderDays is how long before expiry the holder is reminded
const ReminderDays = 3

// ReservationService manages module reservations
type ReservationService struct {
	repo     repository.ReservationRepository
	projects repository.ProjectRepository
	users    repository.UserRepository
	mailer   Mailer
	site     config.SiteConfig
}

// NewReservationService creates the reservation service
func NewReservationService(repo repository.ReservationRepository, projects repository.ProjectRepository,
	users repository.UserRepository, mailer Mailer, site config.SiteConfig) *ReservationService {
	return &ReservationService{repo: repo, projects: projects, users: users, mailer: mailer, site: site}
}

// Map returns a project's held modules by module (nil when reservations are off)
func (s *ReservationService) Map(ctx context.Context, p *models.Project) map[string]*models.ModuleReservation {
	if s == nil || p == nil || !p.Reservations {
		return nil
	}
	list, err := s.repo.Active(ctx, p.ID)
	if err != nil {
		log.Printf("reservations: project %d: %v", p.ID, err)
		return nil
	}
	out := make(map[string]*models.ModuleReservation, len(list))
	for _, r := range list {
		out[r.Module] = r
	}
	return out
}

// Holder returns who holds a module (nil when it is free or reservations are off)
func (s *ReservationService) Holder(ctx context.Context, p *models.Project, module string) *models.ModuleReservation {
	if s == nil || p == nil || !p.Reservations || module == "" {
		return nil
	}
	r, err := s.repo.ActiveFor(ctx, p.ID, module)
	if err != nil {
		return nil
	}
	return r
}

// Check refuses when another member holds the module
func (s *ReservationService) Check(ctx context.Context, user *models.User, p *models.Project, module string) error {
	if h := s.Holder(ctx, p, module); h != nil && (user == nil || h.UserID != user.ID) {
		return ErrModuleReserved
	}
	return nil
}

// Claim reserves the module for user when it is free (and reservations are on); it refuses when another
// member holds it. Holding it already is fine.
func (s *ReservationService) Claim(ctx context.Context, user *models.User, p *models.Project, module string) error {
	if s == nil || p == nil || !p.Reservations || module == "" || user == nil {
		return nil
	}
	if err := s.Check(ctx, user, p, module); err != nil {
		return err
	}
	if s.Holder(ctx, p, module) != nil {
		return nil // theirs already
	}
	err := s.repo.Create(ctx, p.ID, module, user.ID)
	if errors.Is(err, repository.ErrConflict) { // someone was quicker
		return s.Check(ctx, user, p, module)
	}
	return err
}

// Release ends a reservation: the holder, or an owner/manager (p.Role); the holder gets an email when someone
// else released it
func (s *ReservationService) Release(ctx context.Context, user *models.User, p *models.Project, module string) error {
	h := s.Holder(ctx, p, module)
	if h == nil {
		return nil
	}
	reason := models.ReleaseHolder
	if h.UserID != user.ID {
		if !p.Role.CanManage() {
			return ErrForbidden
		}
		reason = models.ReleaseManager
	}
	if err := s.repo.Release(ctx, h.ID, &user.ID, reason); err != nil {
		return err
	}
	if reason == models.ReleaseManager {
		s.notify(ctx, p, h, "manager", user.DisplayName())
	}
	return nil
}

// ReleaseMerged releases a module held by holderID because all their changes to it are upstream now
func (s *ReservationService) ReleaseMerged(ctx context.Context, projectID int64, module string, holderID int64) {
	if s == nil {
		return
	}
	p, err := s.projects.GetByID(ctx, projectID)
	if err != nil {
		return
	}
	h := s.Holder(ctx, p, module)
	if h == nil || h.UserID != holderID {
		return
	}
	if err := s.repo.Release(ctx, h.ID, nil, models.ReleaseMerged); err != nil {
		log.Printf("reservations: release merged %s: %v", module, err)
		return
	}
	s.notify(ctx, p, h, "merged", "")
}

// ReleasePushed releases a module held by holderID after Pelokalan committed their file to the repository
// (the review email already tells them)
func (s *ReservationService) ReleasePushed(ctx context.Context, projectID int64, module string, holderID int64) {
	if s == nil {
		return
	}
	p, err := s.projects.GetByID(ctx, projectID)
	if err != nil {
		return
	}
	if h := s.Holder(ctx, p, module); h != nil && h.UserID == holderID {
		if err := s.repo.Release(ctx, h.ID, nil, models.ReleaseMerged); err != nil {
			log.Printf("reservations: release pushed %s: %v", module, err)
		}
	}
}

// Expire reminds holders whose reservation ends within ReminderDays and releases those past the project's
// ReservationDays without saves. It returns how many it released.
func (s *ReservationService) Expire(ctx context.Context, now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	list, err := s.repo.Expiring(ctx)
	if err != nil {
		return 0, err
	}
	projects := map[int64]*models.Project{}
	released := 0
	for _, r := range list {
		p, ok := projects[r.ProjectID]
		if !ok {
			if p, err = s.projects.GetByID(ctx, r.ProjectID); err != nil {
				continue
			}
			projects[r.ProjectID] = p
		}
		if !p.Reservations || p.ReservationDays <= 0 {
			continue
		}
		ends := r.LastSave.AddDate(0, 0, p.ReservationDays)
		switch {
		case !now.Before(ends):
			if err := s.repo.Release(ctx, r.ID, nil, models.ReleaseExpired); err != nil {
				return released, err
			}
			released++
			s.notify(ctx, p, r, "expired", "")
		case ends.Sub(now) <= ReminderDays*24*time.Hour && (r.RemindedAt == nil || r.RemindedAt.Before(r.LastSave)):
			if err := s.repo.SetReminded(ctx, r.ID); err != nil {
				return released, err
			}
			s.notify(ctx, p, r, "reminder", "")
		}
	}
	return released, nil
}

// notify emails the holder about their reservation: kind manager (released by who), merged, expired,
// reminder
func (s *ReservationService) notify(ctx context.Context, p *models.Project, r *models.ModuleReservation, kind, who string) {
	if s.mailer == nil || s.users == nil {
		return
	}
	u, err := s.users.GetByID(ctx, r.UserID)
	if err != nil {
		return
	}
	lang := i18n.Normalize(u.Lang)
	base := strings.TrimRight(s.site.BaseURL, "/")
	link := base + p.Path() + "/files"
	subject := i18n.T(lang, fmt.Sprintf("mail.reserve_%s_subject", kind), r.Module, p.Name)
	var body string
	switch kind {
	case "manager":
		body = i18n.T(lang, "mail.reserve_manager", r.Module, p.Name, who)
	case "reminder":
		body = i18n.T(lang, "mail.reserve_reminder", r.Module, p.Name, p.ReservationDays, ReminderDays)
	case "expired":
		body = i18n.T(lang, "mail.reserve_expired", r.Module, p.Name, p.ReservationDays)
	default:
		body = i18n.T(lang, "mail.reserve_merged", r.Module, p.Name)
	}
	hello := i18n.T(lang, "mail.badge_hello", u.DisplayName())
	button := i18n.T(lang, "mail.reserve_button")
	text := fmt.Sprintf("%s\n\n%s\n\n%s\n\n— %s\n", hello, body, link, s.site.Name)
	htmlBody := fmt.Sprintf(`<meta charset="utf-8"><div style="font-family:sans-serif;max-width:560px;color:#1d1f24"><p>%s</p><p>%s</p><p>%s</p>
<p style="color:#888;font-size:12px">%s · %s</p></div>`,
		html.EscapeString(hello), html.EscapeString(body), emailButton(link, button),
		html.EscapeString(s.site.Name), html.EscapeString(i18n.T(lang, "mail.reserve_footer", p.Name)))
	if err := s.mailer.Send(ctx, &Message{To: u.Email, Subject: subject, Text: text, HTML: htmlBody, Category: "reservation"}); err != nil {
		log.Printf("reservations: %s email to user %d: %v", kind, u.ID, err)
	}
}
