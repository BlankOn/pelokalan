// Package githubapp talks to GitHub as the Pelokalan GitHub App: it finds the app's installation on a
// repository, gets short-lived installation tokens (cached until shortly before they expire), reads a
// file on a branch and commits a new version of it through the contents API. Nothing else is done
// with the access: no branches, no pull requests (yet).
//
// The app is registered once by the operator (GITHUB_APP_ID, GITHUB_APP_SLUG, GITHUB_APP_PRIVATE_KEY);
// a repository's maintainers then install it on their repository with write access to contents.
package githubapp

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultAPI is GitHub's REST API
const DefaultAPI = "https://api.github.com"

// Errors returned by the client
var (
	ErrNotInstalled = errors.New("the GitHub App is not installed on this repository")
	ErrNoWrite      = errors.New("the GitHub App may not write the repository's contents")
	ErrConflict     = errors.New("the file changed on the branch meanwhile")
	ErrNotFound     = errors.New("not found on GitHub")
)

// Client is the GitHub App
type Client struct {
	appID int64
	slug  string
	key   *rsa.PrivateKey
	api   string
	http  *http.Client
	now   func() time.Time

	mu     sync.Mutex
	tokens map[int64]installToken // by installation
}

type installToken struct {
	token   string
	expires time.Time
	write   bool // contents: write
}

// New creates the client from the app id, its URL name (slug) and its PEM private key; api "" = GitHub
func New(appID int64, slug, privateKeyPEM, api string) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("github app private key: %w", err)
	}
	if api == "" {
		api = DefaultAPI
	}
	return &Client{appID: appID, slug: slug, key: key, api: strings.TrimRight(api, "/"),
		http: &http.Client{Timeout: 30 * time.Second}, now: time.Now, tokens: map[int64]installToken{}}, nil
}

// InstallURL is where a maintainer installs the app on their repositories
func (c *Client) InstallURL() string {
	return "https://github.com/apps/" + url.PathEscape(c.slug) + "/installations/new"
}

// Slug is the app's URL name ("pelokalan"); commits it makes are by "<slug>[bot]"
func (c *Client) Slug() string { return c.slug }

// appJWT signs a token for the app itself (10 minutes at most; issued a minute early for clock drift)
func (c *Client) appJWT() (string, error) {
	now := c.now()
	return jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(c.appID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}).SignedString(c.key)
}

func (c *Client) do(ctx context.Context, method, path, bearer string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "pelokalan-github-app")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return res.StatusCode, err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return res.StatusCode, fmt.Errorf("github %s %s: %d %s", method, path, res.StatusCode, e.Message)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return res.StatusCode, err
		}
	}
	return res.StatusCode, nil
}

func repoPath(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// AppInfo is what GitHub says about the app itself
type AppInfo struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// App asks GitHub who the app is (checks the app id and private key)
func (c *Client) App(ctx context.Context) (*AppInfo, error) {
	tok, err := c.appJWT()
	if err != nil {
		return nil, err
	}
	var out AppInfo
	if _, err := c.do(ctx, http.MethodGet, "/app", tok, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Installation returns the app's installation id on a repository ("owner/name")
func (c *Client) Installation(ctx context.Context, repo string) (int64, error) {
	tok, err := c.appJWT()
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"id"`
	}
	status, err := c.do(ctx, http.MethodGet, repoPath(repo)+"/installation", tok, nil, &out)
	if status == http.StatusNotFound {
		return 0, ErrNotInstalled
	}
	if err != nil {
		return 0, err
	}
	return out.ID, nil
}

// token returns an installation token (cached until 5 minutes before it expires)
func (c *Client) token(ctx context.Context, installation int64) (installToken, error) {
	c.mu.Lock()
	t, ok := c.tokens[installation]
	c.mu.Unlock()
	if ok && c.now().Before(t.expires.Add(-5*time.Minute)) {
		return t, nil
	}
	jwtTok, err := c.appJWT()
	if err != nil {
		return installToken{}, err
	}
	var out struct {
		Token       string            `json:"token"`
		ExpiresAt   time.Time         `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	status, err := c.do(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installation, 10)+"/access_tokens", jwtTok, nil, &out)
	if status == http.StatusNotFound {
		return installToken{}, ErrNotInstalled
	}
	if err != nil {
		return installToken{}, err
	}
	t = installToken{token: out.Token, expires: out.ExpiresAt, write: out.Permissions["contents"] == "write"}
	c.mu.Lock()
	c.tokens[installation] = t
	c.mu.Unlock()
	return t, nil
}

// Access is what the app may do on a repository
type Access struct {
	Installation int64
	Write        bool // contents: write
}

// Check finds the installation on a repository and whether it may write its contents
func (c *Client) Check(ctx context.Context, repo string) (Access, error) {
	id, err := c.Installation(ctx, repo)
	if err != nil {
		return Access{}, err
	}
	c.mu.Lock()
	delete(c.tokens, id) // permissions may have changed: ask again
	c.mu.Unlock()
	t, err := c.token(ctx, id)
	if err != nil {
		return Access{}, err
	}
	return Access{Installation: id, Write: t.write}, nil
}

// File is a file on a branch
type File struct {
	Data []byte
	SHA  string // blob sha, needed to replace it
}

// GetFile reads a file on a branch
func (c *Client) GetFile(ctx context.Context, installation int64, repo, branch, path string) (*File, error) {
	t, err := c.token(ctx, installation)
	if err != nil {
		return nil, err
	}
	var out struct {
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	status, err := c.do(ctx, http.MethodGet, repoPath(repo)+"/contents/"+escapePath(path)+"?ref="+url.QueryEscape(branch), t.token, nil, &out)
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	content := out.Content
	if content == "" && out.Size > 0 { // over 1 MB: the contents API leaves it out; read the blob
		var blob struct {
			Content string `json:"content"`
		}
		if _, err := c.do(ctx, http.MethodGet, repoPath(repo)+"/git/blobs/"+url.PathEscape(out.SHA), t.token, nil, &blob); err != nil {
			return nil, err
		}
		content = blob.Content
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content, "\n", ""))
	if err != nil {
		return nil, err
	}
	return &File{Data: data, SHA: out.SHA}, nil
}

// Person is a commit author
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Commit is the result of PutFile
type Commit struct {
	SHA string
	URL string // the commit's page on GitHub
}

// PutFile replaces a file on a branch with one commit (sha = the blob it replaces; ErrConflict when
// the branch has another version by now). The commit is authored by author and committed by the app.
func (c *Client) PutFile(ctx context.Context, installation int64, repo, branch, path string, data []byte, sha, message string, author Person) (*Commit, error) {
	t, err := c.token(ctx, installation)
	if err != nil {
		return nil, err
	}
	if !t.write {
		return nil, ErrNoWrite
	}
	body := map[string]any{"message": message, "content": base64.StdEncoding.EncodeToString(data), "sha": sha, "branch": branch}
	if author.Email != "" {
		body["author"] = author
	}
	var out struct {
		Commit struct {
			SHA     string `json:"sha"`
			HTMLURL string `json:"html_url"`
		} `json:"commit"`
	}
	status, err := c.do(ctx, http.MethodPut, repoPath(repo)+"/contents/"+escapePath(path), t.token, body, &out)
	switch status {
	case http.StatusConflict, http.StatusUnprocessableEntity:
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "sha") || status == http.StatusConflict {
			return nil, ErrConflict
		}
	case http.StatusForbidden:
		return nil, ErrNoWrite
	case http.StatusNotFound:
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &Commit{SHA: out.Commit.SHA, URL: out.Commit.HTMLURL}, nil
}

func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
