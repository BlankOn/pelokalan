package project

import "testing"

func TestSlugs(t *testing.T) {
	cases := map[string]string{"GNOME ID": "gnome-id", "  BlankOn: Linux!! ": "blankon-linux", "51": "project-51", "Ab": "project-ab", "": "project"}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ok := range []string{"gnome-id", "gnome51", "a1b"} {
		if !ValidSlug(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"123", "-ab", "ab-", "a--b", "Gnome", "ab", "gnome_id"} {
		if ValidSlug(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
