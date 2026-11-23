package badgecard

import (
	"math"
	"regexp"
	"strconv"

	"github.com/fogleman/gg"
)

var (
	elemRe  = regexp.MustCompile(`<(path|circle|rect)\b([^>]*)/?>`)
	attrRe  = regexp.MustCompile(`([a-z]+)="([^"]*)"`)
	tokenRe = regexp.MustCompile(`[MmLlHhVvCcAaZz]|-?(?:\d+\.?\d*|\.\d+)`)
)

// drawIcon adds the icon's shapes to dc's path at (x, y) scaled by s (24 units → 24*s pixels)
func drawIcon(dc *gg.Context, svg string, x, y, s float64) {
	pt := func(px, py float64) (float64, float64) { return x + px*s, y + py*s }
	for _, el := range elemRe.FindAllStringSubmatch(svg, -1) {
		a := map[string]float64{}
		d := ""
		for _, kv := range attrRe.FindAllStringSubmatch(el[2], -1) {
			if kv[1] == "d" {
				d = kv[2]
			} else if f, err := strconv.ParseFloat(kv[2], 64); err == nil {
				a[kv[1]] = f
			}
		}
		switch el[1] {
		case "circle":
			cx, cy := pt(a["cx"], a["cy"])
			dc.NewSubPath()
			dc.DrawCircle(cx, cy, a["r"]*s)
		case "rect":
			rx, ry := pt(a["x"], a["y"])
			dc.NewSubPath()
			dc.DrawRoundedRectangle(rx, ry, a["width"]*s, a["height"]*s, a["rx"]*s)
		case "path":
			drawPath(dc, d, pt, s)
		}
	}
}

// drawPath follows an SVG path's commands (absolute and relative M L H V C A Z, with repeats)
func drawPath(dc *gg.Context, d string, pt func(float64, float64) (float64, float64), s float64) {
	toks := tokenRe.FindAllString(d, -1)
	var cmd byte
	var cx, cy, sx, sy float64 // current and subpath start, in icon units
	i := 0
	num := func() float64 {
		if i >= len(toks) {
			return 0
		}
		f, _ := strconv.ParseFloat(toks[i], 64)
		i++
		return f
	}
	isNum := func() bool { return i < len(toks) && !regexp.MustCompile(`^[A-Za-z]$`).MatchString(toks[i]) }
	for i < len(toks) {
		if !isNum() {
			cmd = toks[i][0]
			i++
		}
		rel := cmd >= 'a'
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = cx, cy
		}
		switch cmd | 0x20 {
		case 'm':
			cx, cy = ox+num(), oy+num()
			sx, sy = cx, cy
			dc.NewSubPath()
			dc.MoveTo(pt(cx, cy))
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'l':
			cx, cy = ox+num(), oy+num()
			dc.LineTo(pt(cx, cy))
		case 'h':
			cx = ox + num()
			dc.LineTo(pt(cx, cy))
		case 'v':
			cy = oy + num()
			dc.LineTo(pt(cx, cy))
		case 'c':
			x1, y1 := ox+num(), oy+num()
			x2, y2 := ox+num(), oy+num()
			cx, cy = ox+num(), oy+num()
			ax, ay := pt(x1, y1)
			bx, by := pt(x2, y2)
			ex, ey := pt(cx, cy)
			dc.CubicTo(ax, ay, bx, by, ex, ey)
		case 'a':
			rx, ry := num(), num()
			num() // x-axis rotation: the icons use none
			large, sweep := num() != 0, num() != 0
			x2, y2 := ox+num(), oy+num()
			arcTo(dc, cx, cy, rx, ry, large, sweep, x2, y2, pt, s)
			cx, cy = x2, y2
		case 'z':
			dc.ClosePath()
			cx, cy = sx, sy
		default:
			i++ // unknown: skip a token so the loop ends
		}
	}
}

// arcTo draws an SVG elliptical arc without rotation (endpoint to centre parameterization)
func arcTo(dc *gg.Context, x1, y1, rx, ry float64, large, sweep bool, x2, y2 float64, pt func(float64, float64) (float64, float64), s float64) {
	if rx == 0 || ry == 0 || (x1 == x2 && y1 == y2) {
		dc.LineTo(pt(x2, y2))
		return
	}
	dx, dy := (x1-x2)/2, (y1-y2)/2
	if l := dx*dx/(rx*rx) + dy*dy/(ry*ry); l > 1 {
		rx, ry = rx*math.Sqrt(l), ry*math.Sqrt(l)
	}
	num := rx*rx*ry*ry - rx*rx*dy*dy - ry*ry*dx*dx
	den := rx*rx*dy*dy + ry*ry*dx*dx
	k := math.Sqrt(math.Max(0, num/den))
	if large == sweep {
		k = -k
	}
	cxp, cyp := k*rx*dy/ry, -k*ry*dx/rx
	ccx, ccy := cxp+(x1+x2)/2, cyp+(y1+y2)/2
	a1 := math.Atan2((y1-ccy)/ry, (x1-ccx)/rx)
	a2 := math.Atan2((y2-ccy)/ry, (x2-ccx)/rx)
	if sweep && a2 < a1 {
		a2 += 2 * math.Pi
	} else if !sweep && a2 > a1 {
		a2 -= 2 * math.Pi
	}
	px, py := pt(ccx, ccy)
	dc.DrawEllipticalArc(px, py, rx*s, ry*s, a1, a2)
}
