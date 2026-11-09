package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/config"
	"pelokalan/internal/glossary"
	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/service"
)

// Handler holds web handler dependencies
type Handler struct {
	templates      map[string]*template.Template
	partials       *template.Template
	templatesDir   string
	siteConfig     config.SiteConfig
	serverConfig   config.ServerConfig
	authService    *service.AuthService
	projectService *service.ProjectService
	catalogService *service.CatalogService
	glossary       *service.GlossaryService
	ai             *service.AIService
	discuss        *service.DiscussionService
	wiki           *service.WikiService
	contexts       *service.ContextService
	poFetcher      *glossary.Fetcher // downloads PO files from a URL (SSRF-safe)
	syncEvery      time.Duration     // automatic glossary/project file sync interval
	thesaurus      *service.ThesaurusService
	assetVersion   string
}

// NewHandler creates a new web handler
func NewHandler(
	templatesDir string,
	siteConfig config.SiteConfig,
	serverConfig config.ServerConfig,
	authService *service.AuthService,
	projectService *service.ProjectService,
	catalogService *service.CatalogService,
	glossaryService *service.GlossaryService,
	aiService *service.AIService,
	discussionService *service.DiscussionService,
	wikiService *service.WikiService,
	contextService *service.ContextService,
) (*Handler, error) {
	h := &Handler{
		templatesDir:   templatesDir,
		siteConfig:     siteConfig,
		serverConfig:   serverConfig,
		authService:    authService,
		projectService: projectService,
		catalogService: catalogService,
		glossary:       glossaryService,
		ai:             aiService,
		discuss:        discussionService,
		wiki:           wikiService,
		contexts:       contextService,
	}
	h.assetVersion = assetVersion("static")
	if err := h.loadTemplates(); err != nil {
		return nil, err
	}
	return h, nil
}

// SetThesaurus enables synonym suggestions in the editor
func (h *Handler) SetThesaurus(t *service.ThesaurusService) { h.thesaurus = t }

// SetPOFetcher enables creating PO files from a URL
func (h *Handler) SetPOFetcher(f *glossary.Fetcher) { h.poFetcher = f }

// SetSyncInterval sets how often sync links are downloaded automatically (shown to users)
func (h *Handler) SetSyncInterval(d time.Duration) { h.syncEvery = d }

// PageData is passed to every template
type PageData struct {
	Title    string
	Lang     string
	User     *models.User
	SiteName string
	Path     string
	Asset    string // cache-busting version of static files
	Error    string
	Flash    string
	Data     map[string]any
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"t": func(lang, key string, args ...any) string { return i18n.T(lang, key, args...) },
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, errors.New("dict needs key/value pairs")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
		"add":      func(a, b int) int { return a + b },
		"timeAgo":  timeAgo,
		"date":     func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"langName": po.LanguageName,
		"roleName": func(lang string, role models.ProjectRole) string { return i18n.T(lang, "role."+string(role)) },
		"issueName": func(lang, code string) string {
			return i18n.T(lang, "issue."+code)
		},
		"pluralLabel": func(lang string, idx, n int) string {
			if n == 2 {
				if idx == 0 {
					return i18n.T(lang, "editor.plural_singular")
				}
				return i18n.T(lang, "editor.plural_plural")
			}
			return i18n.T(lang, "editor.plural_form", idx+1)
		},
		"languages":   po.Languages,
		"uiLanguages": func() any { return i18n.Languages },
		"isPlural":    func(e *models.Entry) bool { return e.IsPlural() },
		"errText": func(lang, s string) string {
			if i18n.Has(s) {
				return i18n.T(lang, s)
			}
			return s
		},
		"usd": func(v float64) string {
			switch {
			case v == 0:
				return "$0"
			case v < 0.01:
				return fmt.Sprintf("$%.4f", v)
			default:
				return fmt.Sprintf("$%.2f", v)
			}
		},
		"num": func(v int64) string { return groupDigits(v) },
		"commitMsg": func(lang, s string) string {
			parts := strings.Split(s, "|")
			if len(parts) > 0 && i18n.Has(parts[0]) {
				args := make([]any, 0, len(parts)-1)
				for _, a := range parts[1:] {
					args = append(args, a)
				}
				return i18n.T(lang, parts[0], args...)
			}
			return s
		},
		"hasPrefix":   strings.HasPrefix,
		"commentHTML": commentHTML,
		"wikiHTML":    wikiHTML,
		"linkify":     linkify,
		"pathEscape":  url.PathEscape,
		"fileSize": func(n int64) string {
			switch {
			case n >= 1<<20:
				return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
			case n >= 1<<10:
				return fmt.Sprintf("%d KB", n>>10)
			default:
				return fmt.Sprintf("%d B", n)
			}
		},
		"initial": func(s string) string {
			for _, r := range s {
				return strings.ToUpper(string(r))
			}
			return "?"
		},
		"toJSON": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"has": func(list []string, v string) bool {
			for _, x := range list {
				if x == v {
					return true
				}
			}
			return false
		},
		"derefTime": func(t *time.Time) time.Time {
			if t == nil {
				return time.Time{}
			}
			return *t
		},
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		"jobError": func(lang, s string) string {
			parts := strings.Split(s, "|")
			if len(parts) > 0 && i18n.Has(parts[0]) {
				args := make([]any, 0, len(parts)-1)
				for _, a := range parts[1:] {
					args = append(args, a)
				}
				return i18n.T(lang, parts[0], args...)
			}
			return s
		},
		"join": strings.Join,
	}
}

