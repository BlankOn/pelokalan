package web

import (
	"fmt"
	"strings"
	"time"

	"pelokalan/internal/models"
)

// Growth chart on the Statistik tab ("Pertumbuhan terjemahan"): strings people saved with a
// translation, added up hour by hour from the first recorded one to now (flat in hours without
// any). The green total is the sum of four lines by where the text came from: typed by hand, AI
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

type wordsChart struct {
	Lang             string
	W, H             float64
	PlotTop, PlotBot float64
	Line, Area       string     // the total
	Series           []wcSeries // the origin lines
	Points           []wcPoint
	XLabels          []gcLabel
	YLabels          []gcYLabel
	Strings          int
}

func buildWordsChart(lang string, hours []models.ActivityDay, now time.Time) *wordsChart {
	if len(hours) == 0 {
		return nil
	}
	c := &wordsChart{Lang: lang, W: gcW, H: gcH, PlotTop: gcTop, PlotBot: gcH - gcBottom}
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
	// at the end of an hour
	type pt struct {
		t                                time.Time
		total, hour                      int
		manual, aiKept, aiEdited, edited int
	}
	pts := []pt{{t: first}}
	var cur pt
	for h := first; !h.After(last); h = h.Add(time.Hour) {
		a := byHour[h]
		cur.total += a.Strings
		cur.manual += a.Manual
		cur.aiKept += a.AIKept
		cur.aiEdited += a.AIEdited
		cur.edited += a.Edited
		cur.t, cur.hour = h.Add(time.Hour), a.Strings
		pts = append(pts, cur)
	}
	c.Strings = cur.total
	step := niceStep(max(1, cur.total) / 4)
	hi := (cur.total/step + 1) * step
	top, bot := c.PlotTop+(c.PlotBot-c.PlotTop)*0.04, c.PlotBot
	t0, t1 := pts[0].t, pts[len(pts)-1].t
	span := t1.Sub(t0).Seconds()
	xOf := func(t time.Time) float64 {
		return gcLeft + t.Sub(t0).Seconds()/span*(gcW-gcLeft-gcRight)
	}
	yOf := func(v int) float64 { return bot - float64(v)/float64(hi)*(bot-top) }
	var line, lManual, lKept, lAIEdited, lEdited []string
	for i, p := range pts {
		x := xOf(p.t)
		y, ym, yk, ya, ye := yOf(p.total), yOf(p.manual), yOf(p.aiKept), yOf(p.aiEdited), yOf(p.edited)
		line = append(line, fmt.Sprintf("%.1f,%.1f", x, y))
		lManual = append(lManual, fmt.Sprintf("%.1f,%.1f", x, ym))
		lKept = append(lKept, fmt.Sprintf("%.1f,%.1f", x, yk))
		lAIEdited = append(lAIEdited, fmt.Sprintf("%.1f,%.1f", x, ya))
		lEdited = append(lEdited, fmt.Sprintf("%.1f,%.1f", x, ye))
		if i > 0 { // the zero start is not an hour of its own
			c.Points = append(c.Points, wcPoint{X: x, Y: y, Hour: p.t.Add(-time.Hour), Strings: p.total,
				HourStrings: p.hour, Manual: p.manual, AIKept: p.aiKept, AIEdited: p.aiEdited, Edited: p.edited,
				YManual: ym, YAIKept: yk, YAIEdited: ya, YEdit: ye})
		}
	}
	c.Line = strings.Join(line, " ")
	c.Area = fmt.Sprintf("%.1f,%.1f %s %.1f,%.1f", xOf(t0), bot, c.Line, xOf(t1), bot)
	c.Series = []wcSeries{
		{Kind: "manual", Line: strings.Join(lManual, " "), Total: cur.manual},
		{Kind: "ai_kept", Line: strings.Join(lKept, " "), Total: cur.aiKept},
		{Kind: "ai_edited", Line: strings.Join(lAIEdited, " "), Total: cur.aiEdited},
		{Kind: "edited", Line: strings.Join(lEdited, " "), Total: cur.edited},
	}
	for v := 0; v <= hi; v += step {
		c.YLabels = append(c.YLabels, gcYLabel{Y: yOf(v), Value: v})
	}
	n := min(6, len(pts))
	withTime := t1.Sub(t0) <= 3*24*time.Hour // short spans: the hour matters
	for k := 0; k < n; k++ {
		t := t0.Add(time.Duration(float64(t1.Sub(t0)) * float64(k) / float64(n-1))).Round(time.Hour) // whole hours
		label := t.Format("02-01-2006")
		if withTime {
			label = t.Format("02-01 15:04")
		}
		c.XLabels = append(c.XLabels, gcLabel{X: xOf(t), Label: label, Time: t, WithTime: withTime})
	}
	return c
}
