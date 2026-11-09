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
	it := Item{Source: "Open %s", Flags: []string{"c-format"}, Glossary: []TermHint{{Term: "Open", Translation: "Buka"}}}
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
	est := EstimateBatches(Batch(items, 25, 100000), SystemPrompt("English", "Indonesian", "", ""))
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

func TestSystemPromptFileContext(t *testing.T) {
	plain := SystemPrompt("English", "Indonesian", "", "  ")
	if strings.Contains(plain, "<file_context>") {
		t.Fatal("empty context must not add a section")
	}
	with := SystemPrompt("English", "Indonesian", "", "Orca screen reader UI. \"flat review\" = \"tinjau rata\".")
	if !strings.HasPrefix(with, plain) || !strings.Contains(with, "<file_context>\nOrca screen reader UI.") {
		t.Fatalf("context section missing or rules not first:\n%s", with)
	}
	long := SystemPrompt("English", "Indonesian", "", strings.Repeat("x", MaxFileContext+500))
	if !strings.Contains(long, "\n"+strings.Repeat("x", MaxFileContext)+"…\n</file_context>") {
		t.Fatal("long context is not capped")
	}
}

func TestContextMakesGlossaryOptional(t *testing.T) {
	note := "Tecla adalah aplikasi untuk menampilkan layout papan tik. Bahasa inggris `key` di sini kemungkinan besar adalah `tombol`, bukan `kunci`."
	words := ContextWords(note)
	if !Mentioned("Key Description", words) || !Mentioned("_Keys…", words) && Mentioned("Show _Description", words) {
		t.Fatal("terms with a word from the note must be detected")
	}
	if Mentioned("Show Description", words) || Mentioned("Open", ContextWords("")) {
		t.Fatal("unrelated terms must stay required")
	}
	// words only used in a sentence or example do not override the glossary; quoted ones do
	example := ContextWords("Contoh, key description adalah pola MD; diterjemahkan menjadi Deskripsi tombol.")
	if Mentioned("Description", example) || Mentioned("Key", example) {
		t.Fatal("unquoted words must not override the glossary")
	}
	quoted := ContextWords(`Istilah: "flat review" = “tinjau rata”, dan ` + "`Keys`.")
	if !Mentioned("Flat Review", quoted) || !Mentioned("_Keys…", quoted) || Mentioned("Review Changes", ContextWords("review")) {
		t.Fatal("quoted words must override the glossary")
	}
	hints := MarkOptional([]TermHint{{Term: "Key", Translation: "Kunci"}, {Term: "Description", Translation: "Deskripsi"}}, words)
	if !hints[0].Optional || hints[1].Optional {
		t.Fatalf("got %+v", hints)
	}
	// an optional term missing from the result is not a glossary problem; a required one is
	it := Item{Source: "Key Description", Glossary: hints}
	if p := Verify(it, []string{"Deskripsi Tombol"}, 2); len(p) != 0 {
		t.Fatalf("Deskripsi Tombol should pass, got %v", p)
	}
	if p := Verify(it, []string{"Keterangan Tombol"}, 2); len(p) == 0 {
		t.Fatal("missing required term must be flagged")
	}
	if !strings.Contains(SystemPrompt("English", "Indonesian", "", note), `"optional": true`) {
		t.Fatal("prompt must explain optional terms")
	}
}

func TestSystemPromptProjectContext(t *testing.T) {
	p := SystemPrompt("English", "Indonesian", "Gunakan bahasa santai.", "Berkas Tecla.")
	pi, fi := strings.Index(p, "<project_context>\nGunakan bahasa santai."), strings.Index(p, "<file_context>\nBerkas Tecla.")
	if pi < 0 || fi < 0 || pi > fi {
		t.Fatalf("project context must come before the file context:\n%s", p)
	}
	if strings.Contains(SystemPrompt("English", "Indonesian", " ", ""), "<project_context>") {
		t.Fatal("empty project context must not add a section")
	}
}

