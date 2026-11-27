package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
)

// BadgeShare is one earned badge tier with its texts and addresses, for the share page, the share
// dialog and the email about a new badge
type BadgeShare struct {
	Key, Icon, TierName string
	Tier, Tiers         int
	PageURL, ImageURL   string
	Name, TierLabel     string // badge name, "Emas · tingkat 3 dari 5"
	Done                string // what it took, past tense
	Text                string // ready-made post text (first person)
	OGTitle, OGDesc     string
}

// LinkedInURL, XURL and FacebookURL share the badge page (the networks read its preview tags)
func (s BadgeShare) LinkedInURL() string {
	return "https://www.linkedin.com/sharing/share-offsite/?url=" + url.QueryEscape(s.PageURL)
}

func (s BadgeShare) XURL() string {
	return "https://twitter.com/intent/tweet?text=" + url.QueryEscape(s.Text) + "&url=" + url.QueryEscape(s.PageURL)
}

func (s BadgeShare) FacebookURL() string {
	return "https://www.facebook.com/sharer/sharer.php?u=" + url.QueryEscape(s.PageURL)
}

// BadgeShareFor builds the texts of badge b at tier (1-based) for user u in lang; base is the site address
func BadgeShareFor(base, lang string, u *models.User, b Badge, tier int) BadgeShare {
	tn := TierName(tier)
	s := BadgeShare{Key: b.Key, Icon: b.Icon, TierName: tn, Tier: tier, Tiers: b.Tiers}
	s.PageURL = fmt.Sprintf("%s/users/%s/badges/%s/%d", strings.TrimRight(base, "/"), url.PathEscape(u.Username), b.Key, tier)
	s.ImageURL = s.PageURL + ".png"
	s.Name = i18n.T(lang, "badge."+b.Key)
	tierName := i18n.T(lang, fmt.Sprintf("badges.tier_%s", tn))
	s.TierLabel = i18n.T(lang, "badges.tier_label", tierName, tier, b.Tiers)
	s.Done = i18n.T(lang, "badge."+b.Key+"_done", FormatNum(b.Shown(b.Steps[tier-1])))
	s.Text = i18n.T(lang, "share.text", tierName, s.Name, s.Done)
	s.OGTitle = i18n.T(lang, "share.og_title", u.DisplayName(), tierName, s.Name)
	s.OGDesc = i18n.T(lang, "share.og_desc", CapFirst(s.Done))
	// the image address changes with what the card says (renamed badge or person, new wording), so
	// browsers and link previews never keep an outdated picture for the day it may be cached
	sum := sha256.Sum256([]byte(strings.Join([]string{u.DisplayName(), u.Username, s.Name, s.TierLabel, s.Done}, "\x00")))
	s.ImageURL += "?v=" + hex.EncodeToString(sum[:4])
	return s
}

// CapFirst upper-cases the first letter ("menerjemahkan …" → "Menerjemahkan …")
func CapFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// FormatNum writes a whole number with dots between thousands ("12.345"), as the pages do
func FormatNum(v int) string {
	s := strconv.Itoa(v)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
