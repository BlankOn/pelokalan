// Package tpsite reads the public pages of the Translation Project (translationproject.org).
//
// The site has no API. Each language team has a page (/team/<code>.html) with a table of every
// textual domain: its current version, a link to the team's PO file for that version (already
// merged with the newest template; /POT-files/… when nobody translated it yet), the translated
// count, the last update and the assigned translator. The page is read as untrusted text: only
// the pieces below are taken from it, nothing is executed.
//
// Translations go back by email: the translator sends the file named "<domain>-<version>.<team>.po"
// to the robot from their own address (it checks the assignment and the disclaimer), so this
// package only builds the names and addresses for that.
package tpsite

import (
	"bytes"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pelokalan/pkg/po"
)

// Base is the site's address (a variable so tests can serve recorded pages)
var Base = "https://translationproject.org"

// RobotAddress receives PO file submissions (subject: SubmitName)
const RobotAddress = "robot@translationproject.org"

// ErrLayout is returned when a team page no longer has the expected table
var ErrLayout = errors.New("team page layout not recognised")

// Domain is one row of a team page
type Domain struct {
	Name       string // textual domain, e.g. "bash"
	Version    string // current version, e.g. "5.3-rc2"
	FileURL    string // absolute address of the team's PO file (or the POT when untranslated)
	Template   bool   // FileURL is a POT file: nothing translated yet
	Translated int
	Total      int
	Updated    time.Time // last submission (zero = never)
	Translator string    // assigned translator ("" = none)
	Restricted bool      // the domain needs a signed disclaimer from the translator
}

// Fingerprint changes whenever the team's file for the domain changes
func (d Domain) Fingerprint() string {
	return d.Version + "|" + d.FileURL + "|" + strconv.Itoa(d.Translated) + "/" + strconv.Itoa(d.Total) + "|" + d.Updated.Format("2006-01-02")
}

var (
	teamRe   = regexp.MustCompile(`^[a-z]{2,3}(_[A-Z]{2})?(@[a-z]+)?$`)
	domainRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_+.-]{0,99}$`)
	rowRe    = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	cellRe   = regexp.MustCompile(`(?is)<td\b([^>]*)>(.*?)</td>`)
	hrefRe   = regexp.MustCompile(`(?i)href="([^"]*)"`)
	classRe  = regexp.MustCompile(`(?i)class="([^"]*)"`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	countRe  = regexp.MustCompile(`^(\d+)\s*/\s*(\d+)$`)
	domLink  = regexp.MustCompile(`^\.\./domain/([^/"]+)\.html$`)
	fileLink = regexp.MustCompile(`^\.\./(PO-files/[^/"]+/[^/"]+\.po|POT-files/[^/"]+\.pot)$`)
)

// ValidTeam reports whether code looks like a team code ("id", "pt_BR", "sr@latin")
func ValidTeam(code string) bool { return teamRe.MatchString(code) }

// ParseTeam reads a team given as its code or as the address of its team page
func ParseTeam(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(strings.TrimPrefix(u.Host, "www."), "translationproject.org") && u.Scheme+"://"+u.Host != Base {
			return "", false
		}
		p := strings.Trim(u.Path, "/")
		if !strings.HasPrefix(p, "team/") || !strings.HasSuffix(p, ".html") {
			return "", false
		}
		raw = strings.TrimSuffix(strings.TrimPrefix(p, "team/"), ".html")
	}
	return raw, ValidTeam(raw)
}

// TeamURL is the address of a team's page
func TeamURL(team string) string { return Base + "/team/" + team + ".html" }

// DomainURL is the address of a domain's page
func DomainURL(domain string) string { return Base + "/domain/" + url.PathEscape(domain) + ".html" }

// SubmitName is the file name (and email subject) the robot expects: "bash-5.3-rc2.id.po"
func SubmitName(domain, version, team string) string {
	return domain + "-" + version + "." + team + ".po"
}

// FileName is the Pelokalan name of a domain's file: the domain is the module ("bash.id.po";
// dots in a domain become dashes so the module stays whole)
func FileName(domain, team string) string {
	return strings.ReplaceAll(domain, ".", "-") + "." + team + ".po"
}

// ParseTeamPage reads the domain table of a team page
func ParseTeamPage(data []byte) ([]Domain, error) {
	var out []Domain
	for _, m := range rowRe.FindAllSubmatch(data, -1) {
		cells := cellRe.FindAllSubmatch(m[1], -1)
		if len(cells) < 5 {
			continue
		}
		name := link(cells[0][2], domLink)
		file := link(cells[1][2], fileLink)
		if name == "" || file == "" || !domainRe.MatchString(name) {
			continue
		}
		d := Domain{Name: name, Version: text(cells[1][2]), FileURL: Base + "/" + file,
			Template: strings.HasPrefix(file, "POT-files/"), Translator: text(cells[4][2])}
		if c := classRe.FindSubmatch(cells[0][1]); c != nil {
			d.Restricted = strings.Contains(string(c[1]), "restricted")
		}
		if !domainRe.MatchString(d.Version) {
			continue
		}
		if c := countRe.FindStringSubmatch(text(cells[2][2])); c != nil {
			d.Translated, _ = strconv.Atoi(c[1])
			d.Total, _ = strconv.Atoi(c[2])
		}
		if t, err := time.Parse("2006-01-02", text(cells[3][2])); err == nil {
			d.Updated = t
		}
		if len(d.Translator) > 200 {
			d.Translator = d.Translator[:200]
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, ErrLayout
	}
	return out, nil
}

func link(cell []byte, re *regexp.Regexp) string {
	h := hrefRe.FindSubmatch(cell)
	if h == nil {
		return ""
	}
	m := re.FindStringSubmatch(html.UnescapeString(string(h[1])))
	if m == nil {
		return ""
	}
	return m[1]
}

func text(cell []byte) string {
	s := html.UnescapeString(tagRe.ReplaceAllString(string(cell), " "))
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// Prepare makes a downloaded file ready to import as the team's translation: Latin-1 files
// become UTF-8 (the header says so afterwards), and a template becomes an empty PO file of
// the team's language with its plural rule
func Prepare(data []byte, team string, template bool) ([]byte, error) {
	if !utf8.Valid(data) {
		data = latin1ToUTF8(data)
	}
	cat, err := po.ParseBytes(data)
	if err != nil {
		return nil, err
	}
	header := cat.Header
	header = po.SetHeaderField(header, "Content-Type", "text/plain; charset=UTF-8")
	if template {
		cat.HeaderFuzzy = false
		header = po.SetHeaderField(header, "Language", team)
		plural := po.LookupLanguage(team).PluralForms
		header = po.SetHeaderField(header, "Plural-Forms", plural)
		n := po.NPlurals(plural)
		for _, e := range cat.Entries { // one empty form per plural form of the language
			if e.IsPlural() {
				e.Translation = make([]string, n)
			}
		}
	} else if cat.HeaderField("Language") == "" {
		header = po.SetHeaderField(header, "Language", team)
	}
	if bytes.Equal([]byte(header), []byte(cat.Header)) && utf8.Valid(data) && !template {
		return data, nil
	}
	cat.Header = header
	return cat.Bytes(), nil
}

func latin1ToUTF8(b []byte) []byte {
	out := make([]rune, len(b))
	for i, c := range b {
		out[i] = rune(c)
	}
	return []byte(string(out))
}
