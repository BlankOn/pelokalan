package account

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/store"
)

type userRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new PostgreSQL user repository
func NewUserRepository(db *sql.DB) UserRepository {
	return &userRepository{db: db}
}

const userColumns = `id, email, username, name, lang, translator_name, translator_email, language_team, editor_checks_off, badge_emails_off, bio, links, avatar, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	u := &models.User{}
	var off, links string
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.Name, &u.Lang, &u.TranslatorName, &u.TranslatorEmail, &u.LanguageTeam,
		&off, &u.BadgeEmailsOff, &u.Bio, &links, &u.Avatar, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if links != "" && links != "{}" {
		_ = json.Unmarshal([]byte(links), &u.Links)
	}
	if off != "" {
		u.ChecksOff = strings.Split(off, ",")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return u, err
}

func (r *userRepository) Create(ctx context.Context, u *models.User) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO users (email, username, name, lang) VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at, updated_at`,
		u.Email, u.Username, u.Name, u.Lang,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (r *userRepository) GetByID(ctx context.Context, id int64) (*models.User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

func (r *userRepository) Update(ctx context.Context, u *models.User) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET name = $2, lang = $3, translator_name = $4, translator_email = $5, language_team = $6,
		        username = $7, editor_checks_off = $8, updated_at = NOW() WHERE id = $1`,
		u.ID, u.Name, u.Lang, u.TranslatorName, u.TranslatorEmail, u.LanguageTeam, u.Username, strings.Join(u.ChecksOff, ","))
	return err
}

func (r *userRepository) SetAvatar(ctx context.Context, id int64, key string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET avatar = $2, updated_at = NOW() WHERE id = $1`, id, key)
	return err
}

func (r *userRepository) AvatarInUse(ctx context.Context, key string) bool {
	var ok bool
	_ = r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE avatar = $1 AND avatar <> '')`, key).Scan(&ok)
	return ok
}

func (r *userRepository) SetPublicProfile(ctx context.Context, id int64, bio string, links map[string]string) error {
	data, err := json.Marshal(links)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE users SET bio = $2, links = $3, updated_at = NOW() WHERE id = $1`, id, bio, string(data))
	return err
}

func (r *userRepository) TouchLogin(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *userRepository) SetBadgeEmails(ctx context.Context, id int64, off bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET badge_emails_off = $2 WHERE id = $1`, id, off)
	return err
}
