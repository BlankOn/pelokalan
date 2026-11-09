package thesaurus

import "testing"

func TestParseAndLookup(t *testing.T) {
	data := []byte(`{
		"menampilkan": {"tag": "v", "sinonim": ["memperlihatkan", " menyajikan ", "menyajikan", "menampilkan"]},
		"kunci": {"tag": "n", "sinonim": ["gerendel", "ki akal"], "antonim": []},
		"gagal": {"tag": "a", "sinonim": ["batal"], "antonim": ["berhasil"]},
		"kosong": {"tag": "a", "sinonim": []},
		"Gagal ": {"tag": "a", "sinonim": ["kandas"]}
	}`)
	entries, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries without synonyms or antonyms must be skipped, got %d", len(entries))
	}
	var x Index
	x.Set(entries)
	e, ok := x.Lookup("  Menampilkan, ")
	if !ok || len(e.Synonyms) != 2 || e.Synonyms[1] != "menyajikan" {
		t.Fatalf("lookup/cleaning failed: %+v", e)
	}
	if e, _ := x.Lookup("kunci"); e.Synonyms[1] != "akal" {
		t.Fatalf("figurative marker not removed: %+v", e.Synonyms)
	}
	if e, _ := x.Lookup("GAGAL"); len(e.Antonyms) != 1 || e.Tag != "a" || len(e.Synonyms) != 2 {
		t.Fatalf("antonyms/tag: %+v", e)
	}
	if _, ok := x.Lookup("tidakada"); ok {
		t.Fatal("unknown word found")
	}
	if _, err := Parse([]byte(`{}`)); err == nil {
		t.Fatal("empty data must fail")
	}
}

func TestLookupForms(t *testing.T) {
	var x Index
	x.Set([]Entry{
		{Word: "menampilkan", Tag: "v", Synonyms: []string{"memperlihatkan"}},
		{Word: "memilih", Tag: "v", Synonyms: []string{"menyeleksi"}},
		{Word: "mengirim", Tag: "v", Synonyms: []string{"mengutus"}},
		{Word: "menyimpan", Tag: "v", Synonyms: []string{"menaruh"}},
		{Word: "mengubah", Tag: "v", Synonyms: []string{"mengganti"}},
		{Word: "membuka", Tag: "v", Synonyms: []string{"menguak"}},
		{Word: "tampilan", Tag: "n", Synonyms: []string{"bentuk"}},
	})
	for in, want := range map[string]string{
		"Tampilkan": "menampilkan", "ditampilkan": "menampilkan", "pilih": "memilih", "Kirim": "mengirim",
		"simpan": "menyimpan", "Ubah": "mengubah", "buka": "membuka", "dibukanya": "membuka", "Tampilan": "tampilan",
	} {
		e, ok := x.LookupForms(in)
		if !ok || e.Word != want {
			t.Errorf("LookupForms(%q) = %v, want %s", in, e, want)
		}
	}
	if _, ok := x.LookupForms("zzzz"); ok {
		t.Error("unknown word found")
	}
}
