package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"pelokalan/internal/thesaurus"
)

// ThesaurusLookup returns synonyms and antonyms of a word as JSON (project members only)
func (h *Handler) ThesaurusLookup(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=600")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res := map[string]any{"found": false}
	if h.thesaurus != nil && q != "" && utf8.RuneCountInString(q) <= 60 {
		if e, err := h.thesaurus.Lookup(p, q); err == nil && e != nil {
			res = map[string]any{"found": true, "word": e.Word, "tag": e.Tag, "synonyms": e.Synonyms, "antonyms": e.Antonyms,
				// the entry is another form of the selected word (e.g. "menampilkan" for "Tampilkan")
				"asForm": thesaurus.Normalize(q) != e.Word}
		}
	}
	_ = json.NewEncoder(w).Encode(res)
}
