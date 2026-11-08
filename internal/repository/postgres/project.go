package postgres

import (
	"context"
	"database/sql"
	"errors"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type projectRepository struct {
	db *sql.DB
}

// NewProjectRepository creates a new PostgreSQL project repository
func NewProjectRepository(db *sql.DB) repository.ProjectRepository {
	return &projectRepository{db: db}
}

func (r *projectRepository) Create(ctx context.Context, p *models.Project) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO projects (name, slug, description, source_language, owner_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		p.Name, p.Slug, p.Description, p.SourceLanguage, p.OwnerID,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`,
		p.ID, p.OwnerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *projectRepository) GetByID(ctx context.Context, id int64) (*models.Project, error) {
	p := &models.Project{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, slug, description, source_language, owner_id, created_at, updated_at,
		        EXISTS (SELECT 1 FROM project_ai_settings s WHERE s.project_id = projects.id AND s.api_key_enc <> '')
		 FROM projects WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.SourceLanguage, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt, &p.AIEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return p, err
}

func (r *projectRepository) GetBySlug(ctx context.Context, slug string) (*models.Project, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *projectRepository) ListForUser(ctx context.Context, userID int64) ([]*models.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.slug, p.description, p.source_language, p.owner_id, p.created_at, p.updated_at, m.role,
		        (SELECT COUNT(*) FROM project_members pm WHERE pm.project_id = p.id),
		        (SELECT COUNT(*) FROM catalogs c WHERE c.project_id = p.id AND NOT c.is_template)
		 FROM projects p
		 JOIN project_members m ON m.project_id = p.id AND m.user_id = $1
		 ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Project
	for rows.Next() {
		p := &models.Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.SourceLanguage, &p.OwnerID,
			&p.CreatedAt, &p.UpdatedAt, &p.Role, &p.MemberCount, &p.CatalogCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectRepository) ListAll(ctx context.Context, userID int64) ([]*models.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.slug, p.description, p.source_language, p.owner_id, p.created_at, p.updated_at,
		        COALESCE(m.role, ''),
		        (SELECT COUNT(*) FROM project_members pm WHERE pm.project_id = p.id),
		        (SELECT COUNT(DISTINCT i.version_id) FROM glossary_imports i WHERE i.project_id = p.id),
		        (SELECT COALESCE(MAX(i.term_count), 0) FROM glossary_imports i WHERE i.project_id = p.id),
		        EXISTS (SELECT 1 FROM project_ai_settings s WHERE s.project_id = p.id AND s.api_key_enc <> ''),
		        (SELECT COUNT(*) FROM catalogs c WHERE c.project_id = p.id AND NOT c.is_template)
		 FROM projects p
		 LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = $1
		 ORDER BY (m.role IS NULL), p.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Project
	for rows.Next() {
		p := &models.Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.SourceLanguage, &p.OwnerID,
			&p.CreatedAt, &p.UpdatedAt, &p.Role, &p.MemberCount, &p.VersionCount, &p.TermCount, &p.AIEnabled, &p.CatalogCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectRepository) Update(ctx context.Context, p *models.Project) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = $2, description = $3, source_language = $4, slug = $5, updated_at = NOW() WHERE id = $1`,
		p.ID, p.Name, p.Description, p.SourceLanguage, p.Slug)
	return err
}

