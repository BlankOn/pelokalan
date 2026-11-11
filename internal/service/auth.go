package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/mail"
	"pelokalan/internal/po"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"pelokalan/internal/config"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// AuthService handles passwordless registration and sign in with one-time codes
type AuthService struct {
	users     repository.UserRepository
	otps      repository.OTPRepository
	mailer    Mailer
	cfg       config.AuthConfig
	site      config.SiteConfig
	jwtSecret []byte
	jwtExpiry time.Duration
	activity  *ActivityService
}

// Claims represents JWT claims
type Claims struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// SetActivity makes the service record sign-ups in the activity log
func (s *AuthService) SetActivity(a *ActivityService) { s.activity = a }

// NewAuthService creates a new auth service
func NewAuthService(users repository.UserRepository, otps repository.OTPRepository, mailer Mailer,
	cfg config.AuthConfig, site config.SiteConfig, jwtCfg config.JWTConfig) *AuthService {
	return &AuthService{
		users:     users,
		otps:      otps,
		mailer:    mailer,
		cfg:       cfg,
		site:      site,
		jwtSecret: []byte(jwtCfg.Secret),
		jwtExpiry: jwtCfg.Expiration,
	}
}

// ResendWait returns how long the address must wait before another code can be
// sent (0 when it may ask now)
func (s *AuthService) ResendWait(ctx context.Context, email string) time.Duration {
	prev, err := s.otps.LatestForEmail(ctx, config.NormalizeEmail(email))
	if err != nil {
		return 0
	}
	if wait := s.cfg.OTPResendInterval - time.Since(prev.CreatedAt); wait > 0 {
		return wait
	}
	return 0
}

// WhitelistEnabled reports whether sign-up is limited to the email whitelist
func (s *AuthService) WhitelistEnabled() bool { return s.cfg.Whitelist.Enabled() }

// IsAllowed reports whether the email is on the whitelist (always true when no whitelist is set)
func (s *AuthService) IsAllowed(email string) bool { return s.cfg.Whitelist.IsAllowed(email) }

// TokenExpiry returns how long session tokens are valid
func (s *AuthService) TokenExpiry() time.Duration { return s.jwtExpiry }

// OTPExpiry returns how long one-time codes are valid
func (s *AuthService) OTPExpiry() time.Duration { return s.cfg.OTPExpiration }

func normalizeAndCheck(email string) (string, error) {
	email = config.NormalizeEmail(email)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email, "@") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

