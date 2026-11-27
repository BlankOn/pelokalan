package source

import "testing"

func TestGitBranchName(t *testing.T) {
	for in, want := range map[string]string{
		"library/os.po":        "pelokalan/library-os",
		"library/os.path.po":   "pelokalan/library-os.path",
		"whatsnew/3.14.po":     "pelokalan/whatsnew-3.14",
		"c-api/init_config.po": "pelokalan/c-api-init_config",
		"faq/a b..c.po":        "pelokalan/faq-a-b-c",
		"..po":                 "pelokalan/file",
	} {
		if got := GitBranchName(in); got != want {
			t.Errorf("GitBranchName(%q) = %q, want %q", in, got, want)
		}
	}
}
