package glossary

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

const poA = `msgid ""
msgstr ""
"Language: id\n"

msgid "_Open"
msgstr "_Buka"

msgid "Open…"
msgstr "Buka…"

#, fuzzy
msgid "Close"
msgstr "Tutup"

msgid "%d"
msgstr "%d"

msgid "translator-credits"
msgstr "Someone <a@b.c>"

msgctxt "menu"
msgid "Save"
msgstr "Simpan"
`

// no Language header: detected from the file name
const poB = `msgid ""
msgstr ""

msgid "Open"
msgstr "Membuka"

msgid "Save"
msgstr "Simpan"
`

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestReadFilesFormats(t *testing.T) {
	tgz := tarGz(t, map[string]string{"a.master.id.po": poA, "../evil/b.main.id.po": poB, "README": "x"})
	files, err := ReadFiles("x.tar.gz", tgz, Limits{})
	if err != nil || len(files) != 2 {
		t.Fatalf("tar.gz: %v %d", err, len(files))
	}
	for _, f := range files {
		if f.Name != "a.master.id.po" && f.Name != "b.main.id.po" {
			t.Errorf("unexpected name %q", f.Name)
		}
	}

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("dir/a.id.po")
	w.Write([]byte(poA))
	zw.Close()
	if files, err := ReadFiles("x.zip", zbuf.Bytes(), Limits{}); err != nil || len(files) != 1 {
		t.Fatalf("zip: %v", err)
	}

	if files, err := ReadFiles("a.id.po", []byte(poA), Limits{}); err != nil || len(files) != 1 {
		t.Fatalf("po: %v", err)
	}
	if _, err := ReadFiles("x.txt", []byte("hello"), Limits{}); err != ErrUnsupported {
		t.Fatalf("txt: %v", err)
	}
	if _, err := ReadFiles("x.tar.gz", tgz, Limits{MaxFiles: 1, MaxFileBytes: 1 << 20, MaxTotalBytes: 1 << 20}); err != ErrTooLarge {
		t.Fatalf("limit: %v", err)
	}
}

func TestBuild(t *testing.T) {
	res := Build([]File{{"a.master.id.po", []byte(poA)}, {"b.main.id.po", []byte(poB)}}, "")
	if res.Files != 2 || len(res.Skipped) != 0 || len(res.Languages) != 1 || res.Languages[0] != "id" {
		t.Fatalf("result = %+v", res)
	}
	// _Open/Open… -> Buka (2 occurrences, module a.master), Open -> Membuka, Save -> Simpan (2)
	got := map[string]*Term{}
	for _, term := range res.Terms {
		got[term.SourceNorm+"="+term.TranslationNorm] = term
	}
	if len(got) != 3 {
		t.Fatalf("terms = %v", got)
	}
	if b := got["open=buka"]; b == nil || b.Occurrences != 2 || b.Source != "Open" || b.Modules[0] != "a.master" {
		t.Errorf("open=buka wrong: %+v", b)
	}
	if s := got["save=simpan"]; s == nil || s.Occurrences != 2 || len(s.Modules) != 2 || s.Contexts[0] != "menu" {
		t.Errorf("save=simpan wrong: %+v", s)
	}
	if res.Entries != 5 {
		t.Errorf("entries = %d, want 5", res.Entries)
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"_Open…":           "Open",
		"Save _As...":      "Save As",
		"&File":            "File",
		"Name:":            "Name",
		"file_name":        "file_name",
		"a_b c_d":          "a_b c_d",
		"  Many   spaces ": "Many spaces",
		"Rock & Roll":      "Rock & Roll",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
	pairs := [][4]string{
		{"_Open", "B_uka", "Open", "Buka"},
		{"_Cancel", "Ba_tal", "Cancel", "Batal"},
		{"file_name", "nama_berkas", "file_name", "nama_berkas"},
		{"Save", "Simpan_", "Save", "Simpan_"},
	}
	for _, p := range pairs {
		if s, tr := CleanPair(p[0], p[1]); s != p[2] || tr != p[3] {
			t.Errorf("CleanPair(%q, %q) = %q, %q", p[0], p[1], s, tr)
		}
	}
	if ModuleName("gtk.gtk-4-24.id.po", "id") != "gtk.gtk-4-24" {
		t.Error("module name")
	}
}
