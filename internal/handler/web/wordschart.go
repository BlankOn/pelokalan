package web

import (
	"fmt"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
)

// Growth chart on the Statistik tab ("Pertumbuhan terjemahan"): strings people saved with a
// translation, added up hour by hour from the first recorded one to now; stretches of more than a day
// without any are compressed to a marked break (wcQuiet) so the active periods fill the width. The green total is the sum of four lines by where the text came from: typed by hand, AI
// drafts saved unchanged, AI drafts edited before saving, and existing translations edited. All
// lines share one scale. Hovering shows the nearest hour in the same floating card as the module
// group chart (app.js).
type wcPoint struct {
	X, Y                               float64
	Hour                               time.Time // start of the hour; the point is the total at its end
	Strings                            int       // running total
	HourStrings                        int       // that hour alone
	Manual, AIKept, AIEdited, Edited   int       // running totals per origin
	YManual, YAIKept, YAIEdited, YEdit float64
}

// wcSeries is one origin line of the chart
type wcSeries struct {
	Kind  string // css class and i18n key suffix: manual, ai_kept, ai_edited, edited
	Line  string
	Total int
}

// wcQuiet is the longest stretch without saved strings drawn to scale; longer ones become a break of
// wcGapW units with a dashed marker (its tooltip says how long it was). wcLabelGap spaces the time labels.
const (
	wcQuiet    = 24 * time.Hour
	wcGapW     = 26.0
	wcLabelGap = 110.0
)

// wcGap is a compressed quiet stretch on the chart
type wcGap struct {
	X, X2, W, Mid float64
	Title         string
}

type wordsChart struct {
	Lang             string
	W, H             float64
	PlotTop, PlotBot float64
	Line, Area       string     // the total
	Series           []wcSeries // the origin lines
	Points           []wcPoint
	XLabels          []gcLabel
	Gaps             []wcGap
	PlotH, GapTextY  float64 // break marker: band height, "⋯" position
	YLabels          []gcYLabel
	Strings          int
}

