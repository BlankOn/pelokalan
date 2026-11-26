package gitpatch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const base = `# Indonesian translation
#
msgid ""
msgstr ""
"Project-Id-Version: Python 3.14\n"
"Language: id\n"

#: ../../library/os.rst:2
msgid "Process Parameters"
msgstr "Parameter Proses"

#: ../../library/os.rst:9
#, fuzzy, python-format
#| msgid "Old source"
msgid "Return %s"
msgstr "Kembalikan %s"

#: ../../library/os.rst:12
msgid ""
"The design of all built-in operating system dependent modules of Python is "
"such that as long as the same functionality is available."
msgstr ""

#: ../../library/os.rst:20
msgid "Untouched"
msgstr "Tidak disentuh"
`

func TestDiffOnlyChangedEntries(t *testing.T) {
	want := map[string]Target{
		"Process Parameters": {Translation: []string{"Parameter Proses"}}, // same: no change
		"Return %s":          {Translation: []string{"Mengembalikan %s"}}, // fixed and approved
		"The design of all built-in operating system dependent modules of Python is such that as long as the same functionality is available.": {
			Translation: []string{"Rancangan semua modul bawaan yang bergantung pada sistem operasi di Python dibuat sedemikian rupa sehingga selama fungsinya tersedia."},
			Fuzzy:       true, // an AI draft: exported as fuzzy
		},
		"Not in the repository": {Translation: []string{"x"}},
	}
	d := Diff("library/os.po", []byte(base), want)
	if d == nil || d.Changed != 2 {
		t.Fatalf("diff: %+v", d)
	}
	h := d.Hunks
	for _, s := range []string{
		"-#, fuzzy, python-format\n-#| msgid \"Old source\"\n+#, python-format\n", // fuzzy and #| gone, other flags kept
		" msgid \"Return %s\"\n", // the source stays context
		"-msgstr \"Kembalikan %s\"\n+msgstr \"Mengembalikan %s\"\n",
		"+#, fuzzy\n", // new flags line for the AI draft
		" msgstr \"\"\n+\"Rancangan semua modul bawaan yang bergantung pada sistem operasi di Python \"\n",
	} {
		if !strings.Contains(h, s) {
			t.Errorf("hunks miss %q:\n%s", s, h)
		}
	}
	if strings.Contains(h, "Tidak disentuh") || strings.Contains(h, "Project-Id-Version") {
		t.Errorf("unchanged parts in the diff:\n%s", h)
	}
	if Diff("library/os.po", []byte(base), map[string]Target{"Untouched": {Translation: []string{"Tidak disentuh"}}}) != nil {
		t.Error("no change should give no diff")
	}
}

func TestFormatPatch(t *testing.T) {
	d := Diff("library/os.po", []byte(base), map[string]Target{"Untouched": {Translation: []string{"Belum disentuh"}}})
	p := string(FormatPatch(Commit{AuthorName: "Budi Santoso", AuthorEmail: "budi@example.org",
		Date: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC), Subject: "Update Indonesian translation", Body: "From Pelokalan."}, []*FileDiff{d}))
	for _, s := range []string{"From: Budi Santoso <budi@example.org>\n", "Subject: [PATCH] Update Indonesian translation\n",
		"diff --git a/library/os.po b/library/os.po\n", "@@ -23,4 +23,4 @@\n", "-msgstr \"Tidak disentuh\"\n+msgstr \"Belum disentuh\"\n"} {
		if !strings.Contains(p, s) {
			t.Errorf("patch misses %q:\n%s", s, p)
		}
	}
}

func TestWrapLikeGettext(t *testing.T) {
	var b bytes.Buffer
	writeString(&b, "msgstr", "Untuk menutup \"objek berkas\" yang dikembalikan oleh fungsi bawaan :func:`open` atau :func:`popen`, gunakan metodenya.")
	got := b.String()
	// gettext never breaks right before ":", so "bawaan :func:`open`" stays together
	want := "msgstr \"\"\n\"Untuk menutup \\\"objek berkas\\\" yang dikembalikan oleh fungsi \"\n\"bawaan :func:`open` atau :func:`popen`, gunakan metodenya.\"\n"
	if got != want {
		t.Errorf("wrap:\n%s\nwant:\n%s", got, want)
	}
	b.Reset()
	writeString(&b, "msgstr", "Pendek")
	if b.String() != "msgstr \"Pendek\"\n" {
		t.Errorf("short: %q", b.String())
	}
}

// the patch applies with git am and changes exactly the wanted entries
func TestGitAm(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t", "HOME="+dir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "library"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "library", "os.po"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "base")
	want := map[string]Target{
		"Return %s": {Translation: []string{"Mengembalikan %s"}},
		"Untouched": {Translation: []string{"Belum disentuh"}},
	}
	d := Diff("library/os.po", []byte(base), want)
	patch := FormatPatch(Commit{AuthorName: "Budi Santoso", AuthorEmail: "budi@example.org", Date: time.Now(),
		Subject: "Update Indonesian translation"}, []*FileDiff{d})
	pf := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(pf, patch, 0o644); err != nil {
		t.Fatal(err)
	}
	run("am", "-q", pf)
	if got := run("log", "-1", "--format=%an <%ae> %s"); strings.TrimSpace(got) != "Budi Santoso <budi@example.org> Update Indonesian translation" {
		t.Errorf("commit: %q", got)
	}
	stat := run("show", "--stat", "--format=", "HEAD")
	if !strings.Contains(stat, "library/os.po | 7 +++----") {
		t.Errorf("stat: %s", stat)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "library", "os.po"))
	for _, s := range []string{"msgstr \"Mengembalikan %s\"", "#, python-format\nmsgid \"Return %s\"", "msgstr \"Belum disentuh\""} {
		if !strings.Contains(string(data), s) {
			t.Errorf("file misses %q", s)
		}
	}
}

func TestApply(t *testing.T) {
	want := map[string]Target{
		"Process Parameters": {Translation: []string{"Parameter Proses"}},
		"Return %s":          {Translation: []string{"Mengembalikan %s"}},
	}
	out, n := Apply([]byte(base), want)
	if n != 1 {
		t.Fatalf("changed = %d, want 1", n)
	}
	s := string(out)
	if !strings.Contains(s, "msgstr \"Mengembalikan %s\"\n") || strings.Contains(s, "Kembalikan %s") ||
		!strings.Contains(s, "Tidak disentuh") || !strings.HasSuffix(s, "\n") {
		t.Fatalf("applied file:\n%s", s)
	}
	// the result is what the patch describes: applying again changes nothing
	if _, n := Apply(out, want); n != 0 {
		t.Fatalf("second apply changed %d entries", n)
	}
	if d := Diff("x.po", out, want); d != nil {
		t.Fatalf("diff after apply: %+v", d)
	}
	if same, n := Apply([]byte(base), map[string]Target{}); n != 0 || string(same) != base {
		t.Fatal("nothing wanted should return the file itself")
	}
}
