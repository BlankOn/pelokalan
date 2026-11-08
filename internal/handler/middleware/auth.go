package middleware

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

type contextKey string

const (
	userKey contextKey = "user"
	langKey contextKey = "lang"
)

// TokenCookie is the name of the session cookie
const TokenCookie = "token"

// LangCookie is the name of the UI language cookie
const LangCookie = "lang"

// Auth middleware for JWT cookie authentication
type Auth struct {
	authService *service.AuthService
}

// NewAuth creates a new Auth middleware
func NewAuth(authService *service.AuthService) *Auth {
	return &Auth{authService: authService}
}

func (m *Auth) load(r *http.Request) *http.Request {
	cookie, err := r.Cookie(TokenCookie)
	if err != nil || cookie.Value == "" {
		return r
	}
	user, err := m.authService.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return r
	}
	ctx := context.WithValue(r.Context(), userKey, user)
	// a signed-in user's saved language wins over the cookie
	if i18n.IsSupported(user.Lang) {
		ctx = context.WithValue(ctx, langKey, user.Lang)
	}
	return r.WithContext(ctx)
}

// Optional extracts the user if signed in but does not require it
func (m *Auth) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, m.load(r))
	})
}

// Required redirects to the sign-in page when the user is not signed in
func (m *Auth) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = m.load(r)
		if GetUser(r.Context()) == nil {
			http.SetCookie(w, &http.Cookie{Name: TokenCookie, Value: "", Path: "/", MaxAge: -1})
			target := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", target)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUser returns the signed-in user or nil
func GetUser(ctx context.Context) *models.User {
	u, _ := ctx.Value(userKey).(*models.User)
	return u
}

// Lang resolves the UI language from the "lang" cookie and Accept-Language
// header, defaulting to Indonesian.
func Lang(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := ""
		if c, err := r.Cookie(LangCookie); err == nil && i18n.IsSupported(c.Value) {
			lang = c.Value
		}
		if lang == "" {
			lang = i18n.Default
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), langKey, lang)))
	})
}

// GetLang returns the UI language of the request
func GetLang(ctx context.Context) string {
	if l, ok := ctx.Value(langKey).(string); ok {
		return l
	}
	return i18n.Default
}

// SameOrigin rejects state-changing requests coming from other sites. Together
// with SameSite=Lax cookies this protects form posts against CSRF.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets conservative response headers
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
