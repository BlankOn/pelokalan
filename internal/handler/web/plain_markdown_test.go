package web

import "testing"

func TestPlainMarkdown(t *testing.T) {
	cases := map[string]string{
		"Pakai **direktori** dan `os.path`.":              "Pakai direktori dan os.path.",
		"Lihat [panduan](https://example.org/x) ini":      "Lihat panduan ini",
		"![tangkapan layar](/projects/x/attachments/1/a)": "🖼 tangkapan layar",
		"> kutipan\n- butir satu\n## Judul":               "kutipan\nbutir satu\nJudul",
		"biasa saja":                                      "biasa saja",
	}
	for in, want := range cases {
		if got := plainMarkdown(in); got != want {
			t.Errorf("plainMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
}
