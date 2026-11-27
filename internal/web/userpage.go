package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/activity"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/i18n"
)

// Public profile pages: /users/{username} shows a person's translation work across all projects (totals,
// a contribution calendar, streaks, badges and projects). Public like the projects' Statistik tab; the
// sign-in email is never shown.

// heatCell is one day of the contribution calendar
type heatCell struct {
	X, Y  int
	Level int // 0 = nothing, 1–4 more
	Title string
}

type heatLabel struct {
	X, Y  int
	Label string
}

// heatmap is the contribution calendar: 53 weeks (Monday to Sunday columns) up to today
type heatmap struct {
	W, H     int
	Cells    []heatCell
	Months   []heatLabel
	Weekdays []heatLabel
	Levels   []int // the lower bound of levels 1–4, for the legend
}

const (
	heatCell0  = 11 // cell size
	heatGap    = 3
	heatLeft   = 30 // room for weekday labels
	heatTop    = 16 // room for month labels
	heatWeeks  = 53
	heatStride = heatCell0 + heatGap
)

// heatLevels are the lower bounds of the calendar's colour levels (strings in a day)
var heatLevels = []int{1, 10, 50, 150}

func heatLevel(n int) int {
	l := 0
	for i, b := range heatLevels {
		if n >= b {
			l = i + 1
		}
	}
	return l
}

// shortDate formats a date as "3 Nov 2026"
func shortDate(lang string, d time.Time) string {
	return fmt.Sprintf("%d %s %d", d.Day(), i18n.T(lang, fmt.Sprintf("month.short_%d", int(d.Month()))), d.Year())
}

func buildHeatmap(lang string, days []models.ProfileDay, today time.Time) heatmap {
	by := map[string]int{}
	for _, d := range days {
		by[d.Day.Format("2006-01-02")] = d.Contributions
	}
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7)) // Monday of this week
	start := monday.AddDate(0, 0, -7*(heatWeeks-1))
	h := heatmap{W: heatLeft + heatWeeks*heatStride, H: heatTop + 7*heatStride, Levels: heatLevels}
	lastMonth := time.Month(0)
	for w := 0; w < heatWeeks; w++ {
		x := heatLeft + w*heatStride
		if m := start.AddDate(0, 0, w*7).Month(); m != lastMonth { // a column whose Monday starts a new month
			h.Months = append(h.Months, heatLabel{X: x, Y: heatTop - 5, Label: i18n.T(lang, fmt.Sprintf("month.short_%d", int(m)))})
			lastMonth = m
		}
		for r := 0; r < 7; r++ {
			d := start.AddDate(0, 0, w*7+r)
			if d.After(today) {
				break
			}
			n := by[d.Format("2006-01-02")]
			title := i18n.T(lang, "user.heat_none", shortDate(lang, d))
			if n > 0 {
				title = i18n.T(lang, "user.heat_day", n, shortDate(lang, d))
			}
			h.Cells = append(h.Cells, heatCell{X: x, Y: heatTop + r*heatStride, Level: heatLevel(n), Title: title})
		}
	}
	if len(h.Months) > 1 && h.Months[1].X-h.Months[0].X < 3*heatStride { // a partial first month would overlap
		h.Months = h.Months[1:]
	}
	for _, r := range []int{0, 2, 4} {
		h.Weekdays = append(h.Weekdays, heatLabel{X: heatLeft - 6, Y: heatTop + r*heatStride + heatCell0 - 2, Label: i18n.T(lang, fmt.Sprintf("weekday.short_%d", r))})
	}
	return h
}

// UserPage is a person's public profile: GET /users/{username}
func (h *Handler) UserPage(w http.ResponseWriter, r *http.Request) {
	u, err := h.authService.UserByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	loc := siteLocation
	prof, err := h.activity.Profile(r.Context(), u, loc, time.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "")
	data.Title = u.DisplayName() + " (@" + u.Username + ")"
	data.Data["Profile"] = prof
	data.Data["Heatmap"] = buildHeatmap(data.Lang, prof.Days, prof.Today)
	data.Data["TimeZone"] = loc.String()
	// share links of the earned badges (the owner sees Share buttons) and the profile's own link preview
	shares := map[string]badgeShare{}
	var top *activity.Badge
	for _, g := range prof.Badges {
		for i, b := range g.Badges {
			if b.Tier == 0 {
				continue
			}
			shares[b.Key] = h.shareFor(data.Lang, u, b, b.Tier)
			if top == nil || b.Tier > top.Tier {
				top = &g.Badges[i]
			}
		}
	}
	data.Data["Shares"] = shares
	data.Data["Mine"] = data.User != nil && data.User.ID == u.ID
	ol := h.ownerLang(u)
	meta := pageMeta{Title: i18n.T(ol, "user.og_title", u.DisplayName(), u.Username),
		Desc: i18n.T(ol, "user.og_desc", groupDigits(int64(prof.Stats.Contributions)), prof.Earned),
		URL:  h.siteURL() + "/users/" + u.Username}
	if top != nil {
		s := h.shareFor(ol, u, *top, top.Tier)
		meta.Image, meta.ImageAlt = s.ImageURL, s.Name+" · "+s.TierLabel
	}
	data.Data["Meta"] = meta
	h.render(w, "user", data)
}
