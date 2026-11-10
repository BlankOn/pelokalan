package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Progress chart of a module group (above Berkas proyek): translated, fuzzy and untranslated
// totals over time, each line scaled to its own range like the l10n.gnome.org team charts.
const (
	gcW      = 760.0
	gcH      = 230.0
	gcLeft   = 8.0
	gcRight  = 8.0
	gcTop    = 14.0
	gcBottom = 10.0
)

type gcSeries struct {
	Key      string // translated, fuzzy, untranslated
	Line     string // polyline points
	Latest   int
	Change   int // since the first point of the range
	Min, Max int
}

type gcPoint struct {
	X, Y1, Y2, Y3 float64
	Stat          models.GroupStat
}

type gcYLabel struct {
	Y     float64
	Value int
}

type gcLabel struct {
	X        float64
	Label    string
	Time     time.Time
	WithTime bool
}

type groupChart struct {
	Lang     string
	Project  *models.Project
	Groups   []*models.ProjectFileVersion
	Selected map[int64]bool // ticked groups (none = all)
	Range    string
	Ranges   []service.StatsRange
	Series   []gcSeries
	Points   []gcPoint
	XLabels  []gcLabel
	YLabels  []gcYLabel
	Scale    string // "" = shared, "each" = per line
	W, H     float64
	PlotTop  float64
	PlotBot  float64
	First    time.Time
	HasPoint bool
}

func (h *Handler) groupChart(r *http.Request, p *models.Project, groups []*models.ProjectFileVersion) *groupChart {
	if len(groups) == 0 {
		return nil
	}
	c := &groupChart{Lang: middleware.GetLang(r.Context()), Project: p, Groups: groups, Ranges: service.StatsRanges,
		W: gcW, H: gcH, PlotTop: gcTop, PlotBot: gcH - gcBottom, Range: "30", Selected: map[int64]bool{}}
	known := map[int64]bool{}
	for _, g := range groups {
		known[g.ID] = true
	}
	for _, id := range groupParams(r) {
		if known[id] {
			c.Selected[id] = true
		}
	}
	if r.URL.Query().Get("cs") == "each" {
		c.Scale = "each"
	}
	days := 30
	for _, rg := range service.StatsRanges {
		if rg.Key == r.URL.Query().Get("cr") {
			c.Range, days = rg.Key, rg.Days
		}
	}
	// the ticked groups (none = all), added up per hour
	var series [][]models.GroupStat
	for _, g := range groups {
		if len(c.Selected) > 0 && !c.Selected[g.ID] {
			continue
		}
		if st, err := h.glossary.GroupStats(r.Context(), p, g.ID, "", days); err == nil && len(st) > 0 {
			series = append(series, st)
		}
	}
	// "Hari ini": one point per hour; longer ranges: one point per day (the last of each)
	bucket := 24 * time.Hour
	if days == 1 {
		bucket = time.Hour
	}
	stats := lastPerBucket(sumByHour(series), bucket)
	if len(stats) == 0 {
		return c
	}
	c.HasPoint = true
	c.First = stats[0].RecordedAt
	get := []func(models.GroupStat) int{
		func(g models.GroupStat) int { return g.Translated },
		func(g models.GroupStat) int { return g.Fuzzy },
		func(g models.GroupStat) int { return g.Untranslated() },
	}
	keys := []string{"translated", "fuzzy", "untranslated"}
	t0, t1 := stats[0].RecordedAt, stats[len(stats)-1].RecordedAt
	span := t1.Sub(t0).Seconds()
	xOf := func(t time.Time) float64 {
		if span <= 0 {
			return gcW / 2
		}
		return gcLeft + t.Sub(t0).Seconds()/span*(gcW-gcLeft-gcRight)
	}
	ys := make([][]float64, 3)
	top, bot := gcTop+(gcH-gcBottom-gcTop)*0.06, gcH-gcBottom-(gcH-gcBottom-gcTop)*0.06
	// Shared scale (default): every line on one axis, so the distance between lines is real.
	// Per line ("each"): every line zoomed to its own range to see small changes; the legend
	// then shows each line's own range, as positions cannot be compared between lines.
	glo, ghi := get[0](stats[0]), get[0](stats[0])
	for _, f := range get {
		for _, s := range stats {
			glo, ghi = min(glo, f(s)), max(ghi, f(s))
		}
	}
	// round the axis to tidy steps (1, 2 or 5 × a power of ten), four of them
	step := niceStep(max(1, ghi-glo) / 4)
	glo, ghi = (glo/step)*step, (ghi/step+1)*step
	for i, f := range get {
		lo, hi := f(stats[0]), f(stats[0])
		for _, s := range stats {
			lo, hi = min(lo, f(s)), max(hi, f(s))
		}
		slo, shi := glo, ghi
		if c.Scale == "each" {
			slo, shi = lo, hi
			if shi == slo { // no change: a small band around the value, so the line sits mid-height
				slo, shi = slo-1, shi+1
			}
		}
		var pts []string
		for _, s := range stats {
			y := bot - float64(f(s)-slo)/float64(shi-slo)*(bot-top)
			ys[i] = append(ys[i], y)
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", xOf(s.RecordedAt), y))
		}
		c.Series = append(c.Series, gcSeries{Key: keys[i], Line: strings.Join(pts, " "),
			Latest: f(stats[len(stats)-1]), Change: f(stats[len(stats)-1]) - f(stats[0]), Min: lo, Max: hi})
	}
	if c.Scale != "each" { // value labels along the left side (shared scale only)
		for v := glo; v <= ghi; v += step {
			y := bot - float64(v-glo)/float64(ghi-glo)*(bot-top)
			c.YLabels = append(c.YLabels, gcYLabel{Y: y, Value: v})
		}
	}
	for j, s := range stats {
		c.Points = append(c.Points, gcPoint{X: xOf(s.RecordedAt), Y1: ys[0][j], Y2: ys[1][j], Y3: ys[2][j], Stat: s})
	}
	// date labels: up to 6 along the bottom
	n := min(6, len(stats))
	for k := 0; k < n; k++ {
		var t time.Time
		if n == 1 {
			t = t0
		} else {
			t = t0.Add(time.Duration(float64(t1.Sub(t0)) * float64(k) / float64(n-1)))
		}
		// shown in the reader's time zone by app.js (data-t); the text is the UTC fallback
		label := t.Format("02-01")
		withTime := span < 2*24*3600
		if withTime {
			label = t.Format("02-01 15:04")
		}
		c.XLabels = append(c.XLabels, gcLabel{X: xOf(t), Label: label, Time: t, WithTime: withTime})
	}
	return c
}

