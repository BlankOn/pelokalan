package config

import (
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func init() {
	// Load .env file if it exists (ignore error if not found)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
}

// Config holds application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
	Site     SiteConfig
	Mail     MailConfig
	Auth     AuthConfig
	Glossary GlossaryConfig
	AI       AIConfig
}

// AIConfig holds settings for LLM pre-translation
type AIConfig struct {
	// SecretKey encrypts stored API keys; defaults to JWT_SECRET
	SecretKey string
	// AllowPrivateHosts permits API base URLs on local or private networks
	AllowPrivateHosts bool
}

// GlossaryConfig holds settings for automatic glossary downloads
type GlossaryConfig struct {
	// FetchInterval is how often each download URL is fetched
	FetchInterval time.Duration
	// MaxDownloadMB limits the size of a downloaded archive
	MaxDownloadMB int64
	// AllowPrivateHosts permits URLs on local or private networks (off by default)
	AllowPrivateHosts bool
	// ThesaurusURL is where the Indonesian thesaurus JSON is downloaded from when the
	// database has none yet; empty disables the thesaurus
	ThesaurusURL string
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// MaxUploadMB limits the size of uploaded translation files.
	MaxUploadMB int64
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	Database     string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret     string
	Expiration time.Duration
}

// SiteConfig holds site-wide configuration
type SiteConfig struct {
	BaseURL string
	Name    string
	// DefaultLang is the UI language used when the visitor has not picked one.
	DefaultLang string
}

// MailConfig holds outgoing email configuration (Mailtrap)
type MailConfig struct {
	// APIToken is the Mailtrap API token. When empty, emails are logged instead of sent.
	APIToken string
	// InboxID selects the Mailtrap Email Testing (sandbox) inbox. When empty,
	// emails go through the Email Sending (production) API.
	InboxID string
	// FromEmail must belong to a verified sending domain in production mode.
	FromEmail string
	FromName  string
}

// AuthConfig holds sign-in configuration
type AuthConfig struct {
	// Whitelist is the set of email addresses allowed to register or sign in.
	// When empty, anyone can register and sign in.
	Whitelist *Whitelist
	// OTPExpiration is how long a one-time code stays valid.
	OTPExpiration time.Duration
	// OTPMaxAttempts is how many wrong guesses invalidate a code.
	OTPMaxAttempts int
	// OTPResendInterval is the minimum time between two codes for one email.
	OTPResendInterval time.Duration
	// InvitationExpiration is how long a project invitation stays valid.
	InvitationExpiration time.Duration
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         getEnv("SERVER_HOST", "localhost"),
			Port:         getEnvInt("SERVER_PORT", 8080),
			ReadTimeout:  time.Duration(getEnvInt("SERVER_READ_TIMEOUT", 30)) * time.Second,
			WriteTimeout: time.Duration(getEnvInt("SERVER_WRITE_TIMEOUT", 30)) * time.Second,
			MaxUploadMB:  int64(getEnvInt("MAX_UPLOAD_MB", 20)),
		},
		Database: DatabaseConfig{
			Host:         getEnv("DB_HOST", "localhost"),
			Port:         getEnvInt("DB_PORT", 5432),
			User:         getEnv("DB_USER", "postgres"),
			Password:     getEnv("DB_PASSWORD", "postgres"),
			Database:     getEnv("DB_NAME", "pelokalan"),
			SSLMode:      getEnv("DB_SSLMODE", "disable"),
			MaxOpenConns: getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns: getEnvInt("DB_MAX_IDLE_CONNS", 5),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
			Expiration: time.Duration(getEnvInt("JWT_EXPIRATION_HOURS", 72)) * time.Hour,
		},
		Site: SiteConfig{
			BaseURL:     strings.TrimRight(getEnv("SITE_BASE_URL", "http://localhost:8080"), "/"),
			Name:        getEnv("SITE_NAME", "Pelokalan"),
			DefaultLang: getEnv("DEFAULT_LANG", "id"),
		},
		Mail: MailConfig{
			APIToken:  getEnv("MAILTRAP_API_TOKEN", ""),
			InboxID:   getEnv("MAILTRAP_INBOX_ID", ""),
			FromEmail: getEnv("MAIL_FROM_EMAIL", "noreply@localhost"),
			FromName:  getEnv("MAIL_FROM_NAME", "Pelokalan"),
		},
		Auth: AuthConfig{
			Whitelist:            ParseWhitelist(os.Getenv("EMAIL_WHITELIST")),
			OTPExpiration:        time.Duration(getEnvInt("OTP_EXPIRATION_MINUTES", 10)) * time.Minute,
			OTPMaxAttempts:       getEnvInt("OTP_MAX_ATTEMPTS", 5),
			OTPResendInterval:    time.Duration(getEnvInt("OTP_RESEND_INTERVAL_SECONDS", 20)) * time.Second,
			InvitationExpiration: time.Duration(getEnvInt("INVITATION_EXPIRATION_DAYS", 14)) * 24 * time.Hour,
		},
		AI: AIConfig{
			SecretKey:         getEnv("AI_SECRET_KEY", getEnv("JWT_SECRET", "your-secret-key-change-in-production")),
			AllowPrivateHosts: getEnv("AI_ALLOW_PRIVATE_HOSTS", "false") == "true",
		},
		Glossary: GlossaryConfig{
			FetchInterval:     time.Duration(getEnvInt("GLOSSARY_FETCH_INTERVAL_HOURS", 3)) * time.Hour,
			MaxDownloadMB:     int64(getEnvInt("GLOSSARY_MAX_DOWNLOAD_MB", 100)),
			AllowPrivateHosts: getEnv("GLOSSARY_ALLOW_PRIVATE_HOSTS", "false") == "true",
			ThesaurusURL: getEnv("THESAURUS_URL",
				"https://raw.githubusercontent.com/victoriasovereigne/tesaurus/refs/heads/master/dict.json"),
		},
	}
}