func buildWordsChart(lang string, hours []models.ActivityDay, now time.Time) *wordsChart {
	if len(hours) == 0 {
		return nil
	}
	c := &wordsChart{Lang: lang, W: gcW, H: gcH, PlotTop: gcTop, PlotBot: gcH - gcBottom}
	c.PlotH, c.GapTextY = c.PlotBot-c.PlotTop, c.PlotBot-4
	byHour := map[time.Time]models.ActivityDay{}
	for _, h := range hours {
		byHour[h.Day.UTC().Truncate(time.Hour)] = h
	}
	first := hours[0].Day.UTC().Truncate(time.Hour)
	last := now.UTC().Truncate(time.Hour)
	if l := hours[len(hours)-1].Day.UTC().Truncate(time.Hour); l.After(last) {
		last = l
	}
	// the lines start at zero at the beginning of the first hour; each later point is the total
	// at the end of an hour. Quiet stretches of more than wcQuiet (no strings saved) are compressed to a
	// short break (wcGap) so active periods get the width; the line stays continuous (flat) across them.
	type pt struct {
		t                                time.Time
		total, hour                      int
		manual, aiKept, aiEdited, edited int
		gapBefore                        bool // a compressed quiet stretch ends at this point
	}
	pts := []pt{{t: first}}
	var cur pt
	for h := first; !h.After(last); {
		a, ok := byHour[h]
		if !ok || a.Strings == 0 { // how long is this quiet stretch?
			end := h
			for !end.After(last) {
				if b, ok := byHour[end]; ok && b.Strings > 0 {
					break
				}
				end = end.Add(time.Hour)
			}
			if end.Sub(h) > wcQuiet {
				cur.t, cur.hour, cur.gapBefore = end, 0, true
				pts = append(pts, cur)
				cur.gapBefore = false
				h = end
				continue
			}
		}
		cur.total += a.Strings
		cur.manual += a.Manual
		cur.aiKept += a.AIKept
		cur.aiEdited += a.AIEdited
		cur.edited += a.Edited
		cur.t, cur.hour = h.Add(time.Hour), a.Strings
		pts = append(pts, cur)
		h = h.Add(time.Hour)
	}
	c.Strings = cur.total
	step := niceStep(max(1, cur.total) / 4)
	hi := (cur.total/step + 1) * step
	top, bot := c.PlotTop+(c.PlotBot-c.PlotTop)*0.04, c.PlotBot
	t0, t1 := pts[0].t, pts[len(pts)-1].t
	// x positions: active time in proportion, each compressed stretch a fixed break
	gaps, active := 0, 0.0
	for i := 1; i < len(pts); i++ {
		if pts[i].gapBefore {
			gaps++
		} else {
			active += pts[i].t.Sub(pts[i-1].t).Hours()
		}
	}
	plotW := gcW - gcLeft - gcRight
	gapW := min(wcGapW, plotW*0.5/float64(max(1, gaps)))
	perHour := 0.0
	if active > 0 {
		perHour = (plotW - gapW*float64(gaps)) / active
	} else if gaps > 0 {
		gapW = plotW / float64(gaps)
	}
	xs := make([]float64, len(pts))
	xs[0] = gcLeft
	for i := 1; i < len(pts); i++ {
		if pts[i].gapBefore {
			xs[i] = xs[i-1] + gapW
			days := int(pts[i].t.Sub(pts[i-1].t).Hours()/24 + 0.5)
			c.Gaps = append(c.Gaps, wcGap{X: xs[i-1], X2: xs[i-1] + gapW, W: gapW, Mid: xs[i-1] + gapW/2,
				Title: i18n.T(lang, "insight.growth_gap", max(1, days), pts[i-1].t.Format("02-01-2006"), pts[i].t.Format("02-01-2006"))})
		} else {
			xs[i] = xs[i-1] + pts[i].t.Sub(pts[i-1].t).Hours()*perHour
		}
	}
	yOf := func(v int) float64 { return bot - float64(v)/float64(hi)*(bot-top) }
	var line, lManual, lKept, lAIEdited, lEdited []string
	for i, p := range pts {
		x := xs[i]
		y, ym, yk, ya, ye := yOf(p.total), yOf(p.manual), yOf(p.aiKept), yOf(p.aiEdited), yOf(p.edited)
		line = append(line, fmt.Sprintf("%.1f,%.1f", x, y))
		lManual = append(lManual, fmt.Sprintf("%.1f,%.1f", x, ym))
		lKept = append(lKept, fmt.Sprintf("%.1f,%.1f", x, yk))
		lAIEdited = append(lAIEdited, fmt.Sprintf("%.1f,%.1f", x, ya))
		lEdited = append(lEdited, fmt.Sprintf("%.1f,%.1f", x, ye))
		if i > 0 && !p.gapBefore { // the zero start and the ends of quiet stretches are no hours of their own
			c.Points = append(c.Points, wcPoint{X: x, Y: y, Hour: p.t.Add(-time.Hour), Strings: p.total,
				HourStrings: p.hour, Manual: p.manual, AIKept: p.aiKept, AIEdited: p.aiEdited, Edited: p.edited,
				YManual: ym, YAIKept: yk, YAIEdited: ya, YEdit: ye})
		}
	}
	c.Line = strings.Join(line, " ")
	c.Area = fmt.Sprintf("%.1f,%.1f %s %.1f,%.1f", xs[0], bot, c.Line, xs[len(xs)-1], bot)
	c.Series = []wcSeries{
		{Kind: "manual", Line: strings.Join(lManual, " "), Total: cur.manual},
		{Kind: "ai_kept", Line: strings.Join(lKept, " "), Total: cur.aiKept},
		{Kind: "ai_edited", Line: strings.Join(lAIEdited, " "), Total: cur.aiEdited},
		{Kind: "edited", Line: strings.Join(lEdited, " "), Total: cur.edited},
	}
	for v := 0; v <= hi; v += step {
		c.YLabels = append(c.YLabels, gcYLabel{Y: yOf(v), Value: v})
	}
	// time labels where active stretches start (after a break) and at the end, at least wcLabelGap apart
	withTime := t1.Sub(t0) <= 3*24*time.Hour // short spans: the hour matters
	addLabel := func(x float64, t time.Time) {
		if n := len(c.XLabels); n > 0 && x-c.XLabels[n-1].X < wcLabelGap {
			return
		}
		label := t.Format("02-01-2006")
		if withTime {
			label = t.Format("02-01 15:04")
		}
		c.XLabels = append(c.XLabels, gcLabel{X: x, Label: label, Time: t, WithTime: withTime})
	}
	for i, p := range pts {
		if i == 0 || p.gapBefore {
			addLabel(xs[i], p.t)
			continue
		}
		// long active stretches get labels in between too
		if n := len(c.XLabels); n > 0 && xs[i]-c.XLabels[n-1].X >= 2*wcLabelGap {
			addLabel(xs[i], p.t)
		}
	}
	if n := len(c.XLabels); n > 0 && xs[len(xs)-1]-c.XLabels[n-1].X >= wcLabelGap {
		addLabel(xs[len(xs)-1], t1)
	}
	return c
}