// GroupChart returns the progress chart for another group or range (htmx)
func (h *Handler) GroupChart(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	groups, err := h.glossary.ProjectFileVersions(r.Context(), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	c := h.groupChart(r, p, groups)
	if c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.renderPartials(w, partial{"group_chart", c})
}

// sumByHour adds up several groups' recorded totals: one point per hour in which any group
// was recorded, each group counting with its latest value at that hour (groups not yet
// recorded count as nothing)
func sumByHour(series [][]models.GroupStat) []models.GroupStat {
	if len(series) == 1 {
		return series[0]
	}
	hours := map[time.Time]bool{}
	for _, st := range series {
		for _, s := range st {
			hours[s.RecordedAt.Truncate(time.Hour)] = true
		}
	}
	var keys []time.Time
	for t := range hours {
		keys = append(keys, t)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	idx := make([]int, len(series)) // next unread point of each group
	last := make([]*models.GroupStat, len(series))
	var out []models.GroupStat
	for _, hour := range keys {
		end := hour.Add(time.Hour)
		sum := models.GroupStat{RecordedAt: hour}
		for i, st := range series {
			for idx[i] < len(st) && st[idx[i]].RecordedAt.Before(end) {
				last[i] = &st[idx[i]]
				idx[i]++
			}
			if l := last[i]; l != nil {
				sum.Total += l.Total
				sum.Translated += l.Translated
				sum.Fuzzy += l.Fuzzy
				if l.RecordedAt.After(sum.RecordedAt) {
					sum.RecordedAt = l.RecordedAt
				}
			}
		}
		out = append(out, sum)
	}
	return out
}

// lastPerBucket keeps the last point of every period (an hour, a day), oldest first
func lastPerBucket(stats []models.GroupStat, d time.Duration) []models.GroupStat {
	var out []models.GroupStat
	for _, s := range stats {
		if n := len(out); n > 0 && out[n-1].RecordedAt.Truncate(d).Equal(s.RecordedAt.Truncate(d)) {
			out[n-1] = s
			continue
		}
		out = append(out, s)
	}
	return out
}

// niceStep rounds a step up to 1, 2 or 5 times a power of ten
func niceStep(raw int) int {
	p := 1
	for p*10 <= raw {
		p *= 10
	}
	for _, m := range []int{1, 2, 5, 10} {
		if m*p >= raw {
			return m * p
		}
	}
	return 10 * p
}
