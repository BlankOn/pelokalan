package web

import (
	"fmt"
	"strings"
	"time"

	"pelokalan/internal/models"
)

// Growth chart on the Wawasan tab: translated source words added up over time, one point per
// day from the first recorded translation to today (flat on days without translations).
type wcPoint struct {
	X, Y           float64
	Day            time.Time
	Words, Strings int // running totals
	DayWords       int // that day alone
}

type wordsChart struct {
	Lang             string
	W, H             float64
	PlotTop, PlotBot float64
	Line, Area       string
	Points           []wcPoint
	XLabels          []gcLabel
	YLabels          []gcYLabel
	Words, Strings   int
}

func buildWordsChart(lang string, days []models.ActivityDay, now time.Time) *wordsChart {
	if len(days) == 0 {
		return nil
	}
	c := &wordsChart{Lang: lang, W: gcW, H: gcH, PlotTop: gcTop, PlotBot: gcH - gcBottom}
	byDay := map[time.Time]models.ActivityDay{}
	for _, d := range days {
		byDay[d.Day.UTC().Truncate(24*time.Hour)] = d
	}
	first := days[0].Day.UTC().Truncate(24 * time.Hour)
	last := now.UTC().Truncate(24 * time.Hour)
	if l := days[len(days)-1].Day.UTC().Truncate(24 * time.Hour); l.After(last) {
		last = l
	}
	// the line starts at zero at the beginning of the first day; each later point is the total
	// at the end of a day
	type pt struct {
		t                    time.Time
		words, strs, dayWord int
	}
	pts := []pt{{t: first}}
	words, strs := 0, 0
	for d := first; !d.After(last); d = d.Add(24 * time.Hour) {
		a := byDay[d]
		words += a.Words
		strs += a.Strings
		pts = append(pts, pt{t: d.Add(24 * time.Hour), words: words, strs: strs, dayWord: a.Words})
	}
	c.Words, c.Strings = words, strs
	step := niceStep(max(1, words) / 4)
	hi := (words/step + 1) * step
	top, bot := c.PlotTop+(c.PlotBot-c.PlotTop)*0.04, c.PlotBot
	t0, t1 := pts[0].t, pts[len(pts)-1].t
	span := t1.Sub(t0).Seconds()
	xOf := func(t time.Time) float64 {
		return gcLeft + t.Sub(t0).Seconds()/span*(gcW-gcLeft-gcRight)
	}
	yOf := func(v int) float64 { return bot - float64(v)/float64(hi)*(bot-top) }
	var line []string
	for _, p := range pts {
		x, y := xOf(p.t), yOf(p.words)
		line = append(line, fmt.Sprintf("%.1f,%.1f", x, y))
		c.Points = append(c.Points, wcPoint{X: x, Y: y, Day: p.t.Add(-time.Second), Words: p.words, Strings: p.strs, DayWords: p.dayWord})
	}
	c.Points = c.Points[1:] // the zero start is not a day of its own
	c.Line = strings.Join(line, " ")
	c.Area = fmt.Sprintf("%.1f,%.1f %s %.1f,%.1f", xOf(t0), bot, c.Line, xOf(t1), bot)
	for v := 0; v <= hi; v += step {
		c.YLabels = append(c.YLabels, gcYLabel{Y: yOf(v), Value: v})
	}
	n := min(6, len(pts))
	for k := 0; k < n; k++ {
		t := t0.Add(time.Duration(float64(t1.Sub(t0)) * float64(k) / float64(n-1)))
		c.XLabels = append(c.XLabels, gcLabel{X: xOf(t), Label: t.Format("02-01-2006"), Time: t})
	}
	return c
}