// usernameRe allows 3-30 lowercase letters, digits, dots, underscores and
// dashes, starting and ending with a letter or digit
var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,28}[a-z0-9]$`)

// reservedUsernames would clash with application URLs or be misleading
var reservedUsernames = map[string]bool{
	"glossary": true, "members": true, "settings": true, "files": true, "upload": true, "translations": true,
	"update-all": true, "invitations": true, "ai": true, "delete": true, "po": true, "api": true, "static": true,
	"login": true, "logout": true, "register": true, "verify": true, "profile": true, "projects": true,
	"catalogs": true, "lang": true, "join": true, "invite-links": true, "root": true, "system": true, "null": true, "undefined": true, "me": true,
}

// NormalizeUsername lowercases and trims a username ("@Ani " -> "ani")
func NormalizeUsername(u string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(u), "@"))
}

func validUsername(u string) error {
	if !usernameRe.MatchString(u) || strings.Contains(u, "..") {
		return ErrUsernameInvalid
	}
	if reservedUsernames[u] {
		return ErrUsernameReserved
	}
	return nil
}

func (s *AuthService) usernameAvailable(ctx context.Context, u string, selfID int64) error {
	if err := validUsername(u); err != nil {
		return err
	}
	existing, err := s.users.GetByUsername(ctx, u)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil
	case err != nil:
		return err
	case existing.ID != selfID:
		return ErrUsernameTaken
	}
	return nil
}

// StartRegistration validates the request and emails a registration code
func (s *AuthService) StartRegistration(ctx context.Context, email, username, name, lang string) (string, error) {
	email, err := normalizeAndCheck(email)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return email, ErrNameRequired
	}
	username = NormalizeUsername(username)
	if err := s.usernameAvailable(ctx, username, 0); err != nil {
		return email, err
	}
	if !s.IsAllowed(email) {
		return email, ErrNotWhitelisted
	}
	if _, err := s.users.GetByEmail(ctx, email); err == nil {
		return email, ErrUserExists
	} else if !errors.Is(err, repository.ErrNotFound) {
		return email, err
	}
	return email, s.sendCode(ctx, email, models.OTPPurposeRegister, name, username, lang)
}

// StartLogin validates the request and emails a sign-in code
func (s *AuthService) StartLogin(ctx context.Context, email, lang string) (string, error) {
	email, err := normalizeAndCheck(email)
	if err != nil {
		return "", err
	}
	if !s.IsAllowed(email) {
		return email, ErrNotWhitelisted
	}
	if _, err := s.users.GetByEmail(ctx, email); errors.Is(err, repository.ErrNotFound) {
		return email, ErrUserNotFound
	} else if err != nil {
		return email, err
	}
	return email, s.sendCode(ctx, email, models.OTPPurposeLogin, "", "", lang)
}

// ResendCode sends a fresh code for a pending registration or sign in
func (s *AuthService) ResendCode(ctx context.Context, email, purpose, lang string) error {
	email = config.NormalizeEmail(email)
	if !s.IsAllowed(email) {
		return ErrNotWhitelisted
	}
	name, username := "", ""
	if purpose == models.OTPPurposeRegister {
		prev, err := s.otps.GetLatest(ctx, email, purpose)
		if err != nil {
			return ErrOTPExpired
		}
		name, username = prev.Name, prev.Username
		if _, err := s.users.GetByEmail(ctx, email); err == nil {
			return ErrUserExists
		}
	} else {
		if _, err := s.users.GetByEmail(ctx, email); err != nil {
			return ErrUserNotFound
		}
	}
	return s.sendCode(ctx, email, purpose, name, username, lang)
}

func (s *AuthService) sendCode(ctx context.Context, email, purpose, name, username, lang string) error {
	// the wait is per email address, shared by sign-in and registration
	if wait := s.ResendWait(ctx, email); wait > 0 {
		return userErr("err.otp_too_soon", int(wait.Seconds()+0.999))
	}

	code, err := randomDigits(6)
	if err != nil {
		return err
	}
	if err := s.otps.InvalidateAll(ctx, email); err != nil {
		return err
	}
	otp := &models.OTPCode{
		Email:     email,
		Purpose:   purpose,
		CodeHash:  s.hashCode(email, code),
		Name:      name,
		Username:  username,
		ExpiresAt: time.Now().Add(s.cfg.OTPExpiration),
	}
	if err := s.otps.Create(ctx, otp); err != nil {
		return err
	}

	minutes := int(s.cfg.OTPExpiration.Minutes())
	subjectKey := "mail.otp_login_subject"
	if purpose == models.OTPPurposeRegister {
		subjectKey = "mail.otp_register_subject"
	}
	subject := i18n.T(lang, subjectKey, s.site.Name)
	text := i18n.T(lang, "mail.otp_body", code, minutes, s.site.Name)
	html := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px">
<p>%s</p>
<p style="font-size:32px;letter-spacing:8px;font-weight:bold;font-family:monospace">%s</p>
<p style="color:#666">%s</p></div>`,
		i18n.T(lang, "mail.otp_intro", s.site.Name), code, i18n.T(lang, "mail.otp_footer", minutes))

	if err := s.mailer.Send(ctx, &Message{To: email, Subject: subject, Text: text, HTML: html, Category: "otp"}); err != nil {
		log.Printf("failed to send code to %s: %v", email, err)
		return userErr("err.mail_failed")
	}
	return nil
}

