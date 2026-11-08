package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pelokalan/internal/config"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

// ProjectService handles projects, members and invitations
type ProjectService struct {
	projects repository.ProjectRepository
	users    repository.UserRepository
	mailer   Mailer
	auth     *AuthService
	cfg      config.AuthConfig
	site     config.SiteConfig
}

// NewProjectService creates a new project service
func NewProjectService(projects repository.ProjectRepository, users repository.UserRepository, mailer Mailer,
	auth *AuthService, cfg config.AuthConfig, site config.SiteConfig) *ProjectService {
	return &ProjectService{projects: projects, users: users, mailer: mailer, auth: auth, cfg: cfg, site: site}
}

// ProjectInput holds editable project fields
type ProjectInput struct {
	Name           string
	Slug           string // empty: derived from the name
	Description    string
	SourceLanguage string
}

// slugRe allows 3-50 lowercase letters, digits and dashes, not at the ends
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}[a-z0-9]$`)

// validSlug checks a project slug; it must contain a letter so it is never
// mistaken for a numeric project id
func validSlug(s string) bool {
	return slugRe.MatchString(s) && !strings.Contains(s, "--") && strings.IndexFunc(s, func(r rune) bool { return r >= 'a' && r <= 'z' }) >= 0
}

// Slugify derives a slug from a project name ("GNOME ID" -> "gnome-id")
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 46 {
		s = strings.Trim(s[:46], "-")
	}
	if !validSlug(s) {
		s = strings.Trim("project-"+s, "-")
		if !validSlug(s) {
			s = "project"
		}
	}
	return s
}

// uniqueSlug returns slug, or slug-2, slug-3… when taken by another project
func (s *ProjectService) uniqueSlug(ctx context.Context, slug string, selfID int64) (string, error) {
	for i := 1; i < 1000; i++ {
		cand := slug
		if i > 1 {
			cand = slug + "-" + strconv.Itoa(i)
		}
		existing, err := s.projects.GetBySlug(ctx, cand)
		if errors.Is(err, repository.ErrNotFound) || (err == nil && existing.ID == selfID) {
			return cand, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", ErrSlugTaken
}

// resolveSlug validates a requested slug, or derives one from the name
func (s *ProjectService) resolveSlug(ctx context.Context, in *ProjectInput, selfID int64) error {
	if in.Slug == "" {
		slug, err := s.uniqueSlug(ctx, Slugify(in.Name), selfID)
		in.Slug = slug
		return err
	}
	if !validSlug(in.Slug) {
		return ErrSlugInvalid
	}
	existing, err := s.projects.GetBySlug(ctx, in.Slug)
	if err == nil && existing.ID != selfID {
		return ErrSlugTaken
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return nil
}

func (in *ProjectInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.Description = strings.TrimSpace(in.Description)
	in.SourceLanguage = strings.TrimSpace(in.SourceLanguage)
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		return ErrProjectNameRequired
	}
	if in.SourceLanguage == "" {
		in.SourceLanguage = "en"
	}
	if !validLanguageCode(in.SourceLanguage) {
		return ErrInvalidLanguage
	}
	return nil
}

// Create creates a project owned by the user
func (s *ProjectService) Create(ctx context.Context, user *models.User, in ProjectInput) (*models.Project, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	if err := s.resolveSlug(ctx, &in, 0); err != nil {
		return nil, err
	}
	p := &models.Project{Name: in.Name, Slug: in.Slug, Description: in.Description, SourceLanguage: in.SourceLanguage, OwnerID: user.ID}
	if err := s.projects.Create(ctx, p); err != nil {
		return nil, err
	}
	p.Role = models.RoleOwner
	return p, nil
}

// ListForUser returns the projects the user belongs to
func (s *ProjectService) ListForUser(ctx context.Context, user *models.User) ([]*models.Project, error) {
	return s.projects.ListForUser(ctx, user.ID)
}

// ListAll returns every project (projects are public); user may be nil
func (s *ProjectService) ListAll(ctx context.Context, user *models.User) ([]*models.Project, error) {
	var uid int64
	if user != nil {
		uid = user.ID
	}
	return s.projects.ListAll(ctx, uid)
}

// GetPublic returns a project for reading. Projects are public; Role is empty
// for visitors and non-members. user may be nil.
func (s *ProjectService) GetPublic(ctx context.Context, user *models.User, id int64) (*models.Project, error) {
	p, err := s.projects.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if user != nil {
		role, err := s.projects.GetMemberRole(ctx, id, user.ID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
		p.Role = role
	}
	return p, nil
}

// GetPublicBySlug is GetPublic for a project address such as "gnome-id"
func (s *ProjectService) GetPublicBySlug(ctx context.Context, user *models.User, slug string) (*models.Project, error) {
	p, err := s.projects.GetBySlug(ctx, strings.ToLower(slug))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return s.GetPublic(ctx, user, p.ID)
}

// Get returns a project with the user's role, or ErrNotFound when the user is not a member
func (s *ProjectService) Get(ctx context.Context, user *models.User, id int64) (*models.Project, error) {
	role, err := s.projects.GetMemberRole(ctx, id, user.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	p, err := s.projects.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	p.Role = role
	return p, nil
}

// Update changes project details (owner or manager)
func (s *ProjectService) Update(ctx context.Context, p *models.Project, in ProjectInput) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	if err := in.clean(); err != nil {
		return err
	}
	if in.Slug == "" {
		in.Slug = p.Slug
	}
	if err := s.resolveSlug(ctx, &in, p.ID); err != nil {
		return err
	}
	p.Name, p.Slug, p.Description, p.SourceLanguage = in.Name, in.Slug, in.Description, in.SourceLanguage
	return s.projects.Update(ctx, p)
}

// Delete removes the project and all its files (owner only)
func (s *ProjectService) Delete(ctx context.Context, p *models.Project) error {
	if !p.Role.IsOwner() {
		return ErrForbidden
	}
	return s.projects.Delete(ctx, p.ID)
}

// Members lists project members
func (s *ProjectService) Members(ctx context.Context, p *models.Project) ([]*models.ProjectMember, error) {
	return s.projects.ListMembers(ctx, p.ID)
}

// PendingInvitations lists open invitations of a project
func (s *ProjectService) PendingInvitations(ctx context.Context, p *models.Project) ([]*models.Invitation, error) {
	return s.projects.ListPendingInvitationsForProject(ctx, p.ID)
}

// Invite asks a whitelisted email address to join the project
func (s *ProjectService) Invite(ctx context.Context, inviter *models.User, p *models.Project, email string, role models.ProjectRole, lang string) (*models.Invitation, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	if !role.IsInvitable() || (role == models.RoleManager && !p.Role.CanManage()) {
		return nil, ErrInvalidRole
	}
	email, err := normalizeAndCheck(email)
	if err != nil {
		return nil, err
	}
	if !s.auth.IsAllowed(email) {
		return nil, ErrNotWhitelisted
	}
	if u, err := s.users.GetByEmail(ctx, email); err == nil {
		if _, err := s.projects.GetMemberRole(ctx, p.ID, u.ID); err == nil {
			return nil, ErrAlreadyMember
		}
	}
	if existing, err := s.projects.GetPendingInvitation(ctx, p.ID, email); err == nil {
		if !existing.IsExpired() {
			return nil, ErrAlreadyInvited
		}
		if err := s.projects.DeleteInvitation(ctx, existing.ID); err != nil {
			return nil, err
		}
	}

	inv := &models.Invitation{
		ProjectID: p.ID,
		Email:     email,
		Role:      role,
		InvitedBy: inviter.ID,
		ExpiresAt: time.Now().Add(s.cfg.InvitationExpiration),
	}
	if err := s.projects.CreateInvitation(ctx, inv); err != nil {
		return nil, err
	}
	inv.ProjectName = p.Name
	inv.InviterName = inviter.DisplayName()

	// Write the email in the invitee's language when they already have an account
	if u, err := s.users.GetByEmail(ctx, email); err == nil {
		lang = u.Lang
	}
	link := s.site.BaseURL + "/invitations"
	roleName := i18n.T(lang, "role."+string(role))
	subject := i18n.T(lang, "mail.invite_subject", inv.InviterName, p.Name)
	text := i18n.T(lang, "mail.invite_body", inv.InviterName, p.Name, roleName, link, s.site.Name)
	htmlBody := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px"><p>%s</p>
<p><a href="%s" style="display:inline-block;background:#2f6fde;color:#fff;padding:10px 18px;border-radius:6px;text-decoration:none">%s</a></p>
<p style="color:#666">%s</p></div>`,
		html.EscapeString(i18n.T(lang, "mail.invite_intro", inv.InviterName, p.Name, roleName)),
		link, html.EscapeString(i18n.T(lang, "mail.invite_button")),
		html.EscapeString(i18n.T(lang, "mail.invite_footer", s.site.Name)))
	if err := s.mailer.Send(ctx, &Message{To: email, Subject: subject, Text: text, HTML: htmlBody, Category: "invitation"}); err != nil {
		// The invitation stays valid and is shown after sign in, so do not fail.
		log.Printf("failed to send invitation email to %s: %v", email, err)
	}
	return inv, nil
}

