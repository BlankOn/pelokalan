package web

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"pelokalan/internal/models"
)

// The editor renders the Konteks history also for modules without any Konteks yet
// (no edit list at all); that must not fail the page.
func TestContextHistoryRendersWithoutEdits(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(funcMap()).ParseFiles("../../templates/partials/context_history.html"))
	cases := map[string]any{
		"missing": nil,
		"empty":   []models.ContextEdit{},
		"one":     []models.ContextEdit{{Username: "budi", At: time.Now()}},
	}
	for name, edits := range cases {
		data := map[string]any{"Lang": "id", "Data": map[string]any{}}
		if edits != nil {
			data["Data"].(map[string]any)["ContextEdits"] = edits
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "context_history", data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "one" && !strings.Contains(buf.String(), "@budi") {
			t.Errorf("%s: history line missing: %s", name, buf.String())
		}
	}
}