// Verify checks a code and returns the signed-in user, creating the account
// for registration codes.
func (s *AuthService) Verify(ctx context.Context, email, purpose, code, lang string) (*models.User, error) {
	email = config.NormalizeEmail(email)
	if !s.IsAllowed(email) {
		return nil, ErrNotWhitelisted
	}
	code = strings.Join(strings.Fields(code), "")

	otp, err := s.otps.GetLatest(ctx, email, purpose)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrOTPInvalid
	} else if err != nil {
		return nil, err
	}
	if otp.UsedAt != nil || otp.IsExpired() {
		return nil, ErrOTPExpired
	}
	if otp.Attempts >= s.cfg.OTPMaxAttempts {
		return nil, ErrOTPTooManyAttempts
	}
	if !hmac.Equal([]byte(otp.CodeHash), []byte(s.hashCode(email, code))) {
		if err := s.otps.IncrementAttempts(ctx, otp.ID); err != nil {
			return nil, err
		}
		if otp.Attempts+1 >= s.cfg.OTPMaxAttempts {
			return nil, ErrOTPTooManyAttempts
		}
		return nil, ErrOTPInvalid
	}
	if err := s.otps.MarkUsed(ctx, otp.ID); err != nil {
		return nil, ErrOTPExpired
	}

	user, err := s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
	case errors.Is(err, repository.ErrNotFound) && purpose == models.OTPPurposeRegister:
		user = &models.User{Email: email, Username: otp.Username, Name: otp.Name, Lang: i18n.Normalize(lang)}
		if user.Username == "" { // registration started before usernames existed
			return nil, ErrOTPExpired
		}
		if err := s.usernameAvailable(ctx, user.Username, 0); err != nil {
			return nil, err
		}
		if err := s.users.Create(ctx, user); err != nil {
			return nil, err
		}
		s.activity.Record(ctx, &models.ActivityEvent{Kind: models.ActivityUserRegistered, UserID: &user.ID})
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrUserNotFound
	default:
		return nil, err
	}
	_ = s.users.TouchLogin(ctx, user.ID)
	return user, nil
}

func (s *AuthService) hashCode(email, code string) string {
	m := hmac.New(sha256.New, s.jwtSecret)
	m.Write([]byte(email + "\x00" + code))
	return hex.EncodeToString(m.Sum(nil))
}

func randomDigits(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String(), nil
}

// GenerateToken creates a JWT token for a user
func (s *AuthService) GenerateToken(user *models.User) (string, error) {
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

// Authenticate validates a token and returns the user. Users removed from the
// whitelist lose access immediately.
func (s *AuthService) Authenticate(ctx context.Context, tokenString string) (*models.User, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if !s.IsAllowed(user.Email) {
		return nil, ErrNotWhitelisted
	}
	return user, nil
}

// UpdateProfile changes the user's name and UI language
func (s *AuthService) UpdateProfile(ctx context.Context, user *models.User, username, name, lang string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return ErrNameRequired
	}
	username = NormalizeUsername(username)
	if err := s.usernameAvailable(ctx, username, user.ID); err != nil {
		return err
	}
	user.Username = username
	user.Name = name
	user.Lang = i18n.Normalize(lang)
	return s.users.Update(ctx, user)
}

// UpdateIdentity changes how the user is credited in PO headers
func (s *AuthService) UpdateIdentity(ctx context.Context, user *models.User, name, email, team string) error {
	name = strings.TrimSpace(name)
	team = strings.TrimSpace(team)
	email = strings.TrimSpace(email)
	if email != "" {
		addr, err := mail.ParseAddress(email)
		if err != nil || addr.Address != email {
			return ErrInvalidEmail
		}
	}
	if len([]rune(name)) > 100 || len([]rune(team)) > 200 || strings.ContainsAny(name+team, "\n\r") {
		return userErr("err.identity_invalid")
	}
	user.TranslatorName, user.TranslatorEmail, user.LanguageTeam = name, email, team
	return s.users.Update(ctx, user)
}

// EditorStatuses are the statuses Validasi can show besides problem entries
var EditorStatuses = []string{"untranslated", "fuzzy"}

// UpdateEditorChecks stores which editor checks are on (issue codes from po.Checks and
// EditorStatuses); everything not in on is switched off for every file of the account
func (s *AuthService) UpdateEditorChecks(ctx context.Context, user *models.User, on []string) error {
	var off []string
	for _, code := range append(append([]string(nil), po.Checks...), EditorStatuses...) {
		if !slices.Contains(on, code) {
			off = append(off, code)
		}
	}
	user.ChecksOff = off
	return s.users.Update(ctx, user)
}

// SetLang stores the user's preferred UI language
func (s *AuthService) SetLang(ctx context.Context, user *models.User, lang string) error {
	user.Lang = i18n.Normalize(lang)
	return s.users.Update(ctx, user)
}

// Cleanup removes stale one-time codes
func (s *AuthService) Cleanup(ctx context.Context) error { return s.otps.CleanupExpired(ctx) }
