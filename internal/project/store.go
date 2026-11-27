package project

import (
	"context"

	"pelokalan/internal/models"
)

// ProjectRepository defines methods for project, member and invitation data access
type Repository interface {
	Create(ctx context.Context, p *models.Project) error
	GetByID(ctx context.Context, id int64) (*models.Project, error)
	GetBySlug(ctx context.Context, slug string) (*models.Project, error)
	ListForUser(ctx context.Context, userID int64) ([]*models.Project, error)
	// ListAll returns every project; Role is filled for projects the user belongs to (userID 0 = visitor)
	ListAll(ctx context.Context, userID int64) ([]*models.Project, error)
	Update(ctx context.Context, p *models.Project) error
	Delete(ctx context.Context, id int64) error

	GetMemberRole(ctx context.Context, projectID, userID int64) (models.ProjectRole, error)
	ListMembers(ctx context.Context, projectID int64) ([]*models.ProjectMember, error)
	AddMember(ctx context.Context, projectID, userID int64, role models.ProjectRole) error
	UpdateMemberRole(ctx context.Context, projectID, userID int64, role models.ProjectRole) error
	RemoveMember(ctx context.Context, projectID, userID int64) error

	CreateInvitation(ctx context.Context, inv *models.Invitation) error
	GetInvitation(ctx context.Context, id int64) (*models.Invitation, error)
	GetPendingInvitation(ctx context.Context, projectID int64, email string) (*models.Invitation, error)
	ListPendingInvitationsForProject(ctx context.Context, projectID int64) ([]*models.Invitation, error)
	ListPendingInvitationsForEmail(ctx context.Context, email string) ([]*models.Invitation, error)
	MarkInvitationAccepted(ctx context.Context, id int64) error
	MarkInvitationDeclined(ctx context.Context, id int64) error
	DeleteInvitation(ctx context.Context, id int64) error
	CreateInviteLink(ctx context.Context, l *models.InviteLink) error
	GetInviteLinkByToken(ctx context.Context, token string) (*models.InviteLink, error)
	ListInviteLinks(ctx context.Context, projectID int64) ([]*models.InviteLink, error)
	DeleteInviteLink(ctx context.Context, projectID, id int64) error
	// JoinByInviteLink counts a use of a still valid link and adds the user as a member,
	// in one transaction. It returns ErrNotFound when the link is no longer usable.
	JoinByInviteLink(ctx context.Context, linkID, userID int64) error
}

// NotificationRepository stores members' email choices per project
type NotificationRepository interface {
	// Settings returns the choices of the given users (missing users have the defaults)
	Settings(ctx context.Context, projectID int64, userIDs []int64) (map[int64]*models.NotificationSettings, error)
	Save(ctx context.Context, userID, projectID int64, s *models.NotificationSettings) error
}
