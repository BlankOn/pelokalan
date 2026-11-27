package activity

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/platform/mailer"
)

// New badges by email. Badges are computed from the activity, so nothing marks the moment one is earned:
// AwardBadges (hourly, main.go) computes everyone's badges, compares them with the tiers already recorded
// in badge_awards and emails each person the tiers that are new, with share links. The first run after
// the feature shipped (BadgeAwardsBaselineJob) records everything silently, so nobody gets a pile of
// old badges. A tier is announced once, even when its count later drops and rises again.

// BadgeAwardsBaselineJob names the silent first run in one_time_jobs
const BadgeAwardsBaselineJob = "badge-awards-baseline"

type badgeMail struct {
	users   UserGetter
	mailer  mailer.Mailer
	site    config.SiteConfig
	allowed func(email string) bool
}

// UserGetter is what badge emails need from the user store
type UserGetter interface {
	GetByID(ctx context.Context, id int64) (*models.User, error)
}

// SetBadgeMail lets AwardBadges look people up and email them; allowed is the sign-in whitelist
func (s *Service) SetBadgeMail(users UserGetter, mailer mailer.Mailer, site config.SiteConfig, allowed func(string) bool) {
	s.mail = &badgeMail{users: users, mailer: mailer, site: site, allowed: allowed}
}

// AwardBadges records the badge tiers people reached since the last run and, when notify is set, emails
// each person about theirs (unless they turned it off). It returns how many people got an email.
func (s *Service) AwardBadges(ctx context.Context, loc *time.Location, now time.Time, notify bool) (int, error) {
	if s == nil || s.mail == nil {
		return 0, nil
	}
	ids, err := s.repo.BadgeUsers(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, id := range ids {
		u, err := s.mail.users.GetByID(ctx, id)
		if err != nil {
			continue
		}
		prof, err := s.Profile(ctx, u, loc, now)
		if err != nil {
			log.Printf("badges: profile of user %d: %v", id, err)
			continue
		}
		have, err := s.repo.AwardedTiers(ctx, id)
		if err != nil {
			return sent, err
		}
		var awards []models.BadgeAward
		var fresh []Badge // badges with a new tier (their current tier is what the email shows)
		for _, g := range prof.Badges {
			for _, b := range g.Badges {
				if b.Tier <= have[b.Key] {
					continue
				}
				for t := have[b.Key] + 1; t <= b.Tier; t++ {
					awards = append(awards, models.BadgeAward{Badge: b.Key, Tier: t})
				}
				fresh = append(fresh, b)
			}
		}
		if len(awards) == 0 {
			continue
		}
		email := notify && !u.BadgeEmailsOff && (s.mail.allowed == nil || s.mail.allowed(u.Email))
		if err := s.repo.AddAwards(ctx, id, awards, email); err != nil {
			return sent, err
		}
		if !email {
			continue
		}
		if err := s.mail.mailer.Send(ctx, s.badgeMessage(u, fresh)); err != nil {
			log.Printf("badges: email to user %d: %v", id, err)
			continue
		}
		sent++
	}
	return sent, nil
}

// badgeMessage is the email about new badges: what was earned, its picture, and links to share it
func (s *Service) badgeMessage(u *models.User, badges []Badge) *mailer.Message {
	lang := i18n.Normalize(u.Lang)
	base := strings.TrimRight(s.mail.site.BaseURL, "/")
	shares := make([]BadgeShare, len(badges))
	for i, b := range badges {
		shares[i] = BadgeShareFor(base, lang, u, b, b.Tier)
	}
	subject := i18n.T(lang, "mail.badge_subject_many", len(shares))
	if len(shares) == 1 {
		subject = i18n.T(lang, "mail.badge_subject", shares[0].Name, i18n.T(lang, fmt.Sprintf("badges.tier_%s", shares[0].TierName)))
	}
	hello := i18n.T(lang, "mail.badge_hello", u.DisplayName())
	intro := i18n.T(lang, "mail.badge_intro")
	if len(shares) > 1 {
		intro = i18n.T(lang, "mail.badge_intro_many", len(shares))
	}
	encourage := i18n.T(lang, "mail.badge_share")
	profile := base + "/users/" + u.Username
	settings := base + "/profile#badge-emails"
	why := i18n.T(lang, "mail.badge_footer")
	off := i18n.T(lang, "mail.badge_off")

	var text, body strings.Builder
	fmt.Fprintf(&text, "%s\n\n%s\n\n", hello, intro)
	fmt.Fprintf(&body, `<meta charset="utf-8"><div style="font-family:sans-serif;max-width:560px;color:#1d1f24"><p>%s</p><p>%s</p>`,
		html.EscapeString(hello), html.EscapeString(intro))
	for _, sh := range shares {
		done := CapFirst(sh.Done)
		fmt.Fprintf(&text, "• %s · %s\n  %s\n  %s\n  LinkedIn: %s\n  X: %s\n  Facebook: %s\n\n",
			sh.Name, sh.TierLabel, done, sh.PageURL, sh.LinkedInURL(), sh.XURL(), sh.FacebookURL())
		fmt.Fprintf(&body, `<div style="border:1px solid #e7e8ec;border-radius:10px;padding:14px;margin:14px 0">
<a href="%s"><img src="%s" alt="%s" width="530" style="width:100%%;max-width:530px;height:auto;border-radius:8px;display:block"></a>
<p style="margin:12px 0 2px;font-size:17px"><strong>%s</strong></p>
<p style="margin:0 0 4px;color:#4a4e56">%s</p>
<p style="margin:0 0 12px">%s</p>
<p style="margin:0">%s &nbsp; <span style="font-size:13px;color:#4a4e56">%s <a href="%s">LinkedIn</a> · <a href="%s">X</a> · <a href="%s">Facebook</a></span></p></div>`,
			html.EscapeString(sh.PageURL), html.EscapeString(sh.ImageURL), html.EscapeString(sh.Name+" · "+sh.TierLabel),
			html.EscapeString(sh.Name), html.EscapeString(sh.TierLabel), html.EscapeString(done),
			mailer.Button(sh.PageURL, i18n.T(lang, "mail.badge_view")), html.EscapeString(i18n.T(lang, "mail.badge_share_on")),
			html.EscapeString(sh.LinkedInURL()), html.EscapeString(sh.XURL()), html.EscapeString(sh.FacebookURL()))
	}
	fmt.Fprintf(&text, "%s\n\n%s\n\n— %s\n%s\n%s: %s\n", encourage, profile, s.mail.site.Name, why, off, settings)
	fmt.Fprintf(&body, `<p>%s</p><p>%s</p>
<p style="color:#888;font-size:12px">%s · <a href="%s" style="color:#888">%s</a></p></div>`,
		html.EscapeString(encourage), mailer.Button(profile, i18n.T(lang, "mail.badge_profile")),
		html.EscapeString(why), html.EscapeString(settings), html.EscapeString(off))
	return &mailer.Message{To: u.Email, Subject: subject, Text: text.String(), HTML: body.String(), Category: "badge"}
}