// Whitelist holds the email addresses allowed to use the application.
// Entries are exact addresses; an entry starting with "@" allows a whole domain.
type Whitelist struct {
	emails  map[string]struct{}
	domains map[string]struct{}
}

// ParseWhitelist parses a comma separated list of email addresses
func ParseWhitelist(raw string) *Whitelist {
	w := &Whitelist{emails: map[string]struct{}{}, domains: map[string]struct{}{}}
	for _, item := range strings.Split(raw, ",") {
		item = NormalizeEmail(item)
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "@") {
			if len(item) > 1 {
				w.domains[item[1:]] = struct{}{}
			}
			continue
		}
		w.emails[item] = struct{}{}
	}
	return w
}

// Enabled reports whether access is restricted to a list of addresses.
// An empty whitelist means registration is open to everyone.
func (w *Whitelist) Enabled() bool { return w.Len() > 0 }

// IsAllowed reports whether the email address may register or sign in
func (w *Whitelist) IsAllowed(email string) bool {
	if !w.Enabled() {
		return NormalizeEmail(email) != ""
	}
	email = NormalizeEmail(email)
	if email == "" {
		return false
	}
	if _, ok := w.emails[email]; ok {
		return true
	}
	if at := strings.LastIndex(email, "@"); at >= 0 {
		if _, ok := w.domains[email[at+1:]]; ok {
			return true
		}
	}
	return false
}

// Len returns the number of whitelist entries
func (w *Whitelist) Len() int {
	if w == nil {
		return 0
	}
	return len(w.emails) + len(w.domains)
}

// NormalizeEmail lowercases and trims an email address
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// DSN returns the PostgreSQL connection string
func (c *DatabaseConfig) DSN() string {
	return "host=" + c.Host +
		" port=" + strconv.Itoa(c.Port) +
		" user=" + c.User +
		" password=" + c.Password +
		" dbname=" + c.Database +
		" sslmode=" + c.SSLMode
}

// MigrateURL returns a URL-style DSN for golang-migrate
func (c *DatabaseConfig) MigrateURL() string {
	u := url.URL{
		Scheme:   "pgx5",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Database,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
	return u.String()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
