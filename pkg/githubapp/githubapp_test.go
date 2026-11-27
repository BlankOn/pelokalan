package githubapp_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pelokalan/pkg/githubapp"
	"pelokalan/pkg/githubapp/githubtest"
)

func TestClient(t *testing.T) {
	gh := githubtest.New()
	defer gh.Close()
	c, err := githubapp.New(gh.AppID, "pelokalan-test", gh.PrivateKey, gh.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if c.InstallURL() != "https://github.com/apps/pelokalan-test/installations/new" {
		t.Errorf("install URL %q", c.InstallURL())
	}
	if info, err := c.App(ctx); err != nil || info.Slug != "pelokalan-test" {
		t.Fatalf("app: %+v %v", info, err)
	}
	if _, err := c.Check(ctx, "owner/none"); !errors.Is(err, githubapp.ErrNotInstalled) {
		t.Fatalf("not installed: %v", err)
	}

	ro := gh.Install("owner/readonly", false)
	if a, err := c.Check(ctx, "owner/readonly"); err != nil || a.Write || a.Installation != ro {
		t.Fatalf("read-only check: %+v %v", a, err)
	}
	gh.SetFile("owner/readonly", "main", "a.po", []byte("x"))
	if _, err := c.PutFile(ctx, ro, "owner/readonly", "main", "a.po", []byte("y"), "sha", "m", githubapp.Person{}); !errors.Is(err, githubapp.ErrNoWrite) {
		t.Fatalf("read-only write: %v", err)
	}

	id := gh.Install("owner/repo", true)
	a, err := c.Check(ctx, "owner/repo")
	if err != nil || !a.Write || a.Installation != id {
		t.Fatalf("check: %+v %v", a, err)
	}
	gh.SetFile("owner/repo", "3.14", "library/os.po", []byte("msgid \"a\"\nmsgstr \"\"\n"))
	f, err := c.GetFile(ctx, id, "owner/repo", "3.14", "library/os.po")
	if err != nil || !strings.Contains(string(f.Data), `msgid "a"`) || f.SHA == "" {
		t.Fatalf("get: %+v %v", f, err)
	}
	if _, err := c.GetFile(ctx, id, "owner/repo", "3.14", "missing.po"); !errors.Is(err, githubapp.ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	cm, err := c.PutFile(ctx, id, "owner/repo", "3.14", "library/os.po", []byte("new"), f.SHA, "Update os",
		githubapp.Person{Name: "Budi", Email: "budi@example.com"})
	if err != nil || cm.SHA == "" || !strings.Contains(cm.URL, cm.SHA) {
		t.Fatalf("put: %+v %v", cm, err)
	}
	got := gh.LastCommits()
	if len(got) != 1 || got[0].AuthorEmail != "budi@example.com" || string(got[0].Data) != "new" || got[0].Branch != "3.14" {
		t.Fatalf("commit: %+v", got)
	}
	// the old sha again: someone (we) changed it meanwhile
	if _, err := c.PutFile(ctx, id, "owner/repo", "3.14", "library/os.po", []byte("newer"), f.SHA, "m", githubapp.Person{}); !errors.Is(err, githubapp.ErrConflict) {
		t.Fatalf("stale sha: %v", err)
	}
}
