package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeClaude is a stand-in for the Anthropic API: the Models endpoint for "Tes koneksi" and the Messages endpoint,
// which answers glossary extraction requests (one pair per item)
type fakeClaude struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
}

func newFakeClaude(t *testing.T, key string) *fakeClaude {
	f := &fakeClaude{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the SDK sends the key as "Authorization: Bearer"; x-api-key is accepted too
		if r.Header.Get("X-Api-Key") != key && r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/models/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "model", "id": id, "display_name": id, "created_at": "2026-11-01T00:00:00Z"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.requests = append(f.requests, body)
			f.headers = append(f.headers, r.Header.Clone())
			f.mu.Unlock()
			// the user message holds {"items":[{"id":..,"source":..,"translation":..}]}
			msgs, _ := body["messages"].([]any)
			text := ""
			if len(msgs) > 0 {
				if content, ok := msgs[0].(map[string]any)["content"].([]any); ok && len(content) > 0 {
					text, _ = content[0].(map[string]any)["text"].(string)
				}
			}
			var in struct {
				Items []struct {
					ID int64 `json:"id"`
				} `json:"items"`
			}
			_ = json.Unmarshal([]byte(text), &in)
			var items []string
			for _, it := range in.Items {
				items = append(items, `{"id":`+strconv.FormatInt(it.ID, 10)+`,"terms":[{"s":"window","t":"jendela"}]}`)
			}
			answer := "Here is the JSON:\n{\"items\":[" + strings.Join(items, ",") + "]}"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "model": body["model"],
				"content":     []any{map[string]any{"type": "text", "text": answer}},
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 1000, "output_tokens": 100},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return f
}

// Claude as a project's AI provider: settings, the connection test and a real job (glossary extraction)
func TestClaudeProvider(t *testing.T) {
	skipWithoutDB(t)
	key := "sk-ant-test-0123456789"
	fake := newFakeClaude(t, key)
	defer fake.Close()

	owner := signUp(t, "claude")
	slug := owner.newProject("Claude")
	base := "/projects/" + slug
	pid := projectID(t, slug)

	// a Claude model needs the Anthropic provider
	owner.post(base+"/ai", url.Values{"provider": {"openai"}, "base_url": {fake.URL}, "model": {"claude-opus-5-5"},
		"api_key": {key}}).status(http.StatusOK).contains("milik penyedia lain")
	owner.post(base+"/ai", url.Values{"provider": {"anthropic"}, "base_url": {fake.URL}, "model": {"claude-opus-5-5"},
		"api_key": {key}}).redirectsTo(base + "/settings")
	if got := scalar[string](t, `SELECT provider || ' ' || price_input::text || ' ' || price_output::text FROM project_ai_settings WHERE project_id = $1`, pid); got != "anthropic 4.0000 20.0000" {
		t.Fatalf("stored settings = %q", got)
	}
	owner.get(base+"/settings").status(http.StatusOK).contains("Anthropic Claude", `value="anthropic" data-base="https://api.anthropic.com" data-model="claude-opus-5-5" selected`)
	owner.post(base+"/ai/test", nil).redirectsTo(base + "/settings?flash=ai_ok")

	// a run goes through the Messages API: low effort, the refusal fallback, no temperature; the answer (with words
	// around the JSON) becomes glossary rows and its cost is counted at Claude's prices
	long := "Close the window before you move it to another screen."
	owner.upload(base+"/module-groups/import", url.Values{"name": {"docs"}, "version_name": {"1"}, "language": {"id"}},
		"docs.tar.gz", targz(t, map[string][]byte{"ui.po": poFile("ui", long, "Tutup jendela sebelum Anda memindahkannya ke layar lain.")})).
		redirectsTo(base + "/settings?")
	gid := scalar[int64](t, `SELECT id FROM module_groups WHERE project_id = $1`, pid)
	owner.post(base+"/glossary-extract", url.Values{"group": {strconv.FormatInt(gid, 10)}}).status(http.StatusOK).contains("dimulai")
	waitExtract(t, pid, 1)
	fake.mu.Lock()
	if len(fake.requests) != 1 {
		fake.mu.Unlock()
		t.Fatalf("messages requests = %d", len(fake.requests))
	}
	req, hdr := fake.requests[0], fake.headers[0]
	fake.mu.Unlock()
	if req["model"] != "claude-opus-5-5" || req["temperature"] != nil || req["fallbacks"] != "default" {
		t.Fatalf("request = %v", req)
	}
	if oc, _ := req["output_config"].(map[string]any); oc == nil || oc["effort"] != "low" {
		t.Fatalf("output_config = %v", req["output_config"])
	}
	if !strings.Contains(hdr.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Fatalf("beta header = %q", hdr.Get("Anthropic-Beta"))
	}
	if n := scalar[int](t, `SELECT COUNT(*) FROM glossary_terms WHERE project_id = $1 AND origin = 'ai' AND source_norm = 'window'`, pid); n != 1 {
		t.Fatalf("AI rows for window = %d", n)
	}
	// 1000 input tokens at $4/M + 100 output at $20/M
	if cost := scalar[float64](t, `SELECT cost::float8 FROM term_extraction_jobs WHERE project_id = $1`, pid); cost < 0.00599 || cost > 0.00601 {
		t.Fatalf("cost = %v", cost)
	}

	// switching provider needs that provider's key; a wrong key is explained
	owner.post(base+"/ai", url.Values{"provider": {"openai"}, "model": {"gpt-4o-mini"}}).status(http.StatusOK)
	if scalar[string](t, `SELECT provider FROM project_ai_settings WHERE project_id = $1`, pid) != "anthropic" {
		t.Fatal("switched provider without a key")
	}
	owner.post(base+"/ai", url.Values{"provider": {"anthropic"}, "base_url": {fake.URL}, "model": {"claude-haiku-5-5"},
		"api_key": {"sk-ant-wrong-0123456789"}}).redirectsTo(base + "/settings")
	owner.post(base+"/ai/test", nil).status(http.StatusOK).lacks("flash=ai_ok")
}
