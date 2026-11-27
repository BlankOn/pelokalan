package service

import (
	"strings"
	"testing"

	"pelokalan/internal/models"
)

func entry(id, plural string, tr ...string) *models.Entry {
	if len(tr) == 0 {
		tr = []string{""}
	}
	return &models.Entry{MsgID: id, MsgIDPlural: plural, MsgStr: tr}
}

func TestMergeEntries(t *testing.T) {
	tmpl := []*models.Entry{entry("Open", ""), entry("New", ""), entry("%d file", "%d files")}
	ctx := "menu"
	tmpl[0].Context = &ctx
	tmpl[0].References = "src/a.c:1"

	open := entry("Open", "", "Buka")
	open.Context = &ctx
	open.TranslatorComments = "keep me"
	current := []*models.Entry{
		open,
		entry("Gone", "", "Hilang"),
		entry("Gone untranslated", ""),
		entry("%d file", "%d files", "%d berkas"),
	}

	out, res := mergeEntries(tmpl, current, 1)
	if res.Added != 1 || res.Kept != 2 || res.Obsolete != 1 {
		t.Fatalf("result = %+v", res)
	}
	if len(out) != 4 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0].MsgStr[0] != "Buka" || out[0].References != "src/a.c:1" || out[0].TranslatorComments != "keep me" {
		t.Errorf("kept entry wrong: %+v", out[0])
	}
	if out[1].MsgID != "New" || out[1].IsTranslated() {
		t.Errorf("new entry wrong: %+v", out[1])
	}
	if len(out[2].MsgStr) != 1 || out[2].MsgStr[0] != "%d berkas" {
		t.Errorf("plural forms not fitted: %v", out[2].MsgStr)
	}
	if !out[3].Obsolete || out[3].MsgID != "Gone" {
		t.Errorf("obsolete entry wrong: %+v", out[3])
	}
	for i, e := range out {
		if e.Position != i {
			t.Errorf("position %d = %d", i, e.Position)
		}
	}
}

func TestDiffSnapshots(t *testing.T) {
	old := []byte("msgid \"\"\nmsgstr \"\"\n\nmsgid \"Open\"\nmsgstr \"Buka\"\n\nmsgid \"Close\"\nmsgstr \"\"\n\nmsgid \"Gone\"\nmsgstr \"Hilang\"\n")
	cur := []byte("msgid \"\"\nmsgstr \"\"\n\nmsgid \"Open\"\nmsgstr \"Buka\"\n\n#, fuzzy\nmsgid \"Close\"\nmsgstr \"Tutup\"\n\nmsgid \"New\"\nmsgstr \"Baru\"\n")
	d := DiffSnapshots(old, cur)
	if d.Added != 1 || d.Changed != 1 || d.Removed != 1 || len(d.Items) != 3 {
		t.Fatalf("diff = %+v", d)
	}
	if d.Items[0].Kind != "changed" || d.Items[0].Source != "Close" || !d.Items[0].NewFuzzy || d.Items[0].New[0] != "Tutup" {
		t.Errorf("changed item wrong: %+v", d.Items[0])
	}
	if DiffSnapshots(cur, cur).Empty() != true {
		t.Error("identical snapshots must be empty")
	}
	if d := DiffSnapshots(nil, cur); d.Added != 3 || d.HeaderChanged {
		t.Errorf("initial diff = %+v", d)
	}
}

func TestDiffReviewed(t *testing.T) {
	old := []byte("msgid \"\"\nmsgstr \"\"\n\n#, fuzzy, x-origin-ai\nmsgid \"Open\"\nmsgstr \"Buka\"\n\n#, x-origin-ai\nmsgid \"Close\"\nmsgstr \"Tutup\"\n")
	cur := []byte("msgid \"\"\nmsgstr \"\"\n\nmsgid \"Open\"\nmsgstr \"Buka\"\n\nmsgid \"Close\"\nmsgstr \"Tutup sekarang\"\n")
	d := DiffSnapshots(old, cur)
	if d.Reviewed != 1 || d.Changed != 1 || d.Total() != 2 {
		t.Fatalf("diff = %+v", d)
	}
	if d.Items[0].Kind != "reviewed" || d.Items[0].OldOrigin != "ai" || !d.Items[0].OldFuzzy || d.Items[0].NewFuzzy {
		t.Errorf("reviewed item = %+v", d.Items[0])
	}
	if d.Items[1].Kind != "changed" || d.Items[1].OldOrigin != "ai" || d.Items[1].NewOrigin != "" {
		t.Errorf("changed item = %+v", d.Items[1])
	}
}

func TestMentions(t *testing.T) {
	got := Mentions("Hi @ani and @Budi.S, see admin@blankon.id and @ani again. @x @ok_user.")
	want := "ani budi.s ok_user"
	if strings.Join(got, " ") != want {
		t.Errorf("mentions = %v, want %s", got, want)
	}
}
