package app_test

// Integration test harness: the real router and services on a throwaway PostgreSQL database
// (internal/testdb), driven over HTTP like a browser. Emails are captured instead of sent and
// the GNOME translation site is never asked (Options.Offline). Skipped without TEST_DATABASE_URL.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pelokalan/internal/app"
	"pelokalan/internal/config"
	"pelokalan/internal/githubapp/githubtest"
	"pelokalan/internal/service"
	"pelokalan/internal/testdb"
)

var (
	ghFake *githubtest.Server // the fake GitHub API the GitHub App talks to (review_test.go)
	env    *app.App
	srv    *httptest.Server
	mails  = &captureMailer{}
	serial atomic.Int64
)

func TestMain(m *testing.M) {
	flag.Parse()
	if !testdb.Available() {
		fmt.Println("integration tests skipped: set " + testdb.EnvURL + " (make test-integration)")
		os.Exit(m.Run())
	}
	os.Exit(run(m))
}

func run(m *testing.M) int {
	db, err := testdb.Create()
	if err != nil {
		log.Printf("testdb: %v", err)
		return 1
	}
	defer func() {
		if err := db.Drop(); err != nil {
			log.Printf("testdb drop: %v", err)
		}
	}()
	root, err := testdb.Root()
	if err != nil {
		log.Print(err)
		return 1
	}
	if err := app.Migrate(db.Config, root); err != nil {
		log.Print(err)
		return 1
	}
	srv = httptest.NewUnstartedServer(nil)
	srv.Start()
	defer srv.Close()
	ghFake = githubtest.New()
	defer ghFake.Close()
	cfg := testConfig(db.Config, srv.URL)
	cfg.GitHub = config.GitHubConfig{AppID: ghFake.AppID, Slug: "pelokalan-test", PrivateKey: ghFake.PrivateKey, API: ghFake.URL}
	env, err = app.New(cfg, db.SQL, app.Options{Root: root, Mailer: mails, Offline: true})
	if err != nil {
		log.Print(err)
		return 1
	}
	srv.Config.Handler = env.Router
	if !testing.Verbose() {
		log.SetOutput(io.Discard) // request logs
	}
	return m.Run()
}

// testConfig is a fixed configuration: open registration, no external services
func testConfig(db config.DatabaseConfig, base string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", MaxUploadMB: 20,
			ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second},
		Database: db,
		JWT:      config.JWTConfig{Secret: "integration-test-secret", Expiration: 24 * time.Hour},
		Site:     config.SiteConfig{BaseURL: base, Name: "Pelokalan", DefaultLang: "en", TimeZone: "Asia/Jakarta"},
		Mail:     config.MailConfig{FromEmail: "noreply@localhost", FromName: "Pelokalan"},
		Auth: config.AuthConfig{
			Whitelist:            config.ParseWhitelist(""),
			OTPExpiration:        10 * time.Minute,
			OTPMaxAttempts:       5,
			OTPResendInterval:    0,
			InvitationExpiration: 14 * 24 * time.Hour,
		},
		AI: config.AIConfig{SecretKey: "integration-test-secret"},
		// private hosts: the Translation Project test site runs on this machine (tp_test.go)
		Glossary: config.GlossaryConfig{FetchInterval: time.Hour, MaxDownloadMB: 20, AllowPrivateHosts: true},
	}
}

// captureMailer keeps every email so tests can read codes and check notifications
type captureMailer struct {
	mu   sync.Mutex
	sent []*service.Message
}

func (m *captureMailer) Send(_ context.Context, msg *service.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// to returns the emails sent to an address, oldest first
func (m *captureMailer) to(addr string) []*service.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*service.Message
	for _, msg := range m.sent {
		if strings.EqualFold(msg.To, addr) {
			out = append(out, msg)
		}
	}
	return out
}

// waitFor waits until an email to addr whose subject or text contains want arrives
// (notifications are sent in the background)
func (m *captureMailer) waitFor(t *testing.T, addr, want string) *service.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range m.to(addr) {
			if strings.Contains(msg.Subject, want) || strings.Contains(msg.Text, want) {
				return msg
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no email to %s containing %q (got %d emails)", addr, want, len(m.to(addr)))
	return nil
}

// user is a signed-in browser
type user struct {
	t        *testing.T
	c        *http.Client
	ID       int64
	Username string
	Email    string
}

// unique gives names that differ between tests sharing the database
func unique(prefix string) string {
	return fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano()%100000, serial.Add(1))
}

