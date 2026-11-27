package ai

import (
	"reflect"
	"testing"
)

func TestCheckPairs(t *testing.T) {
	it := ExtractItem{
		Source: "This module is no longer part of the Python standard library. It was :ref:`removed in Python 3.13 " +
			"<whatsnew313-pep594>` after being deprecated in Python 3.11.  The removal was decided in :pep:`594`. See ``os.path``.",
		Translation: "Modul ini tidak lagi menjadi bagian dari pustaka standar Python. Modul ini :ref:`dihapus di Python 3.13 " +
			"<whatsnew313-pep594>` setelah dinyatakan usang di Python 3.11. Penghapusan ini ditetapkan dalam :pep:`594`. Lihat ``os.path``.",
	}
	answer := `Here you go:
{"items":[{"id":1,"terms":[
 {"s":"module","t":"Modul"},{"s":"standard library","t":"pustaka standar"},{"s":"standard","t":"standar"},
 {"s":"library","t":"pustaka"},{"s":"removed","t":"dihapus"},["after","setelah"],{"s":"deprecated","t":"dinyatakan usang"},
 {"s":"Removal","t":"Penghapusan"},{"s":"decided","t":"ditetapkan"},
 {"s":"the","t":"ini"},{"s":"Python","t":"Python"},{"s":"os.path","t":"os.path"},{"s":"part","t":"komponen"},
 {"s":"whatsnew313","t":"whatsnew313"},{"s":"module","t":"modul"},{"s":"obsolete","t":"usang"}]}]}`
	got, err := ParseExtractResponse(answer)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pair{{"module", "Modul"}, {"standard library", "pustaka standar"}, {"standard", "standar"},
		{"library", "pustaka"}, {"removed", "dihapus"}, {"after", "setelah"}, {"deprecated", "dinyatakan usang"},
		{"Removal", "Penghapusan"}, {"decided", "ditetapkan"}}
	if kept := CheckPairs(it, got[1]); !reflect.DeepEqual(kept, want) {
		t.Errorf("kept %v\nwant %v", kept, want)
	}
}

func TestExtractCandidate(t *testing.T) {
	for _, c := range []struct {
		src  string
		want bool
	}{{"Open File", false}, {"Show the hidden files here", true}, {"%s of %d %s", false}, {":ref:`x` `y` <b>z</b>", false}} {
		if got := ExtractCandidate(c.src, "x"); got != c.want {
			t.Errorf("%q: got %v", c.src, got)
		}
	}
}

func TestExtractBatch(t *testing.T) {
	items := []ExtractItem{{ID: 1, Source: "aaaa"}, {ID: 2, Source: "bbbb"}, {ID: 3, Source: "cccc"}}
	if b := ExtractBatch(items, 2, 100); len(b) != 2 || len(b[0]) != 2 {
		t.Errorf("by count: %v", b)
	}
	if b := ExtractBatch(items, 10, 9); len(b) != 2 {
		t.Errorf("by size: %v", b)
	}
}