func (r *projectRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

func (r *projectRepository) GetMemberRole(ctx context.Context, projectID, userID int64) (models.ProjectRole, error) {
	var role models.ProjectRole
	err := r.db.QueryRowContext(ctx,
		`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", repository.ErrNotFound
	}
	return role, err
}

func (r *projectRepository) ListMembers(ctx context.Context, projectID int64) ([]*models.ProjectMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.project_id, m.role, m.created_at, u.id, u.email, u.username, u.name, u.lang, u.last_login_at, u.created_at, u.updated_at
		 FROM project_members m JOIN users u ON u.id = m.user_id
		 WHERE m.project_id = $1
		 ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'manager' THEN 1 ELSE 2 END, u.name, u.email`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.ProjectMember
	for rows.Next() {
		m := &models.ProjectMember{}
		u := &m.User
		if err := rows.Scan(&m.ProjectID, &m.Role, &m.CreatedAt,
			&u.ID, &u.Email, &u.Username, &u.Name, &u.Lang, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *projectRepository) AddMember(ctx context.Context, projectID, userID int64, role models.ProjectRole) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, userID, role)
	return err
}

func (r *projectRepository) UpdateMemberRole(ctx context.Context, projectID, userID int64, role models.ProjectRole) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE project_members SET role = $3 WHERE project_id = $1 AND user_id = $2`, projectID, userID, role)
	return err
}

func (r *projectRepository) RemoveMember(ctx context.Context, projectID, userID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
	return err
}

const invitationColumns = `i.id, i.project_id, i.email, i.role, i.invited_by, i.expires_at, i.accepted_at, i.declined_at, i.created_at,
	p.name, COALESCE(NULLIF(u.name, ''), u.email)`

const invitationFrom = ` FROM project_invitations i
	JOIN projects p ON p.id = i.project_id
	JOIN users u ON u.id = i.invited_by `

func scanInvitation(row interface{ Scan(...any) error }) (*models.Invitation, error) {
	inv := &models.Invitation{}
	err := row.Scan(&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.InvitedBy, &inv.ExpiresAt,
		&inv.AcceptedAt, &inv.DeclinedAt, &inv.CreatedAt, &inv.ProjectName, &inv.InviterName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return inv, err
}

func (r *projectRepository) listInvitations(ctx context.Context, where string, arg any) ([]*models.Invitation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+invitationColumns+invitationFrom+where+` ORDER BY i.created_at DESC`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (r *projectRepository) CreateInvitation(ctx context.Context, inv *models.Invitation) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO project_invitations (project_id, email, role, invited_by, expires_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		inv.ProjectID, inv.Email, inv.Role, inv.InvitedBy, inv.ExpiresAt,
	).Scan(&inv.ID, &inv.CreatedAt)
}

func (r *projectRepository) GetInvitation(ctx context.Context, id int64) (*models.Invitation, error) {
	return scanInvitation(r.db.QueryRowContext(ctx, `SELECT `+invitationColumns+invitationFrom+` WHERE i.id = $1`, id))
}

func (r *projectRepository) GetPendingInvitation(ctx context.Context, projectID int64, email string) (*models.Invitation, error) {
	return scanInvitation(r.db.QueryRowContext(ctx, `SELECT `+invitationColumns+invitationFrom+
		` WHERE i.project_id = $1 AND i.email = $2 AND i.accepted_at IS NULL AND i.declined_at IS NULL`, projectID, email))
}

func (r *projectRepository) ListPendingInvitationsForProject(ctx context.Context, projectID int64) ([]*models.Invitation, error) {
	return r.listInvitations(ctx, ` WHERE i.project_id = $1 AND i.accepted_at IS NULL AND i.declined_at IS NULL`, projectID)
}

func (r *projectRepository) ListPendingInvitationsForEmail(ctx context.Context, email string) ([]*models.Invitation, error) {
	return r.listInvitations(ctx, ` WHERE i.email = $1 AND i.accepted_at IS NULL AND i.declined_at IS NULL AND i.expires_at > NOW()`, email)
}

func (r *projectRepository) MarkInvitationAccepted(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE project_invitations SET accepted_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *projectRepository) MarkInvitationDeclined(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE project_invitations SET declined_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *projectRepository) DeleteInvitation(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM project_invitations WHERE id = $1`, id)
	return err
}

const inviteLinkColumns = `l.id, l.project_id, l.token, l.role, l.created_by, l.expires_at, l.max_uses, l.uses, l.created_at,
	COALESCE(NULLIF(u.name, ''), u.username)`

const inviteLinkFrom = ` FROM project_invite_links l JOIN users u ON u.id = l.created_by `

func scanInviteLink(row interface{ Scan(...any) error }) (*models.InviteLink, error) {
	l := &models.InviteLink{}
	var maxUses sql.NullInt64
	err := row.Scan(&l.ID, &l.ProjectID, &l.Token, &l.Role, &l.CreatedBy, &l.ExpiresAt, &maxUses, &l.Uses, &l.CreatedAt, &l.CreatorName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if maxUses.Valid {
		n := int(maxUses.Int64)
		l.MaxUses = &n
	}
	return l, err
}

func (r *projectRepository) CreateInviteLink(ctx context.Context, l *models.InviteLink) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO project_invite_links (project_id, token, role, created_by, expires_at, max_uses)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		l.ProjectID, l.Token, l.Role, l.CreatedBy, l.ExpiresAt, l.MaxUses,
	).Scan(&l.ID, &l.CreatedAt)
}

func (r *projectRepository) GetInviteLinkByToken(ctx context.Context, token string) (*models.InviteLink, error) {
	return scanInviteLink(r.db.QueryRowContext(ctx, `SELECT `+inviteLinkColumns+inviteLinkFrom+` WHERE l.token = $1`, token))
}

func (r *projectRepository) ListInviteLinks(ctx context.Context, projectID int64) ([]*models.InviteLink, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+inviteLinkColumns+inviteLinkFrom+
		` WHERE l.project_id = $1 ORDER BY l.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.InviteLink
	for rows.Next() {
		l, err := scanInviteLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *projectRepository) DeleteInviteLink(ctx context.Context, projectID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM project_invite_links WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (r *projectRepository) JoinByInviteLink(ctx context.Context, linkID, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var projectID int64
	var role string
	// the conditions and the counter update happen in one statement, so a limited
	// link cannot be used more often than allowed by concurrent joins
	err = tx.QueryRowContext(ctx,
		`UPDATE project_invite_links SET uses = uses + 1
		 WHERE id = $1 AND (expires_at IS NULL OR expires_at > NOW()) AND (max_uses IS NULL OR uses < max_uses)
		 RETURNING project_id, role`, linkID).Scan(&projectID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, userID, role); err != nil {
		return err
	}
	// an email invitation to the same project is fulfilled as well
	if _, err := tx.ExecContext(ctx,
		`UPDATE project_invitations SET accepted_at = NOW()
		 WHERE project_id = $1 AND accepted_at IS NULL AND declined_at IS NULL
		   AND email = (SELECT email FROM users WHERE id = $2)`, projectID, userID); err != nil {
		return err
	}
	return tx.Commit()
}
