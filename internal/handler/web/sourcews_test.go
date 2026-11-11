package web

import (
	"strings"
	"testing"
)

func TestSourceWSLineBreaks(t *testing.T) {
	cases := []struct {
		in, want string
		not      []string
	}{
		{"Hello\n", `Hello<span class="ws-tag ws-tag-nl ws-tag-end">↵ baris baru di akhir</span>`, nil},
		{"\nHello", `<span class="ws-tag ws-tag-nl ws-tag-start">↵ baris baru di awal</span>Hello`, nil},
		{"Hello\n\n", "↵ baris baru di akhir ×2", nil},
		{"Hello \n", `Hello <span class="ws-tag ws-tag-end">␣ spasi di akhir</span><span class="ws-tag ws-tag-nl ws-tag-end">↵ baris baru di akhir</span>`, nil},
		{"Hello ", "␣ spasi di akhir", []string{"baris baru"}},
		{"line one\nline two", "line one\nline two", []string{"ws-tag"}},
		{"\r\nHello\r\n", "Hello", []string{"×2"}},
	}
	for _, c := range cases {
		got := string(sourceWS("id", c.in))
		if !strings.Contains(got, c.want) {
			t.Errorf("sourceWS(%q) = %q, want it to contain %q", c.in, got, c.want)
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("sourceWS(%q) = %q, must not contain %q", c.in, got, n)
			}
		}
	}
}
