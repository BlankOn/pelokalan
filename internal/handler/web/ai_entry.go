package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
)

// AITranslateEntry translates one entry with the model and returns the text (JSON); the
// editor puts it in the translation box for the owner to review and save
func (h *Handler) AITranslateEntry(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	e, err := h.catalogService.Entry(r.Context(), c, idParam(r, "eid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// the text currently in the editor is the draft to improve
	_ = r.ParseForm()
	var current []string
	for i := 0; i < 8; i++ {
		v, ok := r.PostForm[fmt.Sprintf("msgstr%d", i)]
		if !ok {
			break
		}
		current = append(current, v[0])
	}
	w.Header().Set("Content-Type", "application/json")
	n, _ := strconv.Atoi(r.PostForm.Get("n"))
	res, err := h.ai.TranslateEntry(r.Context(), middleware.GetUser(r.Context()), p, c, e, current, max(n, 1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	lang := middleware.GetLang(r.Context())
	notice := i18n.T(lang, "ai.entry_done")
	if res.Flagged {
		notice = i18n.T(lang, "ai.entry_flagged")
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"forms": res.Forms, "flagged": res.Flagged, "notice": notice,
		"alternatives": res.Alternatives})
}
