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
