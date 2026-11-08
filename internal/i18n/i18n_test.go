package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLanguagesHaveSameKeys(t *testing.T) {
	for _, k := range Keys(ID) {
		if _, ok := messages[EN][k]; !ok {
			t.Errorf("key %q missing in en", k)
		}
	}
	for _, k := range Keys(EN) {
		if _, ok := messages[ID][k]; !ok {
			t.Errorf("key %q missing in id", k)
		}
	}
}

// Every key used by templates and Go code must exist.
func TestUsedKeysExist(t *testing.T) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bt \$?\.?Lang "([a-z0-9_.]+)"`),
		regexp.MustCompile(`i18n\.T\([^,()]+, "([a-z0-9_.]+)"`),
		regexp.MustCompile(`userErr\("([a-z0-9_.]+)"`),
		regexp.MustCompile(`Key: "([a-z0-9_.]+)"`),
		regexp.MustCompile(`h\.page\(r, "([a-z0-9_.]+)"`),
	}
	flashRe := regexp.MustCompile(`flash=([a-z_]+)`)
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !(strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".go")) || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var keys []string
		for _, re := range patterns {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				keys = append(keys, m[1])
			}
		}
		for _, m := range flashRe.FindAllStringSubmatch(string(b), -1) {
			keys = append(keys, "flash."+m[1])
		}
		for _, k := range keys {
			if _, ok := messages[ID][k]; !ok && !strings.HasSuffix(k, ".") {
				t.Errorf("%s: unknown key %q", path, k)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"role.owner", "role.manager", "role.translator", "issue.placeholders", "issue.newline_start", "issue.newline_end", "issue.plural_count"} {
		if !Has(prefix) {
			t.Errorf("missing dynamic key %q", prefix)
		}
	}
}
