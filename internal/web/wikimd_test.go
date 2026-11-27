package web

import (
	"strings"
	"testing"
)

func TestWikiHTML(t *testing.T) {
	got := string(wikiHTML("# Panduan Gaya\n\n![logo](/projects/x/wiki/files/1/a.png) ![luar](https://evil.example/p.png)\n\n" +
		"[situs](https://l10n.gnome.org) [lokal](/projects/x/wiki/lain)\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"))
	for _, want := range []string{
		`<h1 id="panduan-gaya">Panduan Gaya</h1>`,
		`<img src="/projects/x/wiki/files/1/a.png" alt="logo">`,
		`target="_blank" href="https://evil.example/p.png">luar</a>`,
		`<a rel="noopener noreferrer nofollow" target="_blank" href="https://l10n.gnome.org">situs</a>`,
		`<a href="/projects/x/wiki/lain">lokal</a>`,
		`<table>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	for _, bad := range []string{"<script", "javascript:", `<img src="https://evil`} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe output %q in\n%s", bad, got)
		}
	}
}

func TestWikiFileLinks(t *testing.T) {
	out := string(wikiHTML("[📎 notes.pdf (1.2 MB)](/projects/gnome-id/wiki/files/12/notes.pdf) and [page](/projects/gnome-id/wiki/other)"))
	if !strings.Contains(out, `class="wiki-file"`) || !strings.Contains(out, `download`) {
		t.Errorf("attachment link not marked: %s", out)
	}
	if strings.Count(out, `class="wiki-file"`) != 1 {
		t.Errorf("only links to wiki files are attachments: %s", out)
	}
}
