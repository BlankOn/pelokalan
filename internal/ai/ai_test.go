package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testGlossary() *Glossary {
	return NewGlossary([]Term{
		{Source: "Open", Translation: "Buka", Share: 0.95, Words: 1},
		{Source: "Open Link", Translation: "Buka Tautan", Share: 0.62, Words: 2},
		{Source: "Link", Translation: "Tautan", Share: 0.9, Words: 1},
		{Source: "File", Translation: "Berkas", Share: 0.9, Words: 1},
		{Source: "Folder", Translation: "Folder", Share: 0.5, Words: 1},
		{Source: "Keyboard Shortcuts", Translation: "Pintasan Papan Tik", Share: 0.8, Words: 2},
	})
}

func TestGlossaryMatches(t *testing.T) {
	g := testGlossary()
	got := g.Matches("_Open Link in New File…")
	SortTerms(got)
	if len(got) != 2 || got[0].Term != "File" || got[1].Term != "Open Link" {
		t.Fatalf("matches = %+v (longest phrase must win over its words)", got)
	}
	if m := g.Matches("Show %s keyboard shortcuts"); len(m) != 1 || m[0].Translation != "Pintasan Papan Tik" {
		t.Fatalf("case-insensitive phrase match failed: %+v", m)
	}
	if m := g.Matches("Opening"); len(m) != 0 {
		t.Fatalf("partial word must not match: %+v", m)
	}
}

func TestGlossaryExact(t *testing.T) {
	g := testGlossary()
	if tr, ok := g.Exact("Open"); !ok || tr != "Buka" {
		t.Error("exact match expected")
	}
	if _, ok := g.Exact("_Open"); ok {
		t.Error("accelerator must go to the model")
	}
	if _, ok := g.Exact("Folder"); ok {
		t.Error("inconsistent term must not be applied directly")
	}
}

func TestVerify(t *testing.T) {
	it := Item{Source: "Open %s", Flags: []string{"c-format"}, Glossary: []TermHint{{"Open", "Buka"}}}
	if p := Verify(it, []string{"Buka %s"}, 1); len(p) != 0 {
		t.Errorf("good translation flagged: %v", p)
	}
	if p := Verify(it, []string{"_Membuka %s"}, 1); len(p) != 0 {
		t.Errorf("affix + accelerator should pass: %v", p)
	}
	if p := Verify(it, []string{"Lihat %s"}, 1); len(p) != 1 || p[0] != ProblemGlossary {
		t.Errorf("glossary violation not detected: %v", p)
	}
	if p := Verify(it, []string{"Buka"}, 1); len(p) == 0 || p[0] != ProblemFormat {
		t.Errorf("missing placeholder not detected: %v", p)
	}
}

func TestParseAndBatch(t *testing.T) {
	m, err := ParseResponse("```json\n{\"translations\":[{\"id\":7,\"forms\":[\"Buka\"]},{\"id\":8,\"text\":\"Tutup\"}]}\n```")
	if err != nil || m[7][0] != "Buka" || m[8][0] != "Tutup" {
		t.Fatalf("parse: %v %v", err, m)
	}
	items := make([]Item, 55)
	for i := range items {
		items[i] = Item{ID: int64(i), Source: "Hello world"}
	}
	if b := Batch(items, 25, 100000); len(b) != 3 || len(b[2]) != 5 {
		t.Fatalf("batches = %d", len(b))
	}
	est := EstimateBatches(Batch(items, 25, 100000), SystemPrompt("English", "Indonesian"))
	if est.PromptTokens <= 0 || est.CompletionTokens <= 0 || est.Batches != 3 {
		t.Fatalf("estimate = %+v", est)
	}
}

func TestBox(t *testing.T) {
	b := NewBox("secret")
	sealed, err := b.Seal("sk-test-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "sk-test") {
		t.Fatal("not encrypted")
	}
	if got, err := b.Open(sealed); err != nil || got != "sk-test-1234567890" {
		t.Fatalf("open: %v %q", err, got)
	}
	if _, err := NewBox("other").Open(sealed); err == nil {
		t.Fatal("wrong key must fail")
	}
	if KeyHint("sk-proj-abcdefghijkl") != "sk-…ijkl" {
		t.Errorf("hint = %q", KeyHint("sk-proj-abcdefghijkl"))
	}
}

func TestClient(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if calls == 1 { // first call fails transiently
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"translations\":[]}"}}],"usage":{"prompt_tokens":120,"completion_tokens":30}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "good", "m", true)
	c.backoff = 0
	_, usage, err := c.Chat(context.Background(), "s", "u")
	if err != nil || usage.PromptTokens != 120 || calls != 2 {
		t.Fatalf("chat: %v %+v calls=%d", err, usage, calls)
	}
	if _, _, err := NewClient(srv.URL, "bad", "m", true).Chat(context.Background(), "s", "u"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized: %v", err)
	}
	if _, _, err := NewClient(srv.URL, "good", "m", false).Chat(context.Background(), "s", "u"); err == nil {
		t.Fatal("private address must be blocked")
	}
}
