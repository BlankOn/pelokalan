package web

import (
	"strings"
	"testing"
)

func TestCommentMarkdown(t *testing.T) {
	known := map[string]bool{"ani": true}
	got := string(commentHTML("**Penting**: lihat https://l10n.gnome.org/teams/id/ dan [panduan](https://example.com/a?b=1&c=2).\nBaris kedua @ani, bukan @nobody\n\n- satu\n- dua\n\n`@ani di kode`", known))
	for _, want := range []string{
		"<strong>Penting</strong>",
		`<a rel="noopener noreferrer nofollow" target="_blank" href="https://l10n.gnome.org/teams/id/">https://l10n.gnome.org/teams/id/</a>`,
		`href="https://example.com/a?b=1&amp;c=2">panduan</a>`,
		"<br",
		`<span class="mention">@ani</span>`,
		"@nobody",
		"<li>satu</li>",
		"<code>@ani di kode</code>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, `class="mention">@nobody`) {
		t.Error("unknown user rendered as mention")
	}
}

func TestCommentMarkdownSafety(t *testing.T) {
	for _, in := range []string{
		"<script>alert(1)</script>",
		`<img src=x onerror=alert(1)>`,
		"[klik](javascript:alert(1))",
		"[klik](vbscript:msgbox)",
		`<a href="javascript:alert(1)">x</a>`,
		"![x](javascript:alert(1))",
	} {
		out := strings.ToLower(string(commentHTML(in, nil)))
		for _, bad := range []string{"<script", "onerror", `href="javascript`, `src="javascript`, `href="vbscript`} {
			if strings.Contains(out, bad) {
				t.Errorf("%q rendered unsafely: %s", in, out)
			}
		}
	}
}

func TestLinkify(t *testing.T) {
	got := string(linkify(`Lihat https://l10n.gnome.org/teams/id/. <b>x</b> javascript:alert(1)`))
	want := `Lihat <a href="https://l10n.gnome.org/teams/id/" rel="noopener noreferrer nofollow" target="_blank">https://l10n.gnome.org/teams/id/</a>. &lt;b&gt;x&lt;/b&gt; javascript:alert(1)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}
