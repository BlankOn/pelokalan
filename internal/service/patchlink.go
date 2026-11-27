package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
)

// Patch links let a member fetch a file's git patch from a terminal (`curl … | git am`) without the
// session cookie. A link names the user, the file and an expiry, signed with the JWT secret. The patch is built when the link is fetched,
// so it always holds the current changes; the user's access is checked again then.

// PatchLinkTTL is how long a patch link works
const PatchLinkTTL = 24 * time.Hour

// PatchLink is what a valid patch link grants
type PatchLink struct {
	User      *models.User
	CatalogID int64
}

func (s *AuthService) patchSig(payload string) string {
	m := hmac.New(sha256.New, s.jwtSecret)
	m.Write([]byte("git-patch|" + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:18])
}

// PatchLinkToken signs a patch link for user: kind "c" (catalog) with its id
func (s *AuthService) PatchLinkToken(user *models.User, kind string, id int64, now time.Time) string {
	payload := fmt.Sprintf("%s%s-%s-%s", kind, strconv.FormatInt(id, 36), strconv.FormatInt(user.ID, 36),
		strconv.FormatInt(now.Add(PatchLinkTTL).Unix(), 36))
	return payload + "." + s.patchSig(payload) // base64url signatures never hold "."
}

// ParsePatchLink checks a patch link token and loads its user (who must still be allowed in)
func (s *AuthService) ParsePatchLink(ctx context.Context, token string, now time.Time) (*PatchLink, error) {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return nil, apperr.ErrInvalidToken
	}
	payload, sig := token[:i], token[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.patchSig(payload))) {
		return nil, apperr.ErrInvalidToken
	}
	parts := strings.Split(payload, "-")
	if len(parts) != 3 || len(parts[0]) < 2 {
		return nil, apperr.ErrInvalidToken
	}
	id, err1 := strconv.ParseInt(parts[0][1:], 36, 64)
	uid, err2 := strconv.ParseInt(parts[1], 36, 64)
	exp, err3 := strconv.ParseInt(parts[2], 36, 64)
	if err1 != nil || err2 != nil || err3 != nil || now.Unix() > exp {
		return nil, apperr.ErrInvalidToken
	}
	user, err := s.users.GetByID(ctx, uid)
	if err != nil {
		return nil, apperr.ErrInvalidToken
	}
	if !s.IsAllowed(user.Email) {
		return nil, apperr.ErrNotWhitelisted
	}
	link := &PatchLink{User: user}
	if parts[0][0] != 'c' {
		return nil, apperr.ErrInvalidToken
	}
	link.CatalogID = id
	return link, nil
}
