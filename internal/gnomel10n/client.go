// Package gnomel10n reads the public, read-only JSON API of the GNOME translation
// site (https://l10n.gnome.org/api/v1/): which modules and branches belong to a
// release, and the workflow state and statistics of one module's translation.
//
// The API is rate limited per client address (anonymous requests are throttled
// after roughly a hundred per hour), so every answer is cached, stale answers are
// served while a refresh is pending, and the client keeps its own hourly budget.
package gnomel10n

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pelokalan/internal/netsafe"
)

// DefaultBase is the GNOME translation site
const DefaultBase = "https://l10n.gnome.org"

const (
	// StatsTTL is how long a module's state counts as fresh
	StatsTTL = time.Hour
	// ListTTL is how long the release lists count as fresh (they change rarely)
	ListTTL = 12 * time.Hour
	// DefaultBudget is how many API requests the client sends per hour at most,
	// well below the site's anonymous limit
	DefaultBudget = 40
	// requestTimeout bounds one API request
	requestTimeout = 20 * time.Second
	maxBody        = 2 << 20
)

var (
	// ErrBudget is returned when the hourly budget is used up (or the site throttled us)
	// and nothing is cached
	ErrBudget = errors.New("upstream request budget used up")
	// ErrNotFound is returned when the site does not know the release, module or language
	ErrNotFound = errors.New("not found upstream")
)

// Stats is the upstream state of one module/branch/domain translation
type Stats struct {
	Module       string
	Branch       string
	Domain       string
	Language     string
	State        string // workflow state, e.g. "Translating"; "None" when nobody works on it
	Translated   int
	Fuzzy        int
	Untranslated int
	POFile       string // path of the current PO file on the site
	POTFile      string
	FetchedAt    time.Time
}

// Total is the number of strings
func (s *Stats) Total() int { return s.Translated + s.Fuzzy + s.Untranslated }

// Entry is one module/branch/domain of a release listing
type Entry struct {
	Module    string
	Branch    string
	Domain    string
	StatsPath string
}

// Stem is the start of the PO file name the site uses for the entry
// ("gnome-maps.gnome-51" in gnome-maps.gnome-51.id.po), lowercase: the module for the
// "po" domain, "<module>-help" for documentation and the domain name otherwise.
func (e Entry) Stem() string {
	name := e.Domain
	switch e.Domain {
	case "po":
		name = e.Module
	case "help":
		name = e.Module + "-help"
	}
	return strings.ToLower(name + "." + e.Branch)
}

// Client talks to the API. It is safe for concurrent use.
type Client struct {
	base   string
	http   *http.Client
	budget int
	agent  string
	now    func() time.Time

	mu           sync.Mutex
	cache        map[string]*item         // by API path
	inflight     map[string]chan struct{} // paths being fetched
	sent         []time.Time              // request times within the last hour
	blockedUntil time.Time                // the site asked us to wait
}

type item struct {
	body    []byte // nil when the site answered 404
	fetched time.Time
}

// New creates a client for base (DefaultBase when empty). Unless allowPrivate is set,
// connections to private or local addresses are refused (SSRF protection).
func New(base string, allowPrivate bool, userAgent string) *Client {
	if base == "" {
		base = DefaultBase
	}
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Transport: netsafe.Transport(allowPrivate),
			Timeout:   requestTimeout,
		},
		budget:   DefaultBudget,
		agent:    userAgent,
		now:      time.Now,
		cache:    map[string]*item{},
		inflight: map[string]chan struct{}{},
	}
}

// Base returns the site address, e.g. "https://l10n.gnome.org"
func (c *Client) Base() string { return c.base }

// SetBudget changes the hourly request budget
func (c *Client) SetBudget(n int) { c.mu.Lock(); c.budget = n; c.mu.Unlock() }

// Host is the site's host name, e.g. "l10n.gnome.org"
func (c *Client) Host() string {
	if u, err := url.Parse(c.base); err == nil {
		return u.Host
	}
	return ""
}

// PageURL is the site's page for a translation (its workflow and files)
func (c *Client) PageURL(s *Stats) string {
	return c.base + "/vertimus/" + seg(s.Module) + "/" + seg(s.Branch) + "/" + seg(s.Domain) + "/" + seg(s.Language) + "/"
}

