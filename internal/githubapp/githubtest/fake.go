// Package githubtest is a fake of the parts of GitHub's REST API the Pelokalan GitHub App uses, for tests:
// installations, installation tokens, reading and replacing files on a branch. Never use the real site in tests.
package githubtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Commit is a commit the fake received
type Commit struct {
	Repo, Branch, Path, Message string
	AuthorName, AuthorEmail     string
	Data                        []byte
	SHA                         string
}

// Server is the fake API
type Server struct {
	*httptest.Server
	AppID      int64
	PrivateKey string // PEM

	mu       sync.Mutex
	pub      *rsa.PublicKey
	installs map[string]int64 // repo → installation
	write    map[int64]bool
	files    map[string][]byte // repo|branch|path
	tokens   map[string]int64  // token → installation
	Commits  []Commit
}

// New starts a fake with a fresh app key
func New() *Server {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	s := &Server{AppID: 4242, PrivateKey: string(pemKey), pub: &key.PublicKey, installs: map[string]int64{},
		write: map[int64]bool{}, files: map[string][]byte{}, tokens: map[string]int64{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Install installs the app on a repository, with or without write access to contents
func (s *Server) Install(repo string, write bool) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := int64(1000 + len(s.installs))
	s.installs[repo] = id
	s.write[id] = write
	return id
}

// SetFile puts a file on a branch (as if someone pushed it)
func (s *Server) SetFile(repo, branch, path string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[repo+"|"+branch+"|"+path] = data
}

// File returns a file on a branch
func (s *Server) File(repo, branch, path string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[repo+"|"+branch+"|"+path]
}

// LastCommits returns a copy of the commits received
func (s *Server) LastCommits() []Commit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Commit(nil), s.Commits...)
}

func blobSHA(data []byte) string {
	h := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...))
	return hex.EncodeToString(h[:])
}

func (s *Server) appAuth(r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claims := &jwt.RegisteredClaims{}
	t, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return s.pub, nil },
		jwt.WithValidMethods([]string{"RS256"}))
	return err == nil && t.Valid && claims.Issuer == strconv.FormatInt(s.AppID, 10)
}

func (s *Server) installation(r *http.Request) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	return id, ok
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/app" && r.Method == http.MethodGet:
		if !s.appAuth(r) {
			reply(w, 401, map[string]string{"message": "bad app token"})
			return
		}
		reply(w, 200, map[string]any{"slug": "pelokalan-test", "name": "Pelokalan Test", "owner": map[string]string{"login": "pelokalan"}})
	case strings.HasPrefix(p, "/app/installations/") && strings.HasSuffix(p, "/access_tokens") && r.Method == http.MethodPost:
		if !s.appAuth(r) {
			reply(w, 401, map[string]string{"message": "bad app token"})
			return
		}
		id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(p, "/app/installations/"), "/access_tokens"), 10, 64)
		s.mu.Lock()
		write, ok := s.write[id]
		tok := fmt.Sprintf("ghs_test_%d_%d", id, len(s.tokens))
		if ok {
			s.tokens[tok] = id
		}
		s.mu.Unlock()
		if !ok {
			reply(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		perm := "read"
		if write {
			perm = "write"
		}
		reply(w, 201, map[string]any{"token": tok, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"permissions": map[string]string{"contents": perm, "metadata": "read"}})
	case strings.HasPrefix(p, "/repos/"):
		rest := strings.TrimPrefix(p, "/repos/")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) < 3 {
			reply(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		repo := parts[0] + "/" + parts[1]
		switch {
		case parts[2] == "installation":
			if !s.appAuth(r) {
				reply(w, 401, map[string]string{"message": "bad app token"})
				return
			}
			s.mu.Lock()
			id, ok := s.installs[repo]
			s.mu.Unlock()
			if !ok {
				reply(w, 404, map[string]string{"message": "Not Found"})
				return
			}
			reply(w, 200, map[string]any{"id": id})
		case strings.HasPrefix(parts[2], "contents/"):
			id, ok := s.installation(r)
			s.mu.Lock()
			allowed := ok && s.installs[repo] == id
			canWrite := s.write[id]
			s.mu.Unlock()
			if !allowed {
				reply(w, 404, map[string]string{"message": "Not Found"})
				return
			}
			path := strings.TrimPrefix(parts[2], "contents/")
			if r.Method == http.MethodGet {
				branch := r.URL.Query().Get("ref")
				data := s.File(repo, branch, path)
				if data == nil {
					reply(w, 404, map[string]string{"message": "Not Found"})
					return
				}
				reply(w, 200, map[string]any{"sha": blobSHA(data), "size": len(data), "encoding": "base64",
					"content": base64.StdEncoding.EncodeToString(data)})
				return
			}
			if !canWrite {
				reply(w, 403, map[string]string{"message": "Resource not accessible by integration"})
				return
			}
			var in struct {
				Message, Content, SHA, Branch string
				Author                        struct{ Name, Email string }
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			cur := s.File(repo, in.Branch, path)
			if cur == nil || blobSHA(cur) != in.SHA {
				reply(w, 409, map[string]string{"message": "is at " + blobSHA(cur) + " but expected " + in.SHA})
				return
			}
			data, _ := base64.StdEncoding.DecodeString(in.Content)
			s.SetFile(repo, in.Branch, path, data)
			sha := blobSHA(append([]byte("commit "), data...))
			s.mu.Lock()
			s.Commits = append(s.Commits, Commit{Repo: repo, Branch: in.Branch, Path: path, Message: in.Message,
				AuthorName: in.Author.Name, AuthorEmail: in.Author.Email, Data: data, SHA: sha})
			s.mu.Unlock()
			reply(w, 200, map[string]any{"commit": map[string]any{"sha": sha, "html_url": "https://github.com/" + repo + "/commit/" + sha}})
		default:
			reply(w, 404, map[string]string{"message": "Not Found"})
		}
	default:
		reply(w, 404, map[string]string{"message": "Not Found"})
	}
}
