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
	{"golang.org/x/sync, golang.org/x/text, golang.org/x/image", "https://cs.opensource.google/go/x", "BSD-3-Clause", "licenses.use_x"},
	{"gg", "https://github.com/fogleman/gg", "MIT", "licenses.use_gg"},
	{"freetype (Go)", "https://github.com/golang/freetype", "FreeType License", "licenses.use_freetype"},
	{"Go fonts", "https://go.dev/blog/go-fonts", "BSD-3-Clause", "licenses.use_gofont"},
	{"htmx 1.9.10", "https://htmx.org", "0BSD", "licenses.use_htmx"},
	{"Feather Icons", "https://feathericons.com", "MIT", "licenses.use_feather"},
	{"Simple Icons", "https://simpleicons.org", "CC0-1.0", "licenses.use_simpleicons"},
	{"Anthropic Go SDK", "https://github.com/anthropics/anthropic-sdk-go", "MIT", "licenses.use_anthropic"},
	{"gjson, sjson, match, pretty (tidwall), jsonparser, jsonschema, generic-list-go, ordered-map, standard-webhooks, OpenTelemetry Go, logr, yaml",
		"https://github.com/anthropics/anthropic-sdk-go/blob/main/go.mod", "MIT, Apache-2.0, BSD-3-Clause", "licenses.use_anthropic_deps"},
	{"PostgreSQL", "https://www.postgresql.org", "PostgreSQL License", "licenses.use_postgres"},
}

// thesaurusCredit is the data source of the editor's thesaurus
var thesaurusCredit = credit{"tesaurus (victoriasovereigne)",
	"https://raw.githubusercontent.com/victoriasovereigne/tesaurus/refs/heads/master/dict.json", "", "licenses.use_thesaurus"}

// spellingCredit is the data source of the Indonesian spelling check (no licence stated)
var spellingCredit = credit{"kbbi-v6-full-csv (aryakdaniswara)",
	"https://github.com/aryakdaniswara/kbbi-v6-full-csv", "", "licenses.use_spelling"}

// AboutPage explains what Pelokalan is for and who funds it (public)
func (h *Handler) AboutPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "about", h.page(r, "title.about"))
}

// LicensesPage lists the software and data Pelokalan is built with (public)
func (h *Handler) LicensesPage(w http.ResponseWriter, r *http.Request) {
	data := h.page(r, "title.licenses")
	data.Data["Credits"] = licenseCredits
	data.Data["Thesaurus"] = thesaurusCredit
	data.Data["Spelling"] = spellingCredit
	h.render(w, "licenses", data)
}