// CancelInvitation withdraws a pending invitation
func (s *ProjectService) CancelInvitation(ctx context.Context, p *models.Project, invitationID int64) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	inv, err := s.projects.GetInvitation(ctx, invitationID)
	if err != nil || inv.ProjectID != p.ID {
		return ErrNotFound
	}
	return s.projects.DeleteInvitation(ctx, inv.ID)
}

// InvitationsForUser lists open invitations addressed to the user
func (s *ProjectService) InvitationsForUser(ctx context.Context, user *models.User) ([]*models.Invitation, error) {
	return s.projects.ListPendingInvitationsForEmail(ctx, user.Email)
}

func (s *ProjectService) ownInvitation(ctx context.Context, user *models.User, id int64) (*models.Invitation, error) {
	inv, err := s.projects.GetInvitation(ctx, id)
	if err != nil || inv.Email != user.Email || inv.AcceptedAt != nil || inv.DeclinedAt != nil || inv.IsExpired() {
		return nil, ErrInvitationInvalid
	}
	return inv, nil
}

// AcceptInvitation adds the user to the invited project
func (s *ProjectService) AcceptInvitation(ctx context.Context, user *models.User, id int64) (*models.Invitation, error) {
	inv, err := s.ownInvitation(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AddMember(ctx, inv.ProjectID, user.ID, inv.Role); err != nil {
		return nil, err
	}
	return inv, s.projects.MarkInvitationAccepted(ctx, inv.ID)
}

// DeclineInvitation rejects an invitation
func (s *ProjectService) DeclineInvitation(ctx context.Context, user *models.User, id int64) error {
	inv, err := s.ownInvitation(ctx, user, id)
	if err != nil {
		return err
	}
	return s.projects.MarkInvitationDeclined(ctx, inv.ID)
}

// ChangeRole updates a member's role
func (s *ProjectService) ChangeRole(ctx context.Context, p *models.Project, userID int64, role models.ProjectRole) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	if !role.IsInvitable() {
		return ErrInvalidRole
	}
	current, err := s.projects.GetMemberRole(ctx, p.ID, userID)
	if err != nil {
		return ErrNotFound
	}
	if current == models.RoleOwner {
		return ErrCannotChangeOwner
	}
	return s.projects.UpdateMemberRole(ctx, p.ID, userID, role)
}

// RemoveMember removes a member. Members may always remove themselves (leave),
// except the owner.
func (s *ProjectService) RemoveMember(ctx context.Context, actor *models.User, p *models.Project, userID int64) error {
	if userID != actor.ID && !p.Role.CanManage() {
		return ErrForbidden
	}
	current, err := s.projects.GetMemberRole(ctx, p.ID, userID)
	if err != nil {
		return ErrNotFound
	}
	if current == models.RoleOwner {
		if userID == actor.ID {
			return ErrCannotRemoveSelfOnly
		}
		return ErrCannotChangeOwner
	}
	return s.projects.RemoveMember(ctx, p.ID, userID)
}

func validLanguageCode(code string) bool {
	if len(code) < 2 || len(code) > 12 {
		return false
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-' || r == '@' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