// FileURL turns a path from the API (po_file) into an address on the site
func (c *Client) FileURL(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") {
		return ""
	}
	return c.base + path
}

// ReleaseFromURL returns the release of a download address on the site, e.g.
// "gnome-51" for https://l10n.gnome.org/languages/id/gnome-51/ui.tar.gz ("" = none)
func (c *Client) ReleaseFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, c.Host()) {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "languages" {
		return parts[2]
	}
	return ""
}

// Locale converts a language code to the site's form ("pt-BR" → "pt_BR")
func Locale(lang string) string { return strings.ReplaceAll(strings.TrimSpace(lang), "-", "_") }

func seg(s string) string { return url.PathEscape(s) }

// Releases returns the names of the releases the site knows (e.g. "gnome-51")
func (c *Client) Releases(ctx context.Context) (map[string]bool, error) {
	body, err := c.get(ctx, "/api/v1/releases/", ListTTL)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, r := range list {
		out[r.Name] = true
	}
	return out, nil
}

// Release lists the modules, branches and domains of a release for a language
func (c *Client) Release(ctx context.Context, release, lang string) ([]Entry, error) {
	body, err := c.get(ctx, "/api/v1/releases/"+seg(release)+"/languages/"+seg(Locale(lang)), ListTTL)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Modules []struct {
			Module string `json:"module"`
			Branch string `json:"branch"`
			Stats  string `json:"stats"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(doc.Modules))
	for _, m := range doc.Modules {
		domain := ""
		if i := strings.Index(m.Stats, "/domains/"); i >= 0 {
			domain, _, _ = strings.Cut(m.Stats[i+len("/domains/"):], "/")
		}
		if m.Module == "" || m.Branch == "" || domain == "" || !strings.HasPrefix(m.Stats, "/api/v1/") {
			continue
		}
		out = append(out, Entry{Module: m.Module, Branch: m.Branch, Domain: domain, StatsPath: m.Stats})
	}
	return out, nil
}

// Stats returns the state of one entry
func (c *Client) Stats(ctx context.Context, e Entry) (*Stats, error) {
	body, err := c.get(ctx, e.StatsPath, StatsTTL)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Module     string `json:"module"`
		Branch     string `json:"branch"`
		Domain     string `json:"domain"`
		Language   string `json:"language"`
		State      string `json:"state"`
		Statistics struct {
			Trans   int `json:"trans"`
			Fuzzy   int `json:"fuzzy"`
			Untrans int `json:"untrans"`
		} `json:"statistics"`
		POTFile string `json:"pot_file"`
		POFile  string `json:"po_file"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if doc.Module == "" {
		return nil, ErrNotFound
	}
	var fetched time.Time
	c.mu.Lock()
	if it := c.cache[e.StatsPath]; it != nil {
		fetched = it.fetched
	}
	c.mu.Unlock()
	return &Stats{Module: doc.Module, Branch: doc.Branch, Domain: doc.Domain, Language: doc.Language,
		State: doc.State, Translated: doc.Statistics.Trans, Fuzzy: doc.Statistics.Fuzzy,
		Untranslated: doc.Statistics.Untrans, POFile: doc.POFile, POTFile: doc.POTFile, FetchedAt: fetched}, nil
}

// FileStem is the part of a PO file name that identifies the upstream entry:
// "gnome-maps.gnome-51.id.po" → "gnome-maps.gnome-51" (lowercase; "" when the
// name does not end in ".<lang>.po")
func FileStem(filename, lang string) string {
	name := strings.ToLower(filename)
	suffix := "." + strings.ToLower(Locale(lang)) + ".po"
	if alt := "." + strings.ToLower(lang) + ".po"; !strings.HasSuffix(name, suffix) && strings.HasSuffix(name, alt) {
		suffix = alt
	}
	if !strings.HasSuffix(name, suffix) || len(name) == len(suffix) {
		return ""
	}
	return strings.TrimSuffix(name, suffix)
}

// Lookup finds the upstream state of PO files of one release and language, keyed by
// file name. Files the release does not list are left out; so are files whose state
// could not be fetched (budget used up, site unreachable) and is not cached.
func (c *Client) Lookup(ctx context.Context, release, lang string, filenames []string) map[string]*Stats {
	out := map[string]*Stats{}
	entries, err := c.Release(ctx, release, lang)
	if err != nil {
		return out
	}
	byStem := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byStem[e.Stem()] = e
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		sem = make(chan struct{}, 4)
	)
	for _, f := range filenames {
		e, ok := byStem[FileStem(f, lang)]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(f string, e Entry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s, err := c.Stats(ctx, e); err == nil {
				mu.Lock()
				out[f] = s
				mu.Unlock()
			}
		}(f, e)
	}
	wg.Wait()
	return out
}

