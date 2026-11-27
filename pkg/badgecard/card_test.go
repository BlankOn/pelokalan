package badgecard

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

func TestRender(t *testing.T) {
	for name := range Tiers {
		for icon := range Icons {
			if name != "silver" && icon != "flame" { // every icon once, every tier once
				continue
			}
			data, err := Render(Card{Icon: icon, Tier: name, TierIndex: 2, TierCount: 5, Brand: "Pelokalan",
				Title: "Penjelajah Modul", TierLabel: "Perak · tingkat 2 dari 5", Desc: "Mengerjakan 25 berkas",
				Person: "Ani Wijaya (@ani)", Footer: "pelokalan.blankon.id"})
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds().Dx() != W || img.Bounds().Dy() != H {
				t.Fatalf("%s/%s: %v %v", name, icon, err, img.Bounds())
			}
			if out := os.Getenv("BADGECARD_OUT"); out != "" && icon == "flame" {
				_ = os.WriteFile(out+"/card-"+name+".png", data, 0o644)
			}
		}
	}
}