func (h *Handler) loadTemplates() error {
	partialFiles, err := filepath.Glob(filepath.Join(h.templatesDir, "partials", "*.html"))
	if err != nil {
		return err
	}
	shared := append([]string{filepath.Join(h.templatesDir, "layouts", "base.html")}, partialFiles...)

	h.partials, err = template.New("partials").Funcs(funcMap()).ParseFiles(shared...)
	if err != nil {
		return fmt.Errorf("failed to parse partials: %w", err)
	}

	pageFiles, err := filepath.Glob(filepath.Join(h.templatesDir, "pages", "*.html"))
	if err != nil {
		return err
	}
	h.templates = make(map[string]*template.Template, len(pageFiles))
	for _, pageFile := range pageFiles {
		name := strings.TrimSuffix(filepath.Base(pageFile), ".html")
		tmpl, err := template.New(name).Funcs(funcMap()).ParseFiles(append([]string{pageFile}, shared...)...)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}
		h.templates[name] = tmpl
	}
	return nil
}

// page creates the common page data for a request
func (h *Handler) page(r *http.Request, titleKey string) PageData {
	lang := middleware.GetLang(r.Context())
	title := h.siteConfig.Name
	if titleKey != "" {
		title = i18n.T(lang, titleKey) + " · " + h.siteConfig.Name
	}
	pd := PageData{
		Title:    title,
		Lang:     lang,
		User:     middleware.GetUser(r.Context()),
		SiteName: h.siteConfig.Name,
		Path:     r.URL.RequestURI(),
		Asset:    h.assetVersion,
		Data:     map[string]any{"Year": time.Now().Year()},
	}
	if h.authService != nil {
		pd.Data["WhitelistOn"] = h.authService.WhitelistEnabled()
	}
	if f := r.URL.Query().Get("flash"); f != "" && i18n.Has("flash."+f) {
		pd.Flash = i18n.T(lang, "flash."+f)
	}
	return pd
}

// render renders a full page
func (h *Handler) render(w http.ResponseWriter, name string, data PageData) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// renderPartials renders one or more partial templates (for HTMX)
func (h *Handler) renderPartials(w http.ResponseWriter, parts ...partial) {
	var buf bytes.Buffer
	for _, p := range parts {
		if err := h.partials.ExecuteTemplate(&buf, p.name, p.data); err != nil {
			log.Printf("render partial %s: %v", p.name, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

type partial struct {
	name string
	data any
}

// errorMessage translates an error for display, logging unexpected ones
func (h *Handler) errorMessage(r *http.Request, err error) string {
	lang := middleware.GetLang(r.Context())
	if ue, ok := service.AsUserError(err); ok {
		return i18n.T(lang, ue.Key, ue.Args...)
	}
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	return i18n.T(lang, "err.internal")
}

// fail renders an error page for a service error
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		status = http.StatusForbidden
	default:
		if _, ok := service.AsUserError(err); ok {
			status = http.StatusBadRequest
		}
	}
	msg := h.errorMessage(r, err)
	if isHTMX(r) {
		w.Header().Set("HX-Retarget", "#toast")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(status)
		h.renderPartials(w, partial{"toast", map[string]any{"Message": msg, "Error": true}})
		return
	}
	w.WriteHeader(status)
	data := h.page(r, "title.error")
	data.Error = msg
	h.render(w, "error", data)
}

// redirect performs a redirect that also works for HTMX requests
func redirect(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func idParam(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id
}

// timeAgo returns a human-readable relative time
func timeAgo(lang string, t time.Time) string {
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return i18n.T(lang, "time.just_now")
	case diff < time.Hour:
		return i18n.T(lang, "time.minutes_ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return i18n.T(lang, "time.hours_ago", int(diff.Hours()))
	case diff < 30*24*time.Hour:
		return i18n.T(lang, "time.days_ago", int(diff.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// safeNext returns a local redirect target or the fallback
func safeNext(next, fallback string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return fallback
	}
	return next
}

// groupDigits formats 1234567 as "1.234.567"
func groupDigits(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// assetVersion hashes the static files so their URLs change on every deploy
func assetVersion(dir string) string {
	hash := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil {
			hash.Write([]byte(path))
			hash.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil))[:10]
}
