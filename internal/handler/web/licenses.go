package web

import "net/http"

// credit is one piece of software or data Pelokalan uses
type credit struct {
	Name    string
	URL     string
	License string
	Note    string // i18n key describing what it is used for
}

// licenseCredits lists third-party software and data. Keep it in sync with go.mod and static/.
var licenseCredits = []credit{
	{"Go", "https://go.dev", "BSD-3-Clause", "licenses.use_go"},
	{"chi", "https://github.com/go-chi/chi", "MIT", "licenses.use_chi"},
	{"pgx", "https://github.com/jackc/pgx", "MIT", "licenses.use_pgx"},
	{"pgerrcode, pgpassfile, pgservicefile, puddle", "https://github.com/jackc", "MIT", "licenses.use_pgx_deps"},
	{"golang-migrate", "https://github.com/golang-migrate/migrate", "MIT", "licenses.use_migrate"},
	{"golang-jwt", "https://github.com/golang-jwt/jwt", "MIT", "licenses.use_jwt"},
	{"goldmark", "https://github.com/yuin/goldmark", "MIT", "licenses.use_goldmark"},
	{"godotenv", "https://github.com/joho/godotenv", "MIT", "licenses.use_godotenv"},
	{"golang.org/x/sync, golang.org/x/text", "https://cs.opensource.google/go/x", "BSD-3-Clause", "licenses.use_x"},
	{"htmx 1.9.10", "https://htmx.org", "0BSD", "licenses.use_htmx"},
	{"Feather Icons", "https://feathericons.com", "MIT", "licenses.use_feather"},
	{"PostgreSQL", "https://www.postgresql.org", "PostgreSQL License", "licenses.use_postgres"},
}

// thesaurusCredit is the data source of the editor's thesaurus
var thesaurusCredit = credit{"tesaurus (victoriasovereigne)",
	"https://raw.githubusercontent.com/victoriasovereigne/tesaurus/refs/heads/master/dict.json", "", "licenses.use_thesaurus"}

// AboutPage explains what Pelokalan is for and who funds it (public)
func (h *Handler) AboutPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "about", h.page(r, "title.about"))
}

// LicensesPage lists the software and data Pelokalan is built with (public)
func (h *Handler) LicensesPage(w http.ResponseWriter, r *http.Request) {
	data := h.page(r, "title.licenses")
	data.Data["Credits"] = licenseCredits
	data.Data["Thesaurus"] = thesaurusCredit
	h.render(w, "licenses", data)
}
