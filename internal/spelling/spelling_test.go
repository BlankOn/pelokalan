package spelling

import (
	"fmt"
	"testing"
)

// a tiny CSV in the KBBI extract's format (never the real data in tests)
const sample = `kata,lema,pelafalan,etimologi,makna,tags_kelas,tags_bahasa,tags_bidang,tags_ragam,contoh,turunan,gabungan_kata,peribahasa,kiasan,varian,dasar
"tampil","tampil","","","","","","","","","berpenampilan | menampilkan | penampil | penampilan | tampilan","tampil beda","","","",""
"kontak","kontak","","","","","","","","","berkontak | mengontak","kontak mata | kontak-kontakan","","","",""
"kontrak","kontrak","","","","","","","","","","","","","",""
"unduh","unduh","","","","","","","","","mengunduh | unduhan","","","","",""
"berkas","berkas","","","","","","","","","pemberkasan","berkas data","","","",""
"kata","kata","","","","","","","","","berkata | mengatakan","","","","",""
"tinjau","tinjau","","","","","","","","","meninjau | tinjauan","","","","",""
"buka","buka","","","","","","","","","membuka | terbuka","","","","",""
"simpan","simpan","","","","","","","","","menyimpan | penyimpanan","","","","",""
"hari","hari","","","","","","","","","sehari | sehari-hari","","","","",""
"atur","atur","","","","","","","","","mengatur | pengaturan","","","","",""
"lalu","lalu","","","","","","","","","","","","","",""
"x-ray","X-ray","","","","","","","","","","","","","",""
"3m","3M","","","","","","","","","","","","","",""
`

func dict(t *testing.T) *Dict {
	t.Helper()
	words, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	d := &Dict{}
	d.Set(words)
	return d
}

func TestParse(t *testing.T) {
	d := dict(t)
	for _, w := range []string{"tampil", "menampilkan", "kontak-kontakan", "sehari-hari", "x-ray"} {
		if !d.has(w) {
			t.Errorf("%q missing", w)
		}
	}
	if d.has("3m") || d.has("tampil beda") || d.has("berkas data") {
		t.Error("digits or multi-word entries kept")
	}
	if _, err := Parse([]byte("a,b\n1,2\n")); err == nil {
		t.Error("a CSV without kata accepted")
	}
}

func TestKnownForms(t *testing.T) {
	d := dict(t)
	known := []string{
		"Kontak", "tampilan", "menampilkan", // listed
		"ditampilkan", "Tampilkan", "diunduh", "ditinjau", "disimpan", // passive / imperative from meN- forms
		"berkasnya", "bukalah", "kubuka", // clitics
		"kata-kata", "berkas-berkas", "sehari-hari", // doubled
		"unduhkan", // root + -kan
	}
	for _, w := range known {
		if !d.Known(w, nil) {
			t.Errorf("%q not known", w)
		}
	}
	for _, w := range []string{"kontk", "tampillan", "unduhh", "berkas-xyz"} {
		if d.Known(w, nil) {
			t.Errorf("%q known", w)
		}
	}
	if !d.Known("surel", map[string]bool{"surel": true}) {
		t.Error("project word not accepted")
	}
}

func TestWords(t *testing.T) {
	got := Words(`Buka %s atau %1$d <gui>Berkas</gui> {name} ${x} &amp; https://gnome.org a@b.id _Simpan GNOME iPhone v2 Kata-Kata ok`)
	want := "[Buka atau Berkas Simpan Kata-Kata ok]"
	if fmt.Sprint(got) != want {
		t.Errorf("Words = %v, want %v", got, want)
	}
}

func TestMisspelled(t *testing.T) {
	d := dict(t)
	got := d.Misspelled("Buka kontk Firefox, lalu simpan kontk", "Open contacts in Firefox", nil)
	if fmt.Sprint(got) != "[kontk]" { // Firefox is in the source, kontk is reported once
		t.Errorf("Misspelled = %v", got)
	}
	if got := (&Dict{}).Misspelled("kontk", "", nil); got != nil {
		t.Error("an empty dictionary reports words")
	}
}

func TestSuggest(t *testing.T) {
	d := dict(t)
	got := d.Suggest("kontk", 3)
	if len(got) == 0 || got[0] != "kontak" {
		t.Errorf("Suggest(kontk) = %v", got)
	}
	if got := d.Suggest("knotak", 3); len(got) == 0 || got[0] != "kontak" { // swapped letters
		t.Errorf("Suggest(knotak) = %v", got)
	}
}
