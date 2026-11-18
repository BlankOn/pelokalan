package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Public JSON API: no key, no sign-in, readable from any site (CORS). The members list includes
// each member's sign-in email address (decided by the operator), which the public pages do not show.

type apiMember struct {
	Username string    `json:"username"`
	Name     string    `json:"name"`
	Email    string    `json:"email"` // sign-in address
	Role     string    `json:"role"`  // owner, manager or translator
	JoinedAt time.Time `json:"joined_at"`
}

type apiMembers struct {
	Project struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"project"`
	Total   int         `json:"total"`
	Members []apiMember `json:"members"`
}

func apiJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if status == http.StatusOK {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// apiProject finds a project by slug (or numeric id) for the API, as a visitor
func (h *Handler) apiProject(r *http.Request) (*models.Project, error) {
	param := strings.ToLower(chi.URLParam(r, "id"))
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		return h.projectService.GetPublic(r.Context(), nil, id)
	}
	return h.projectService.GetPublicBySlug(r.Context(), nil, param)
}

// APIMembers lists a project's members: GET /api/projects/{id}/members
func (h *Handler) APIMembers(w http.ResponseWriter, r *http.Request) {
	p, err := h.apiProject(r)
	if errors.Is(err, service.ErrNotFound) {
		apiJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	} else if err != nil {
		apiJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	members, err := h.projectService.Members(r.Context(), p)
	if err != nil {
		apiJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	base := strings.TrimRight(h.siteConfig.BaseURL, "/")
	var out apiMembers
	out.Project.Slug, out.Project.Name, out.Project.URL = p.Slug, p.Name, base+p.Path()
	out.Members = make([]apiMember, 0, len(members))
	for _, m := range members {
		out.Members = append(out.Members, apiMember{Username: m.User.Username, Name: m.User.Name, Email: m.User.Email, Role: string(m.Role),
			JoinedAt: m.CreatedAt.UTC()})
	}
	out.Total = len(out.Members)
	apiJSON(w, http.StatusOK, out)
}
