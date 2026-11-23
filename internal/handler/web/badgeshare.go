package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/badgecard"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Sharing badges: every earned tier has its own page, /users/{username}/badges/{key}/{tier}, with Open Graph
// and Twitter card tags, and a 1200×630 PNG preview (…/{tier}.png, drawn by badgecard). The address names
// the tier, so a link shared at Silver keeps showing Silver after the person reaches Gold (social sites
// cache previews by address). Preview texts are in the badge owner's language: they are the one sharing.

// pageMeta are the link preview tags of a page (base.html)
type pageMeta struct {
	Title, Desc, URL, Image, ImageAlt string
}

// badgeShare is one earned badge tier with its share texts (service.BadgeShareFor)
type badgeShare = service.BadgeShare

func (h *Handler) siteURL() string { return strings.TrimRight(h.siteConfig.BaseURL, "/") }

// shareFor builds the share texts of badge b at tier (1-based) for user u in lang
func (h *Handler) shareFor(lang string, u *models.User, b service.Badge, tier int) badgeShare {
	return service.BadgeShareFor(h.siteURL(), lang, u, b, tier)
}

func (h *Handler) ownerLang(u *models.User) string {
	if u.Lang != "" {
		return u.Lang
	}
	return h.siteConfig.DefaultLang
}

// badgeImages caches rendered share images (drawing one takes a few hundred milliseconds)
var badgeImages = struct {
	sync.Mutex
	m map[string][]byte
}{m: map[string][]byte{}}

func (h *Handler) badgeImage(u *models.User, s badgeShare) ([]byte, error) {
	key := strings.Join([]string{strconv.FormatInt(u.ID, 10), u.Username, u.DisplayName(), s.Key, strconv.Itoa(s.Tier), s.Name, s.Done}, "\x00")
	badgeImages.Lock()
	data, ok := badgeImages.m[key]
	badgeImages.Unlock()
	if ok {
		return data, nil
	}
	host := h.siteURL()
	if pu, err := url.Parse(host); err == nil && pu.Host != "" {
		host = pu.Host
	}
	data, err := badgecard.Render(badgecard.Card{Icon: s.Icon, Tier: s.TierName, TierIndex: s.Tier, TierCount: s.Tiers,
		Brand: h.siteConfig.Name, Title: s.Name, TierLabel: s.TierLabel, Desc: service.CapFirst(s.Done),
		Person: u.DisplayName() + " (@" + u.Username + ")", Footer: host})
	if err != nil {
		return nil, err
	}
	badgeImages.Lock()
	if len(badgeImages.m) >= 300 {
		badgeImages.m = map[string][]byte{}
	}
	badgeImages.m[key] = data
	badgeImages.Unlock()
	return data, nil
}

// BadgeSharePage serves an earned badge tier's share page, or its preview image when the address ends
// in .png: GET /users/{username}/badges/{key}/{tier}
func (h *Handler) BadgeSharePage(w http.ResponseWriter, r *http.Request) {
	u, err := h.authService.UserByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		h.fail(w, r, service.ErrNotFound)
		return
	}
	tierParam := chi.URLParam(r, "tier")
	image := strings.HasSuffix(tierParam, ".png")
	tier, err := strconv.Atoi(strings.TrimSuffix(tierParam, ".png"))
	prof, perr := h.activity.Profile(r.Context(), u, siteLocation, time.Now())
	if perr != nil {
		h.fail(w, r, perr)
		return
	}
	b, ok := prof.FindBadge(chi.URLParam(r, "key"))
	if !ok || err != nil || tier < 1 || tier > b.Tier { // not (or no longer) earned
		if image {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/users/"+url.PathEscape(u.Username), http.StatusSeeOther)
		return
	}
	owner := h.shareFor(h.ownerLang(u), u, b, tier)
	if image {
		data, err := h.badgeImage(u, owner)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if r.URL.Query().Has("download") { // "Unduh gambar": to attach the picture to a post by hand
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pelokalan-%s-%s-%d.png"`, u.Username, owner.Key, owner.Tier))
		}
		_, _ = w.Write(data)
		return
	}
	data := h.page(r, "")
	data.Title = owner.OGTitle
	data.Data["Meta"] = pageMeta{Title: owner.OGTitle, Desc: owner.OGDesc, URL: owner.PageURL, Image: owner.ImageURL, ImageAlt: owner.Name + " · " + owner.TierLabel}
	data.Data["Owner"] = u
	data.Data["Share"] = h.shareFor(data.Lang, u, b, tier) // the page itself in the reader's language
	data.Data["Badge"] = b
	if det, err := h.activity.BadgeDetail(r.Context(), prof, b, tier, siteLocation); err == nil {
		data.Data["Detail"] = h.badgeDetailView(data.Lang, det)
	}
	h.render(w, "badge_share", data)
}

// badgeDetailRow is one project/module line of a badge's details
type badgeDetailRow struct {
	ProjectName, ProjectPath, Module, Filename string
	Count                                      int
	When                                       string // "3 Nov 2026" or "3 Nov – 9 Nov 2026"
}

// badgeDetailView is a badge's details as the share page shows them
type badgeDetailView struct {
	Earned, Period string
	CountKey       string // badges.count_<…>
	Modules        bool
	Rows           []badgeDetailRow
}

// dateRange formats a span of dates ("3 Nov 2026", "3–9 Nov 2026" style kept simple: "3 Nov 2026 – 9 Nov 2026")
func dateRange(lang string, a, b time.Time) string {
	if a.Year() == b.Year() && a.YearDay() == b.YearDay() {
		return shortDate(lang, a)
	}
	return shortDate(lang, a) + " – " + shortDate(lang, b)
}

func (h *Handler) badgeDetailView(lang string, d *service.BadgeDetail) *badgeDetailView {
	v := &badgeDetailView{CountKey: fmt.Sprintf("badges.count_%s", d.Count), Modules: d.Modules}
	if d.EarnedOn != nil {
		e := *d.EarnedOn
		if e.Hour() != 0 || e.Minute() != 0 || e.Location() != time.UTC { // a moment, not a date: its day here
			e = e.In(siteLocation)
		}
		v.Earned = shortDate(lang, e)
	}
	if d.From != nil && d.To != nil {
		v.Period = dateRange(lang, *d.From, *d.To)
	}
	for _, m := range d.Work {
		v.Rows = append(v.Rows, badgeDetailRow{ProjectName: m.ProjectName, ProjectPath: m.ProjectPath(),
			Module: m.Module, Filename: m.Filename, Count: m.Count,
			When: dateRange(lang, m.First.In(siteLocation), m.Last.In(siteLocation))})
	}
	return v
}
