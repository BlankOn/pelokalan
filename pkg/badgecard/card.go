// Package badgecard draws a badge as a 1200×630 PNG for link previews (Open Graph / Twitter cards):
// social sites do not show SVG previews, so the share image is drawn here with the Go fonts.
package badgecard

import (
	"bytes"
	"image/color"
	"math"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// W and H are the share image size (the size Open Graph and Twitter large cards expect)
const (
	W = 1200
	H = 630
)

// Tier is a tier's medal colours: face gradient (light to dark), rim, and the text colour
type Tier struct {
	Light, Mid, Dark, Rim, Text color.RGBA
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{uint8(hex >> 16), uint8(hex >> 8), uint8(hex), 255}
}

// Tiers by name (activity.BadgeTierNames); keep in step with the .tier-* colours in style.css
var Tiers = map[string]Tier{
	"bronze":   {rgb(0xf3c39a), rgb(0xd98c4e), rgb(0xa45a24), rgb(0x7a3f14), rgb(0x9c5a2a)},
	"silver":   {rgb(0xffffff), rgb(0xc4d3e6), rgb(0x7f93ad), rgb(0x4d6584), rgb(0x4d6584)},
	"gold":     {rgb(0xfff1b8), rgb(0xf2c443), rgb(0xc08a0a), rgb(0x8a6206), rgb(0x9a6e05)},
	"platinum": {rgb(0xe3fbfd), rgb(0x8fd6dc), rgb(0x2f8f9a), rgb(0x1d6670), rgb(0x23737d)},
	"diamond":  {rgb(0xeee7ff), rgb(0xb9a4f5), rgb(0x6440c9), rgb(0x45289a), rgb(0x5a37b8)},
}

// Card is what the share image says
type Card struct {
	Icon      string // Icons key
	Tier      string // Tiers key
	TierIndex int    // 1-based tier earned
	TierCount int
	Brand     string // site name
	Title     string // badge name
	TierLabel string // "Emas · tingkat 3 dari 5"
	Desc      string // what it took
	Person    string // "Ani (@ani)"
	Footer    string // site address
}

var regular, bold *truetype.Font

func init() {
	regular, _ = truetype.Parse(goregular.TTF)
	bold, _ = truetype.Parse(gobold.TTF)
}

func face(f *truetype.Font, size float64) font.Face {
	return truetype.NewFace(f, &truetype.Options{Size: size, Hinting: font.HintingFull})
}

func hexagon(dc *gg.Context, cx, cy, r float64) {
	dc.NewSubPath()
	for i := 0; i < 6; i++ {
		a := math.Pi/2 + float64(i)*math.Pi/3 // pointy top
		x, y := cx+r*math.Cos(a)*0.93, cy-r*math.Sin(a)
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	dc.ClosePath()
}

// Medal draws the earned medal: rim, shaded face, shine, white icon, centred on (cx, cy) with radius r
func Medal(dc *gg.Context, t Tier, icon string, cx, cy, r float64) {
	// soft shadow
	for i := 6; i >= 1; i-- {
		dc.SetRGBA(0, 0, 0, 0.025)
		hexagon(dc, cx, cy+10, r+float64(i)*3)
		dc.Fill()
	}
	hexagon(dc, cx, cy, r)
	dc.SetColor(t.Rim)
	dc.Fill()
	g := gg.NewLinearGradient(cx-r, cy-r, cx+r*0.6, cy+r)
	g.AddColorStop(0, t.Light)
	g.AddColorStop(0.35, t.Mid)
	g.AddColorStop(1, t.Dark)
	hexagon(dc, cx, cy, r*0.88)
	dc.SetFillStyle(g)
	dc.Fill()
	// bevel: a light line just inside the face's edge
	hexagon(dc, cx, cy, r*0.84)
	dc.SetColor(color.NRGBA{255, 255, 255, 120})
	dc.SetLineWidth(r * 0.025)
	dc.Stroke()
	if svg, ok := Icons[icon]; ok {
		s := r * 0.95 / 24
		dc.SetRGBA(0, 0, 0, 0.25) // icon shadow
		dc.SetLineWidth(1.9 * s)
		dc.SetLineCapRound()
		dc.SetLineJoinRound()
		drawIcon(dc, svg, cx-12*s, cy-12*s+3, s)
		dc.Stroke()
		dc.SetRGB(1, 1, 1)
		drawIcon(dc, svg, cx-12*s, cy-12*s, s)
		dc.Stroke()
	}
}

// Render draws the share image as PNG
func Render(c Card) ([]byte, error) {
	t, ok := Tiers[c.Tier]
	if !ok {
		t = Tiers["bronze"]
	}
	dc := gg.NewContext(W, H)
	bg := gg.NewLinearGradient(0, 0, W, H)
	bg.AddColorStop(0, color.RGBA{t.Mid.R/4 + 191, t.Mid.G/4 + 191, t.Mid.B/4 + 191, 255})
	bg.AddColorStop(0.55, color.RGBA{255, 255, 255, 255})
	bg.AddColorStop(1, color.RGBA{247, 248, 250, 255})
	dc.SetFillStyle(bg)
	dc.DrawRectangle(0, 0, W, H)
	dc.Fill()
	dc.SetColor(t.Dark) // band along the left edge
	dc.DrawRectangle(0, 0, 14, H)
	dc.Fill()

	Medal(dc, t, c.Icon, 290, 290, 190)
	// tier pips under the medal
	n := max(1, c.TierCount)
	for i := 0; i < n; i++ {
		x := 290 + (float64(i)-float64(n-1)/2)*34
		dc.DrawCircle(x, 548, 11)
		if i < c.TierIndex {
			dc.SetColor(t.Dark)
		} else {
			dc.SetRGB255(220, 223, 229)
		}
		dc.Fill()
	}

	x, maxW := 560.0, 590.0
	dc.SetFontFace(face(bold, 30))
	dc.SetColor(rgb(0x2f6fde))
	dc.DrawString(c.Brand, x, 120)
	y := 150.0
	dc.SetFontFace(face(bold, 66))
	dc.SetRGB255(29, 31, 36)
	for _, line := range dc.WordWrap(c.Title, maxW) {
		y += 74
		dc.DrawString(line, x, y)
	}
	y += 62
	dc.SetFontFace(face(bold, 38))
	dc.SetColor(t.Text)
	dc.DrawString(c.TierLabel, x, y)
	y += 22
	dc.SetFontFace(face(regular, 33))
	dc.SetRGB255(70, 74, 82)
	for _, line := range dc.WordWrap(c.Desc, maxW) {
		y += 44
		dc.DrawString(line, x, y)
	}
	dc.SetFontFace(face(bold, 30))
	dc.SetRGB255(29, 31, 36)
	dc.DrawString(c.Person, x, 540)
	dc.SetFontFace(face(regular, 26))
	dc.SetRGB255(107, 112, 121)
	dc.DrawString(c.Footer, x, 580)

	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
