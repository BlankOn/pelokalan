package service

import (
	"strings"
	"testing"
)

func TestDiffSnapshotsHeaderAndMetadata(t *testing.T) {
	old := `# Kukuh <k@x.org>, 2022.
#
msgid ""
msgstr ""
"Last-Translator: Kukuh <k@x.org>\n"
"Language: id\n"

#: src/a.c:10
#, c-format
msgid "Open %s"
msgstr "Buka %s"

msgid "Close"
msgstr "Tutup"

msgid "Save"
msgstr "Simpan"
`
	cur := `# Kukuh <k@x.org>, 2022.
# herpiko <h@x.org>, 2026.
#
msgid ""
msgstr ""
"Last-Translator: herpiko <h@x.org>\n"
"Language: id\n"

#: src/a.c:12
#, c-format, no-wrap
msgid "Open %s"
msgstr "Buka %s"

msgid "Close"
msgstr "Tutup"

msgid "Save"
msgstr "Simpan berkas"
`
	d := DiffSnapshots([]byte(old), []byte(cur))
	if !d.HeaderChanged || d.Meta != 1 || d.Changed != 1 || d.Total() != 2 {
		t.Fatalf("counts: header=%v meta=%d changed=%d total=%d", d.HeaderChanged, d.Meta, d.Changed, d.Total())
	}
	var hdr []string
	for _, f := range d.HeaderChanges {
		hdr = append(hdr, f.Name+": "+f.Old+" -> "+f.New)
	}
	joined := strings.Join(hdr, " | ")
	if !strings.Contains(joined, "Last-Translator: Kukuh <k@x.org> -> herpiko <h@x.org>") ||
		!strings.Contains(joined, "header_comment:  -> herpiko <h@x.org>, 2026.") {
		t.Errorf("header changes: %s", joined)
	}
	var meta []string
	for _, it := range d.Items {
		if it.Kind == "meta" {
			for _, f := range it.Meta {
				meta = append(meta, f.Name)
			}
		}
	}
	if strings.Join(meta, ",") != "references,flags" {
		t.Errorf("entry metadata changes = %v, want references,flags", meta)
	}
}
