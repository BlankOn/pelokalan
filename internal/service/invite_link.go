package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// Allowed choices for invite links (days 0 = no expiry, uses 0 = unlimited)
var (
	InviteLinkDays = []int{1, 7, 30, 0}
	InviteLinkUses = []int{0, 1, 5, 10, 25, 100}
)

func allowed(v int, list []int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func newLinkToken() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// InviteLinks lists a project's invite links (owners and managers)
func (s *ProjectService) InviteLinks(ctx context.Context, p *models.Project) ([]*models.InviteLink, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	return s.projects.ListInviteLinks(ctx, p.ID)
}

// CreateInviteLink makes a shareable link that adds whoever opens it (after signing in)
// with the given role.
func (s *ProjectService) CreateInviteLink(ctx context.Context, user *models.User, p *models.Project, role models.ProjectRole, days, maxUses int) (*models.InviteLink, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	if !role.IsInvitable() {
		return nil, ErrInvalidRole
	}
	if !allowed(days, InviteLinkDays) || !allowed(maxUses, InviteLinkUses) {
		return nil, ErrInviteLinkOptions
	}
	token, err := newLinkToken()
	if err != nil {
		return nil, err
	}
	l := &models.InviteLink{ProjectID: p.ID, Token: token, Role: role, CreatedBy: user.ID}
	if days > 0 {
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		l.ExpiresAt = &t
	}
	if maxUses > 0 {
		l.MaxUses = &maxUses
	}
	if err := s.projects.CreateInviteLink(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// DeleteInviteLink revokes a link; it stops working immediately
func (s *ProjectService) DeleteInviteLink(ctx context.Context, p *models.Project, id int64) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	if err := s.projects.DeleteInviteLink(ctx, p.ID, id); errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// InviteLink resolves a link token to the link and its project (with the viewer's role).
// Unknown tokens give ErrInviteLinkInvalid; expired or used-up links are returned so the
// page can say why they no longer work.
func (s *ProjectService) InviteLink(ctx context.Context, user *models.User, token string) (*models.InviteLink, *models.Project, error) {
	if len(token) < 16 || len(token) > 64 {
		return nil, nil, ErrInviteLinkInvalid
	}
	l, err := s.projects.GetInviteLinkByToken(ctx, token)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrInviteLinkInvalid
	} else if err != nil {
		return nil, nil, err
	}
	p, err := s.GetPublic(ctx, user, l.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return l, p, nil
}

// JoinByInviteLink adds the signed-in user to the link's project. Existing members
// are left as they are (their role does not change and the link use is not counted).
func (s *ProjectService) JoinByInviteLink(ctx context.Context, user *models.User, token string) (*models.Project, error) {
	l, p, err := s.InviteLink(ctx, user, token)
	if err != nil {
		return nil, err
	}
	if p.Role.IsMember() {
		return p, nil
	}
	if !l.IsActive() {
		return nil, ErrInviteLinkInvalid
	}
	if err := s.projects.JoinByInviteLink(ctx, l.ID, user.ID); errors.Is(err, repository.ErrNotFound) {
		return nil, ErrInviteLinkInvalid
	} else if err != nil {
		return nil, err
	}
	p.Role = l.Role
	return p, nil
}
