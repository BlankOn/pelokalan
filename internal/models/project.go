package models

import "time"

// ProjectRole is a member's role inside a project
type ProjectRole string

const (
	RoleOwner      ProjectRole = "owner"
	RoleManager    ProjectRole = "manager"
	RoleTranslator ProjectRole = "translator"
)

// IsValid reports whether the role is known
func (r ProjectRole) IsValid() bool {
	return r == RoleOwner || r == RoleManager || r == RoleTranslator
}

// IsInvitable reports whether the role can be given through an invitation
func (r ProjectRole) IsInvitable() bool { return r == RoleManager || r == RoleTranslator }

// CanManage reports whether the role may manage files and members
func (r ProjectRole) CanManage() bool { return r == RoleOwner || r == RoleManager }

// IsMember reports whether the viewer belongs to the project (an empty role means a public visitor)
func (r ProjectRole) IsMember() bool { return r.IsValid() }

// IsOwner reports whether the role is owner
func (r ProjectRole) IsOwner() bool { return r == RoleOwner }

// Project groups translation files of one piece of software
type Project struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	SourceLanguage string `json:"source_language"`
	// Language is what the project translates into; files in other languages are refused ("" = not chosen)
	Language string `json:"language"`
	// Open projects can be joined by any signed-in user (as translator); private ones need an invitation
	Open      bool      `json:"open"`
	OwnerID   int64     `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Filled by listing queries
	Role         ProjectRole `json:"role,omitempty"`
	MemberCount  int         `json:"member_count,omitempty"`
	CatalogCount int         `json:"catalog_count,omitempty"`
	VersionCount int         `json:"version_count,omitempty"`
	TermCount    int         `json:"term_count,omitempty"`
	AIEnabled    bool        `json:"ai_enabled,omitempty"` // an LLM API key is configured
}

// Path is the project's address, e.g. /projects/gnome-id
func (p *Project) Path() string { return "/projects/" + p.Slug }

// ProjectMember is a user's membership in a project
type ProjectMember struct {
	ProjectID int64
	User      User
	Role      ProjectRole
	CreatedAt time.Time
}

// Invitation asks someone to join a project
type Invitation struct {
	ID         int64
	ProjectID  int64
	Email      string
	Role       ProjectRole
	InvitedBy  int64
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	DeclinedAt *time.Time
	CreatedAt  time.Time

	// Filled by listing queries
	ProjectName string
	InviterName string
}

// IsExpired reports whether the invitation can no longer be accepted
func (i *Invitation) IsExpired() bool { return time.Now().After(i.ExpiresAt) }

// InviteLink lets anyone with the link join a project after signing in
type InviteLink struct {
	ID        int64
	ProjectID int64
	Token     string
	Role      ProjectRole
	CreatedBy int64
	ExpiresAt *time.Time
	MaxUses   *int
	Uses      int
	CreatedAt time.Time

	// Filled by queries
	CreatorName string
}

// IsExpired reports whether the link's time limit has passed
func (l *InviteLink) IsExpired() bool { return l.ExpiresAt != nil && time.Now().After(*l.ExpiresAt) }

// IsUsedUp reports whether the link reached its use limit
func (l *InviteLink) IsUsedUp() bool { return l.MaxUses != nil && l.Uses >= *l.MaxUses }

// IsActive reports whether the link can still be used to join
func (l *InviteLink) IsActive() bool { return !l.IsExpired() && !l.IsUsedUp() }

// Path is the link's address on this site
func (l *InviteLink) Path() string { return "/join/" + l.Token }

// Limit is the maximum number of uses (0 = unlimited)
func (l *InviteLink) Limit() int {
	if l.MaxUses == nil {
		return 0
	}
	return *l.MaxUses
}