// Remaining is how many requests the hourly budget still allows now (0 while the site
// asked us to wait)
func (c *Client) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Before(c.blockedUntil) {
		return 0
	}
	n := 0
	for _, t := range c.sent {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	return max(c.budget-n, 0)
}

// get returns the cached answer for path while it is fresher than ttl, else fetches it.
// A stale answer is returned when fetching fails.
func (c *Client) get(ctx context.Context, path string, ttl time.Duration) ([]byte, error) {
	c.mu.Lock()
	it := c.cache[path]
	if it != nil {
		if c.now().Sub(it.fetched) < ttl {
			c.mu.Unlock()
			return found(it)
		}
	}
	c.mu.Unlock()
	body, err := c.fetch(ctx, path)
	if err != nil && it != nil && !errors.Is(err, ErrNotFound) {
		return found(it) // stale but better than nothing
	}
	return body, err
}

func found(it *item) ([]byte, error) {
	if it.body == nil {
		return nil, ErrNotFound
	}
	return it.body, nil
}

// fetch downloads path once, even when several callers ask at the same time. The
// download runs in the background, so it still fills the cache when the caller gives up.
func (c *Client) fetch(ctx context.Context, path string) ([]byte, error) {
	c.mu.Lock()
	done, running := c.inflight[path]
	if !running {
		if err := c.takeBudget(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
		done = make(chan struct{})
		c.inflight[path] = done
		go c.download(path, done)
	}
	c.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	it := c.cache[path]
	c.mu.Unlock()
	if it == nil {
		return nil, errors.New("upstream request failed")
	}
	if c.now().Sub(it.fetched) > requestTimeout*2 {
		return nil, errors.New("upstream request failed") // only an old answer is cached
	}
	return found(it)
}

// takeBudget counts one request; c.mu must be held
func (c *Client) takeBudget() error {
	now := c.now()
	if now.Before(c.blockedUntil) {
		return ErrBudget
	}
	keep := c.sent[:0]
	for _, t := range c.sent {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	c.sent = keep
	if len(c.sent) >= c.budget {
		return ErrBudget
	}
	c.sent = append(c.sent, now)
	return nil
}

var waitRe = regexp.MustCompile(`available in (\d+) second`)

func (c *Client) download(path string, done chan struct{}) {
	defer func() {
		c.mu.Lock()
		delete(c.inflight, path)
		c.mu.Unlock()
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	if c.agent != "" {
		req.Header.Set("User-Agent", c.agent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	switch resp.StatusCode {
	case http.StatusOK:
		if !json.Valid(body) {
			return
		}
		c.store(path, body, now)
	case http.StatusNotFound:
		c.store(path, nil, now)
	case http.StatusTooManyRequests:
		c.blockedUntil = now.Add(retryAfter(resp.Header.Get("Retry-After"), body))
	}
}

func (c *Client) store(path string, body []byte, now time.Time) {
	c.cache[path] = &item{body: body, fetched: now}
}

// retryAfter reads how long the site wants us to wait (an hour when it does not say)
func retryAfter(header string, body []byte) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if m := waitRe.FindSubmatch(body); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Hour
}

// Download fetches a file of the site (a po_file path from the API), at most maxBytes
func (c *Client) Download(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	u := c.FileURL(path)
	if u == "" {
		return nil, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.agent != "" {
		req.Header.Set("User-Agent", c.agent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("file too large")
	}
	return data, nil
}
