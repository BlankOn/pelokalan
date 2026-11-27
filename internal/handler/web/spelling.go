package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/service"
)

// Indonesian spelling check in the editor (project members only: the word list has no licence and
// is never published). The translation box asks which of its words are unknown; a click on one
// asks for suggestions; "Tambahkan ke kamus proyek" accepts a word for the whole project.

// SetSpelling enables the spelling check
func (h *Handler) SetSpelling(s *service.SpellingService) { h.spelling = s }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// SpellCheck returns the unknown words of a translation: POST /catalogs/{cid}/spell
// {"text": …, "source": …} → {"unknown": […]} (empty when the file is not Indonesian)
func (h *Handler) SpellCheck(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	var in struct{ Text, Source string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	words := []string{}
	if h.spelling != nil {
		got, err := h.spelling.Check(r.Context(), p, c.Language, in.Text, in.Source)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		words = append(words, got...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"unknown": words, "active": h.spelling.Applies(c.Language)})
}

// SpellSuggest returns close dictionary words: GET /projects/{id}/spell/suggest?w=
func (h *Handler) SpellSuggest(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	word := strings.TrimSpace(r.URL.Query().Get("w"))
	out := []string{}
	if h.spelling != nil && word != "" && utf8.RuneCountInString(word) <= 40 {
		if got, err := h.spelling.Suggest(p, word); err == nil {
			out = append(out, got...)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

// AddDictionaryWord accepts a word in the project: POST /projects/{id}/dictionary {"word": …}
func (h *Handler) AddDictionaryWord(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	var in struct{ Word string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || h.spelling == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	word, err := h.spelling.AddWord(r.Context(), middleware.GetUser(r.Context()), p, in.Word)
	if err != nil {
		msg := h.errorMessage(r, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"word": word,
		"message": i18n.T(middleware.GetLang(r.Context()), "spelling.added", word)})
}

// DeleteDictionaryWord removes a project word (Settings, owners and managers)
func (h *Handler) DeleteDictionaryWord(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if h.spelling != nil {
		if err := h.spelling.DeleteWord(r.Context(), p, r.FormValue("word")); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	q := url.Values{"flash": {"word_deleted"}}
	http.Redirect(w, r, p.Path()+"/settings?"+q.Encode()+"#dictionary", http.StatusSeeOther)
}
