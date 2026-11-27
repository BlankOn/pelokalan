package web

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/service"
	"pelokalan/internal/web/middleware"
)

// UploadAvatar stores a new profile picture: POST /profile/avatar (multipart "avatar") → {"url": …}.
// The browser crops it first; the server cuts a centred square anyway, scales and compresses it.
func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxAvatarUpload+1<<20)
	f, _, err := r.FormFile("avatar")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, service.ErrAvatarInvalid)})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, service.MaxAvatarUpload+1))
	if err == nil {
		err = h.authService.SetAvatar(r.Context(), user, data)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": user.AvatarURL()})
}

// DeleteAvatar removes the profile picture: POST /profile/avatar/delete → {"ok": true}
func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.authService.RemoveAvatar(r.Context(), middleware.GetUser(r.Context())); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Avatar serves a profile picture: GET /avatars/{key}.jpg (public; the key is the content hash, so it never changes)
func (h *Handler) Avatar(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSuffix(chi.URLParam(r, "file"), ".jpg")
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("If-None-Match") == `"`+key+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := h.authService.AvatarData(r.Context(), key)
	if err != nil {
		if !errors.Is(err, apperr.ErrNotFound) {
			h.fail(w, r, err)
			return
		}
		http.NotFound(w, r)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "image/jpeg")
	hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	hdr.Set("ETag", `"`+key+`"`)
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(data)
}
