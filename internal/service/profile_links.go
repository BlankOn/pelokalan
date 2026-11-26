package service

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
)

// Public profile extras: a short description and links (website, GitHub, X, LinkedIn, Mastodon). Links are
// stored as full https addresses; people may type a handle or paste an address.

// MaxBio is the longest public profile description
const MaxBio = 300

var (
	githubUserRe   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	xUserRe        = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
	linkedinSlugRe = regexp.MustCompile(`^[A-Za-z0-9\-_%.]{3,100}$`)
	mastodonUserRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)
	hostRe         = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// pathOn returns the path segments of an address on one of hosts ("https://github.com/name" → ["name"]),
// or nil when raw is not such an address
func pathOn(raw string, hosts ...string) []string {
	s := raw
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	for _, h := range hosts {
		if host == h {
			return strings.Split(strings.Trim(u.Path, "/"), "/")
		}
	}
	return nil
}

// NormalizeLink turns what someone typed for a link kind into its address ("" stays ""); ok is false when it
// does not look like one
func NormalizeLink(kind, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	isURL := strings.Contains(raw, "/")
	switch kind {
	case "website":
		s := raw
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !hostRe.MatchString(strings.ToLower(u.Hostname())) || len(s) > 300 {
			return "", false
		}
		return u.String(), true
	case "github":
		name := strings.TrimPrefix(raw, "@")
		if isURL {
			p := pathOn(raw, "github.com")
			if len(p) == 0 {
				return "", false
			}
			name = p[0]
		}
		if !githubUserRe.MatchString(name) {
			return "", false
		}
		return "https://github.com/" + name, true
	case "x":
		name := strings.TrimPrefix(raw, "@")
		if isURL {
			p := pathOn(raw, "x.com", "twitter.com")
			if len(p) == 0 {
				return "", false
			}
			name = p[0]
		}
		if !xUserRe.MatchString(name) {
			return "", false
		}
		return "https://x.com/" + name, true
	case "linkedin":
		slug := strings.TrimPrefix(raw, "in/")
		if isURL && strings.Contains(raw, "linkedin.com") {
			p := pathOn(raw, "linkedin.com")
			if len(p) < 2 || p[0] != "in" {
				return "", false
			}
			slug = p[1]
		} else if isURL {
			return "", false
		}
		if !linkedinSlugRe.MatchString(slug) {
			return "", false
		}
		return "https://www.linkedin.com/in/" + slug, true
	case "mastodon":
		var user, host string
		if isURL { // https://instance/@user
			s := raw
			if !strings.Contains(s, "://") {
				s = "https://" + s
			}
			u, err := url.Parse(s)
			if err != nil {
				return "", false
			}
			p := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(p) == 0 || !strings.HasPrefix(p[0], "@") {
				return "", false
			}
			user, host = strings.TrimPrefix(p[0], "@"), strings.ToLower(u.Hostname())
		} else { // @user@instance
			parts := strings.Split(strings.TrimPrefix(raw, "@"), "@")
			if len(parts) != 2 {
				return "", false
			}
			user, host = parts[0], strings.ToLower(parts[1])
		}
		if !mastodonUserRe.MatchString(user) || !hostRe.MatchString(host) {
			return "", false
		}
		return "https://" + host + "/@" + user, true
	}
	return "", false
}

// AccountInput is everything on the profile page, saved at once
type AccountInput struct {
	Username, Name, Lang                          string
	TranslatorName, TranslatorEmail, LanguageTeam string
	Bio                                           string
	Links                                         map[string]string // kind → what was typed
	BadgeEmails                                   bool
}

// UpdateAccount checks every field of the profile page first and saves only when all are valid
func (s *AuthService) UpdateAccount(ctx context.Context, user *models.User, in AccountInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 100 {
		return ErrNameRequired
	}
	username := NormalizeUsername(in.Username)
	if err := s.usernameAvailable(ctx, username, user.ID); err != nil {
		return err
	}
	tname, temail, team := strings.TrimSpace(in.TranslatorName), strings.TrimSpace(in.TranslatorEmail), strings.TrimSpace(in.LanguageTeam)
	if temail != "" {
		addr, err := mail.ParseAddress(temail)
		if err != nil || addr.Address != temail {
			return ErrInvalidEmail
		}
	}
	if len([]rune(tname)) > 100 || len([]rune(team)) > 200 || strings.ContainsAny(tname+team, "\n\r") {
		return userErr("err.identity_invalid")
	}
	bio := strings.TrimSpace(strings.ReplaceAll(in.Bio, "\r\n", "\n"))
	if len([]rune(bio)) > MaxBio {
		return userErr("err.bio_length", MaxBio)
	}
	links := map[string]string{}
	for _, k := range models.LinkKinds {
		v, ok := NormalizeLink(k, in.Links[k])
		if !ok {
			return userErr("err.link_invalid", i18n.T(i18n.Normalize(user.Lang), fmt.Sprintf("profile.link_%s", k)))
		}
		if v != "" {
			links[k] = v
		}
	}
	user.Username, user.Name, user.Lang = username, name, i18n.Normalize(in.Lang)
	user.TranslatorName, user.TranslatorEmail, user.LanguageTeam = tname, temail, team
	if err := s.users.Update(ctx, user); err != nil {
		return err
	}
	if err := s.users.SetPublicProfile(ctx, user.ID, bio, links); err != nil {
		return err
	}
	user.Bio, user.Links = bio, links
	return s.SetBadgeEmails(ctx, user, in.BadgeEmails)
}