// visitor is a browser that is not signed in
func visitor(t *testing.T) *user {
	jar, _ := cookiejar.New(nil)
	return &user{t: t, c: &http.Client{
		Jar:           jar,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var codeRe = regexp.MustCompile(`\b(\d{3}\s?\d{3})\b`)

// signUp registers a new account through the forms, reading the code from the email
func signUp(t *testing.T, prefix string) *user {
	t.Helper()
	u := visitor(t)
	u.Username = unique(prefix)
	u.Email = u.Username + "@example.com"
	res := u.post("/register", url.Values{"email": {u.Email}, "username": {u.Username}, "name": {strings.ToUpper(prefix[:1]) + prefix[1:]}})
	res.status(http.StatusOK)
	msgs := mails.to(u.Email)
	if len(msgs) == 0 {
		t.Fatalf("no sign-up code emailed to %s", u.Email)
	}
	code := codeRe.FindStringSubmatch(msgs[len(msgs)-1].Text)
	if code == nil {
		t.Fatalf("no code in email: %q", msgs[len(msgs)-1].Text)
	}
	u.post("/verify", url.Values{"email": {u.Email}, "purpose": {"register"}, "code": {code[1]}}).
		redirectsTo("/projects")
	if err := env.DB.QueryRow(`SELECT id FROM users WHERE email = $1`, u.Email).Scan(&u.ID); err != nil {
		t.Fatalf("user row: %v", err)
	}
	return u
}

// response is a finished request
type response struct {
	t      *testing.T
	Status int
	Body   string
	Header http.Header
	req    string
}

func (u *user) do(req *http.Request) *response {
	u.t.Helper()
	req.Header.Set("Origin", srv.URL)
	res, err := u.c.Do(req)
	if err != nil {
		u.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return &response{t: u.t, Status: res.StatusCode, Body: string(body), Header: res.Header, req: req.Method + " " + req.URL.RequestURI()}
}

func (u *user) get(path string) *response {
	u.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	return u.do(req)
}

func (u *user) post(path string, form url.Values) *response {
	u.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return u.do(req)
}

// hx posts a form like htmx does (handlers answer with a fragment instead of a redirect)
func (u *user) hx(path string, form url.Values) *response {
	u.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return u.do(req)
}

// upload posts a multipart form with one file in the field "file"
func (u *user) upload(path string, fields url.Values, filename string, data []byte) *response {
	u.t.Helper()
	var b strings.Builder
	w := multipart.NewWriter(&b)
	for k, vs := range fields {
		for _, v := range vs {
			w.WriteField(k, v)
		}
	}
	fw, _ := w.CreateFormFile("file", filename)
	fw.Write(data)
	w.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(b.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	return u.do(req)
}

func (r *response) status(want int) *response {
	r.t.Helper()
	if r.Status != want {
		r.t.Fatalf("%s: status %d, want %d\n%s", r.req, r.Status, want, snippet(r.Body))
	}
	return r
}

// redirectsTo checks a See Other redirect whose target starts with prefix and returns the target
func (r *response) redirectsTo(prefix string) string {
	r.t.Helper()
	loc := r.Header.Get("Location")
	if r.Status != http.StatusSeeOther && r.Status != http.StatusFound && r.Status != http.StatusMovedPermanently {
		r.t.Fatalf("%s: status %d, want a redirect to %s\n%s", r.req, r.Status, prefix, snippet(r.Body))
	}
	if !strings.HasPrefix(loc, prefix) {
		r.t.Fatalf("%s: redirects to %q, want %q…", r.req, loc, prefix)
	}
	return loc
}

func (r *response) contains(parts ...string) *response {
	r.t.Helper()
	for _, p := range parts {
		if !strings.Contains(r.Body, p) {
			r.t.Fatalf("%s: body lacks %q\n%s", r.req, p, snippet(r.Body))
		}
	}
	return r
}

func (r *response) lacks(parts ...string) *response {
	r.t.Helper()
	for _, p := range parts {
		if strings.Contains(r.Body, p) {
			r.t.Fatalf("%s: body has %q", r.req, p)
		}
	}
	return r
}

func snippet(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

// scalar reads one value from the test database
func scalar[T any](t *testing.T, query string, args ...any) T {
	t.Helper()
	var v T
	if err := env.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// newProject creates a project through the form and returns its slug
func (u *user) newProject(name string) string {
	u.t.Helper()
	slug := unique("p")
	u.post("/projects", url.Values{"name": {name}, "slug": {slug}, "source_language": {"en"}}).
		redirectsTo("/projects/" + slug)
	return slug
}

// poFile is a small Indonesian PO file with a module name in its header
func poFile(module string, pairs ...string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "msgid \"\"\nmsgstr \"\"\n\"Project-Id-Version: %s main\\n\"\n\"Language: id\\n\"\n"+
		"\"MIME-Version: 1.0\\n\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n\"Content-Transfer-Encoding: 8bit\\n\"\n"+
		"\"Plural-Forms: nplurals=1; plural=0;\\n\"\n\n", module)
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&b, "msgid %q\nmsgstr %q\n\n", pairs[i], pairs[i+1])
	}
	return []byte(b.String())
}

// entry is an entry's id and version in a catalog, by source text
func entry(t *testing.T, catalogID int64, msgid string) (id int64, version int) {
	t.Helper()
	if err := env.DB.QueryRow(`SELECT id, version FROM entries WHERE catalog_id = $1 AND msgid = $2`, catalogID, msgid).
		Scan(&id, &version); err != nil {
		t.Fatalf("entry %q: %v", msgid, err)
	}
	return id, version
}

// msgstr is an entry's first translation
func msgstr(t *testing.T, id int64) string {
	return scalar[string](t, `SELECT msgstr->>0 FROM entries WHERE id = $1`, id)
}
