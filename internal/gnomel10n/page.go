package gnomel10n

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The API has no endpoint that returns many modules at once (one request per module and
// the anonymous limit is about a hundred per hour), but the site's release page for a
// language lists every module of the release with its counts and workflow state. It is
// read as untrusted text: only the pieces below are taken from it, nothing is executed.

// ErrLayout is returned when a release page no longer has the expected rows
var ErrLayout = errors.New("release page layout not recognised")

// PageRow is one module/branch/domain of a release page
type PageRow struct {
	Module       string
	Branch       string
	Domain       string
	Language     string
	State        string // as the API names it: "None", "Translating", "ToReview", …
	Translated   int
	Fuzzy        int
	Untranslated int
	Changed      time.Time // last workflow action (zero = none)
}

// Entry returns the row as a release entry (for its file name Stem)
func (r PageRow) Entry() Entry { return Entry{Module: r.Module, Branch: r.Branch, Domain: r.Domain} }

const maxPage = 8 << 20

var (
	rowRe   = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	linkRe  = regexp.MustCompile(`href="/vertimus/([^/"]+)/([^/"]+)/([^/"]+)/([^/"]+)/"`)
	statsRe = regexp.MustCompile(`(?is)<pre[^>]*class="[^"]*statistics_workflow[^"]*"[^>]*>(.*?)</pre>`)
	countRe = regexp.MustCompile(`(?is)<span[^>]*class="(translated|fuzzy|untranslated)"[^>]*>([^<]*)</span>`)
	badgeRe = regexp.MustCompile(`(?is)<span[^>]*class="badge\b[^"]*"[^>]*>([^<]*)</span>`)
	timeRe  = regexp.MustCompile(`<time[^>]*datetime="([^"]+)"`)
	digitRe = regexp.MustCompile(`[^0-9]`)
)

// pageStates maps the page's English state labels to the API's names
var pageStates = map[string]string{
	"": "None", "none": "None", "translating": "Translating", "translated": "Translated",
	"proofreading": "Proofreading", "proofread": "Proofread", "to review": "ToReview",
	"to commit": "ToCommit", "committing": "Committing", "committed": "Committed",
}

// ParseReleasePage reads the module rows of a release page. Rows without a module link or
// statistics are skipped; ErrLayout means no row could be read at all.
func ParseReleasePage(data []byte) ([]PageRow, error) {
	var out []PageRow
	for _, m := range rowRe.FindAllSubmatch(data, -1) {
		row := m[1]
		link := linkRe.FindSubmatch(row)
		stats := statsRe.FindSubmatch(row)
		if link == nil || stats == nil {
			continue
		}
		var parts [4]string
		ok := true
		for i := range parts {
			v, err := url.PathUnescape(string(link[i+1]))
			if err != nil || v == "" {
				ok = false
			}
			parts[i] = v
		}
		if !ok {
			continue
		}
		r := PageRow{Module: parts[0], Branch: parts[1], Domain: parts[2], Language: parts[3]}
		for _, c := range countRe.FindAllSubmatch(stats[1], -1) {
			n, _ := strconv.Atoi(digitRe.ReplaceAllString(string(c[2]), "")) // blank = 0
			switch strings.ToLower(string(c[1])) {
			case "translated":
				r.Translated = n
			case "fuzzy":
				r.Fuzzy = n
			case "untranslated":
				r.Untranslated = n
			}
		}
		label := ""
		if b := badgeRe.FindSubmatch(row); b != nil {
			label = strings.Join(strings.Fields(html.UnescapeString(string(b[1]))), " ")
		}
		if s, ok := pageStates[strings.ToLower(label)]; ok {
			r.State = s
		} else {
			r.State = strings.ReplaceAll(label, " ", "") // a state the site added later: shown as sent
		}
		if t := timeRe.FindSubmatch(row); t != nil {
			if at, err := time.Parse(time.RFC3339Nano, string(t[1])); err == nil {
				r.Changed = at
			}
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, ErrLayout
	}
	return out, nil
}

// ReleasePage downloads and reads the site's page of a release for a language: kind is
// "ui" (program translations) or "doc" (documentation). It counts against the hourly budget.
func (c *Client) ReleasePage(ctx context.Context, release, lang, kind string) ([]PageRow, error) {
	if kind != "ui" && kind != "doc" {
		return nil, fmt.Errorf("unknown page kind %q", kind)
	}
	c.mu.Lock()
	err := c.takeBudget()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	u := c.base + "/languages/" + seg(Locale(lang)) + "/" + seg(release) + "/" + kind + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en") // state labels are matched in English
	if c.agent != "" {
		req.Header.Set("User-Agent", c.agent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPage+1))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		c.mu.Lock()
		c.blockedUntil = c.now().Add(retryAfter(resp.Header.Get("Retry-After"), data))
		c.mu.Unlock()
		return nil, ErrBudget
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	case len(data) > maxPage:
		return nil, errors.New("release page too large")
	}
	return ParseReleasePage(data)
}