func TestCurrentTranslationIsSent(t *testing.T) {
	it := Item{ID: 7, Source: "Key description:", Current: []string{"Deskripsi kunci:"}}
	if !strings.Contains(UserPrompt([]Item{it}), `"current":["Deskripsi kunci:"]`) {
		t.Fatal("existing translation missing from the request")
	}
	if strings.Contains(UserPrompt([]Item{{ID: 8, Source: "Open"}}), "current") {
		t.Fatal("untranslated items must not carry current")
	}
	if !strings.Contains(SystemPrompt("English", "Indonesian", "", ""), `When "current" is given`) {
		t.Fatal("prompt must explain current")
	}
	// the draft counts toward the batch size
	big := Item{Source: "a", Current: []string{strings.Repeat("x", 90)}}
	if len(Batch([]Item{big, big}, 25, 100)) != 2 {
		t.Fatal("current must count toward the batch size")
	}
}

func TestDraftPromptRules(t *testing.T) {
	p := SystemPrompt("English", "Indonesian", "", "")
	for _, want := range []string{"return it unchanged", "Never swap a correct word for a synonym", "glossary terms as suggestions"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	// a kept draft without the (optional) glossary term is not flagged
	it := Item{Source: "Learn about ways of viewing your photos in a grid", Current: []string{"Pelajari cara melihat foto dalam kotak"},
		Glossary: []TermHint{{Term: "Grid", Translation: "kisi", Optional: true}, {Term: "Learn", Translation: "Belajar", Optional: true}}}
	if p := Verify(it, it.Current, 2); len(p) != 0 {
		t.Fatalf("draft kept as-is must pass, got %v", p)
	}
}

func TestClientTemperatureFallback(t *testing.T) {
	var temps []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if v, ok := req["temperature"]; ok {
			temps = append(temps, "set")
			if v.(float64) != chatTemperature {
				t.Errorf("temperature %v", v)
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"Unsupported value: 'temperature' does not support 0.2 with this model."}}`))
			return
		}
		temps = append(temps, "default")
		w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	if _, _, err := NewClient(srv.URL, "k", "m", true).Chat(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(temps, ",") != "set,default" {
		t.Fatalf("calls: %v", temps)
	}
}

func TestMarkupIsSentAsIs(t *testing.T) {
	p := UserPrompt([]Item{{ID: 1, Source: "pilih <guiseq><gui>View</gui></guiseq> & tekan <key>F5</key>"}})
	if !strings.Contains(p, "<guiseq><gui>View</gui></guiseq> & tekan <key>F5</key>") || strings.Contains(p, `\u003c`) {
		t.Fatalf("markup must not be escaped: %s", p)
	}
	it := Item{Source: "press <key>F5</key>"}
	if p := Verify(it, []string{"tekan \x01key\x02F5\x01/key\x02"}, 2); len(p) != 1 || p[0] != ProblemCorrupt {
		t.Fatalf("control characters must be rejected, got %v", p)
	}
	if p := Verify(it, []string{"tekan F5"}, 2); len(p) == 0 {
		t.Fatal("dropped markup must be flagged")
	}
	if p := Verify(it, []string{"tekan <key>F5</key>"}, 2); len(p) != 0 {
		t.Fatalf("kept markup must pass, got %v", p)
	}
}

func TestMatchCase(t *testing.T) {
	src := "The Adjust button adjusts exposure, saturation and the mid-range contrast. Use Contrast in menus. Open the GPU panel."
	hints := MatchCase([]TermHint{
		{Term: "Exposure", Translation: "Eksposur"},
		{Term: "Contrast", Translation: "Kontras"}, // used both lower and capitalized: keep
		{Term: "saturation", Translation: "Saturasi"},
		{Term: "GPU", Translation: "GPU"},
		{Term: "Adjust", Translation: "Sesuaikan"}, // capitalized in the source: keep
	}, src)
	want := []string{"eksposur", "Kontras", "saturasi", "GPU", "Sesuaikan"}
	for i, h := range hints {
		if h.Translation != want[i] {
			t.Errorf("%s: got %q, want %q", h.Term, h.Translation, want[i])
		}
	}
	if !strings.Contains(SystemPrompt("English", "Indonesian", "", ""), "fixes the word, not its capitalization") {
		t.Fatal("prompt must explain glossary capitalization")
	}
	if lowerFirst("HTML viewer") != "HTML viewer" || lowerFirst("Kunci") != "kunci" {
		t.Fatal("lowerFirst")
	}
}
