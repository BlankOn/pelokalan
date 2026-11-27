package web

import (
	"testing"

	"pelokalan/internal/models"
)

func TestAnchorMarks(t *testing.T) {
	text := `Modul ini <dihapus> setelah dinyatakan usang; dinyatakan lagi`
	cases := []struct {
		name    string
		anchors []models.CommentAnchor
		want    string
	}{
		{"none", nil, `Modul ini &lt;dihapus&gt; setelah dinyatakan usang; dinyatakan lagi`},
		{"nearest occurrence", []models.CommentAnchor{{CommentID: 7, Text: "dinyatakan", Start: 48, Author: "Ani", Body: "pakai <diumumkan>"}},
			`Modul ini &lt;dihapus&gt; setelah dinyatakan usang; <mark class="rv-anchor" data-comment="7" title="Ani: pakai &lt;diumumkan&gt;">dinyatakan</mark> lagi`},
		{"moved text still marked", []models.CommentAnchor{{CommentID: 1, Text: "usang", Start: 2}},
			`Modul ini &lt;dihapus&gt; setelah dinyatakan <mark class="rv-anchor" data-comment="1" title="">usang</mark>; dinyatakan lagi`},
		{"gone from the text", []models.CommentAnchor{{CommentID: 1, Text: "kuno", Start: 0}},
			`Modul ini &lt;dihapus&gt; setelah dinyatakan usang; dinyatakan lagi`},
		{"other plural form", []models.CommentAnchor{{CommentID: 1, Text: "usang", Form: 1}},
			`Modul ini &lt;dihapus&gt; setelah dinyatakan usang; dinyatakan lagi`},
		{"overlap keeps the first", []models.CommentAnchor{{CommentID: 1, Text: "setelah dinyatakan", Start: 20}, {CommentID: 2, Text: "dinyatakan usang", Start: 28}},
			`Modul ini &lt;dihapus&gt; <mark class="rv-anchor" data-comment="1" title="">setelah dinyatakan</mark> usang; dinyatakan lagi`},
	}
	for _, c := range cases {
		if got := string(anchorMarks(text, c.anchors)); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
