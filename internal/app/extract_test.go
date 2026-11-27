package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeExtractAI answers glossary extraction requests like a model would: for every item it names the pairs listed
// for its source text, plus one invented pair that the check must drop
type fakeExtractAI struct {
	*httptest.Server
	mu    sync.Mutex
	calls [][]string // source texts per request
}

func newFakeExtractAI(answers map[string][][2]string) *fakeExtractAI {
	f := &fakeExtractAI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var in struct {
			Items []struct {
				ID     int64  `json:"id"`
				Source string `json:"source"`
			} `json:"items"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &in)
		type pair struct {
			S string `json:"s"`
			T string `json:"t"`
		}
		var out struct {
			Items []struct {
				ID    int64  `json:"id"`
				Terms []pair `json:"terms"`
			} `json:"items"`
		}
		var sources []string
		for _, it := range in.Items {
			sources = append(sources, it.Source)
			terms := []pair{{"standard", "invented"}} // not in the translation: dropped
			for _, a := range answers[it.Source] {
				terms = append(terms, pair{a[0], a[1]})
			}
			out.Items = append(out.Items, struct {
				ID    int64  `json:"id"`
				Terms []pair `json:"terms"`
			}{it.ID, terms})
		}
		f.mu.Lock()
		f.calls = append(f.calls, sources)
		f.mu.Unlock()
		content, _ := json.Marshal(out)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}},
			"usage":   map[string]int{"prompt_tokens": 1000, "completion_tokens": 200},
		})
	}))
	return f
}

func (f *fakeExtractAI) requests() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.calls...)
}

// waitExtract waits until the project has a finished extraction run, and returns how many it has
func waitExtract(t *testing.T, pid int64, runs int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n := scalar[int](t, `SELECT COUNT(*) FROM term_extraction_jobs WHERE project_id = $1 AND status <> 'running'`, pid)
		if n >= runs {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("extraction run %d did not finish", runs)
}

func TestGlossaryExtraction(t *testing.T) {
	long := "This module is no longer part of the Python standard library. It was :ref:`removed in Python 3.13 " +
		"<whatsnew313-pep594>` after being deprecated in Python 3.11."
	longTr := "Modul ini tidak lagi menjadi bagian dari pustaka standar Python. Modul ini :ref:`dihapus di Python 3.13 " +
		"<whatsnew313-pep594>` setelah dinyatakan usang di Python 3.11."
	added := "The removal of the old parser was decided in this release."
	addedTr := "Penghapusan pengurai lama ditetapkan dalam rilis ini."
	fake := newFakeExtractAI(map[string][][2]string{
		long:  {{"module", "Modul"}, {"library", "pustaka"}, {"removed", "dihapus"}, {"deprecated", "dinyatakan usang"}},
		added: {{"removal", "Penghapusan"}, {"decided", "ditetapkan"}, {"parser", "pengurai"}},
	})
	defer fake.Close()

	owner := signUp(t, "extract")
	slug := owner.newProject("Extract")
	base := "/projects/" + slug
	pid := scalar[int64](t, `SELECT id FROM projects WHERE slug = $1`, slug)
	owner.post(base+"/ai", url.Values{"base_url": {fake.URL}, "model": {"gpt-4o-mini"}, "api_key": {"sk-test-0123456789"}}).
		redirectsTo(base + "/settings")

	// a second file that no sync changes: its strings are left to the button
	backlog := "Use the keyword arguments to change the behaviour of this function."
	backlogTr := "Gunakan argumen kata kunci untuk mengubah perilaku fungsi ini."
	sys := poFile("sys", backlog, backlogTr)
	upload := func(pairs ...string) {
		owner.upload(base+"/module-groups/import", url.Values{"name": {"docs"}, "version_name": {"1"}, "language": {"id"}},
			"docs.tar.gz", targz(t, map[string][]byte{"os.po": poFile("os", pairs...), "sys.po": sys})).redirectsTo(base + "/settings?")
	}
	upload("Library", "Pustaka", long, longTr)
	gid := scalar[int64](t, `SELECT id FROM module_groups WHERE project_id = $1`, pid)
	g := strconv.FormatInt(gid, 10)

	// owners and coordinators see the button; visitors do not
	owner.get(base+"/glossary").status(http.StatusOK).contains("Ekstrak istilah dengan AI", `id="extract-dialog"`)
	visitor(t).get(base + "/glossary").status(http.StatusOK).lacks("Ekstrak istilah dengan AI")
	visitor(t).get(base + "/glossary-extract?group=" + g).redirectsTo("/login")

	owner.get(base+"/glossary-extract?group="+g).status(http.StatusOK).contains(`"to_send":2`, "2 string panjang")
	owner.post(base+"/glossary-extract", url.Values{"group": {g}}).status(http.StatusOK).contains("dimulai")
	waitExtract(t, pid, 1)
	if reqs := fake.requests(); len(reqs) != 1 || len(reqs[0]) != 2 {
		t.Fatalf("requests = %q", reqs)
	}

	// checked pairs are glossary rows marked AI; the invented one is not
	res := owner.get(base + "/glossary/search?f=1&terms=1&group=all&q=dihapus").status(http.StatusOK)
	res.contains("removed", "dihapus", `class="gl-ai"`)
	owner.get(base + "/glossary/search?f=1&terms=1&group=all&q=invented").status(http.StatusOK).lacks("invented")
	owner.get(base + "/glossary/search?f=1&terms=1&group=all&q=deprecated").status(http.StatusOK).contains("dinyatakan usang")
	if n := scalar[int](t, `SELECT COUNT(*) FROM glossary_terms WHERE project_id = $1 AND origin = 'ai'`, pid); n != 4 {
		t.Fatalf("AI rows = %d, want 4", n)
	}
	// "library" is also known from a person's short string: not marked as found by AI only
	if fromAI := scalar[bool](t, `SELECT bool_and(origin = 'ai') FROM glossary_terms WHERE project_id = $1 AND source_norm = 'library'`, pid); fromAI {
		t.Fatal("library marked as AI only")
	}
	owner.get(base+"/glossary-extract/status").status(http.StatusOK).contains(`"running":false`, "Ekstraksi selesai")

	// sys.po's string was never extracted (as if the first run had been stopped)
	sum := sha256.Sum256([]byte(backlog + "\x00" + backlogTr))
	if _, err := env.DB.Exec(`DELETE FROM term_extractions WHERE string_hash = $1`, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	// a sync that changes os.po only: just its new string is sent (not sys.po's backlog), the earlier pairs come back
	// from the stored results
	upload("Library", "Pustaka", long, longTr, added, addedTr)
	waitExtract(t, pid, 2)
	reqs := fake.requests()
	if len(reqs) != 2 || len(reqs[1]) != 1 || reqs[1][0] != added {
		t.Fatalf("requests after sync = %q", reqs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for scalar[int](t, `SELECT COUNT(*) FROM glossary_terms WHERE project_id = $1 AND origin = 'ai'`, pid) != 7 {
		if time.Now().After(deadline) {
			t.Fatal("AI rows not rebuilt after the sync")
		}
		time.Sleep(30 * time.Millisecond)
	}
	owner.get(base + "/glossary/search?f=1&terms=1&group=all&q=pengurai").status(http.StatusOK).contains("parser")
	owner.get(base + "/glossary-extract?group=" + g).status(http.StatusOK).contains(`"to_send":1`) // the backlog, for the button
	if strings.Contains(scalar[string](t, `SELECT error FROM term_extraction_jobs WHERE project_id = $1 ORDER BY id DESC LIMIT 1`, pid), "err") {
		t.Fatal("sync run failed")
	}
	// while a run is going the button gives way to its progress and "Hentikan"; a run the server lost (only in the
	// database) is closed by Hentikan too
	owner.get(base + "/glossary").status(http.StatusOK).contains("data-extract-open>").lacks("data-extract-open hidden")
	if _, err := env.DB.Exec(`INSERT INTO term_extraction_jobs (project_id, group_id, status, model, total)
		VALUES ($1, $2, 'running', 'gpt-4o-mini', 10)`, pid, gid); err != nil {
		t.Fatal(err)
	}
	owner.get(base+"/glossary").status(http.StatusOK).contains("data-extract-open hidden", "data-extract-stop")
	visitor(t).post(base+"/glossary-extract/cancel", nil).redirectsTo("/login")
	owner.post(base+"/glossary-extract/cancel", nil).status(http.StatusOK)
	if st := scalar[string](t, `SELECT status FROM term_extraction_jobs WHERE project_id = $1 ORDER BY id DESC LIMIT 1`, pid); st != "cancelled" {
		t.Fatalf("status after Hentikan = %q", st)
	}
	owner.get(base + "/glossary").status(http.StatusOK).lacks("data-extract-open hidden")
}
