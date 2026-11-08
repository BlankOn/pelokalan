package postgres

import (
	"context"
	"database/sql"
	"errors"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type discussionRepository struct {
	db *sql.DB
}

// NewDiscussionRepository creates a new PostgreSQL discussion repository
func NewDiscussionRepository(db *sql.DB) repository.DiscussionRepository {
	return &discussionRepository{db: db}
}

const discussionColumns = `d.id, d.project_id, d.subject_type, d.subject_key, d.title, d.created_by, d.comment_count,
	d.last_activity_at, d.created_at`

func scanDiscussion(row interface{ Scan(...any) error }, extra ...any) (*models.Discussion, error) {
	d := &models.Discussion{}
	dest := append([]any{&d.ID, &d.ProjectID, &d.SubjectType, &d.SubjectKey, &d.Title, &d.CreatedBy, &d.CommentCount,
		&d.LastActivityAt, &d.CreatedAt}, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return d, err
}

func (r *discussionRepository) Get(ctx context.Context, projectID int64, subjectType, subjectKey string) (*models.Discussion, error) {
	return scanDiscussion(r.db.QueryRowContext(ctx,
		`SELECT `+discussionColumns+` FROM discussions d WHERE d.project_id = $1 AND d.subject_type = $2 AND d.subject_key = $3`,
		projectID, subjectType, subjectKey))
}

func (r *discussionRepository) GetByID(ctx context.Context, id int64) (*models.Discussion, error) {
	return scanDiscussion(r.db.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM discussions d WHERE d.id = $1`, id))
}

func (r *discussionRepository) GetOrCreate(ctx context.Context, d *models.Discussion) error {
	// the no-op update makes RETURNING work when the row exists
	return r.db.QueryRowContext(ctx,
		`INSERT INTO discussions (project_id, subject_type, subject_key, title, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (project_id, subject_type, subject_key) DO UPDATE SET title = EXCLUDED.title
		 RETURNING id, comment_count, last_activity_at, created_at`,
		d.ProjectID, d.SubjectType, d.SubjectKey, d.Title, d.CreatedBy,
	).Scan(&d.ID, &d.CommentCount, &d.LastActivityAt, &d.CreatedAt)
}

func (r *discussionRepository) List(ctx context.Context, projectID int64, f repository.DiscussionFilter) ([]*models.Discussion, int, error) {
	where := `d.project_id = $1 AND d.comment_count > 0`
	args := []any{projectID}
	if f.SubjectType != "" {
		args = append(args, f.SubjectType)
		where += ` AND d.subject_type = $2`
	}
	if f.MentionedUserID > 0 {
		args = append(args, f.MentionedUserID)
		where += ` AND EXISTS (SELECT 1 FROM comments c JOIN comment_mentions m ON m.comment_id = c.id
		           WHERE c.discussion_id = d.id AND c.deleted_at IS NULL AND m.user_id = $` + itoaArg(len(args)) + `)`
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM discussions d WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+discussionColumns+`,
		        COALESCE((SELECT COALESCE(NULLIF(u.name, ''), u.username) FROM comments c JOIN users u ON u.id = c.author_id
		                  WHERE c.discussion_id = d.id AND c.deleted_at IS NULL ORDER BY c.id DESC LIMIT 1), ''),
		        (SELECT COUNT(DISTINCT c.author_id) FROM comments c WHERE c.discussion_id = d.id AND c.deleted_at IS NULL)
		 FROM discussions d WHERE `+where+`
		 ORDER BY d.last_activity_at DESC LIMIT $`+itoaArg(len(args)-1)+` OFFSET $`+itoaArg(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Discussion
	for rows.Next() {
		var last string
		var participants int
		d, err := scanDiscussion(rows, &last, &participants)
		if err != nil {
			return nil, 0, err
		}
		d.LastAuthor, d.Participants = last, participants
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func itoaArg(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func (r *discussionRepository) Counts(ctx context.Context, projectID int64, subjectType string, keys []string) (map[string]int, error) {
	out := map[string]int{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT subject_key, comment_count FROM discussions
		 WHERE project_id = $1 AND subject_type = $2 AND subject_key = ANY($3) AND comment_count > 0`,
		projectID, subjectType, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

func insertMentions(ctx context.Context, tx *sql.Tx, commentID int64, mentions []int64) error {
	for _, uid := range mentions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO comment_mentions (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, commentID, uid); err != nil {
			return err
		}
	}
	return nil
}

func (r *discussionRepository) AddComment(ctx context.Context, c *models.Comment, mentions []int64, attachments []*models.Attachment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO comments (discussion_id, author_id, body) VALUES ($1, $2, $3) RETURNING id, created_at`,
		c.DiscussionID, c.AuthorID, c.Body).Scan(&c.ID, &c.CreatedAt); err != nil {
		return err
	}
	if err := insertMentions(ctx, tx, c.ID, mentions); err != nil {
		return err
	}
	for _, a := range attachments {
		a.CommentID = c.ID
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO attachments (comment_id, blob_key, filename, content_type, size)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
			a.CommentID, a.BlobKey, a.Filename, a.ContentType, a.Size).Scan(&a.ID, &a.CreatedAt); err != nil {
			return err
		}
	}
	c.Attachments = attachments
	if _, err := tx.ExecContext(ctx,
		`UPDATE discussions SET comment_count = comment_count + 1, last_activity_at = NOW() WHERE id = $1`, c.DiscussionID); err != nil {
		return err
	}
	return tx.Commit()
}

const commentColumns = `c.id, c.discussion_id, c.author_id, COALESCE(NULLIF(u.name, ''), u.username, ''), COALESCE(u.username, ''),
	c.body, c.created_at, c.edited_at, c.deleted_at`

func scanComment(row interface{ Scan(...any) error }) (*models.Comment, error) {
	c := &models.Comment{}
	err := row.Scan(&c.ID, &c.DiscussionID, &c.AuthorID, &c.AuthorName, &c.AuthorUsername, &c.Body, &c.CreatedAt,
		&c.EditedAt, &c.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return c, err
}

func (r *discussionRepository) Comments(ctx context.Context, discussionID int64) ([]*models.Comment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+commentColumns+` FROM comments c LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.discussion_id = $1 ORDER BY c.id`, discussionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Comment
	byID := map[int64]*models.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		byID[c.ID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, r.loadAttachments(ctx, discussionID, byID)
}

func (r *discussionRepository) loadAttachments(ctx context.Context, discussionID int64, byID map[int64]*models.Comment) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT a.id, a.comment_id, a.blob_key, a.filename, a.content_type, a.size, a.created_at
		 FROM attachments a JOIN comments c ON c.id = a.comment_id
		 WHERE c.discussion_id = $1 AND c.deleted_at IS NULL ORDER BY a.id`, discussionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		a := &models.Attachment{}
		if err := rows.Scan(&a.ID, &a.CommentID, &a.BlobKey, &a.Filename, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			return err
		}
		if c := byID[a.CommentID]; c != nil {
			c.Attachments = append(c.Attachments, a)
		}
	}
	return rows.Err()
}

func (r *discussionRepository) Attachment(ctx context.Context, id int64) (*models.Attachment, int64, error) {
	a := &models.Attachment{}
	var projectID int64
	err := r.db.QueryRowContext(ctx,
		`SELECT a.id, a.comment_id, a.blob_key, a.filename, a.content_type, a.size, a.created_at, d.project_id
		 FROM attachments a JOIN comments c ON c.id = a.comment_id JOIN discussions d ON d.id = c.discussion_id
		 WHERE a.id = $1 AND c.deleted_at IS NULL`, id,
	).Scan(&a.ID, &a.CommentID, &a.BlobKey, &a.Filename, &a.ContentType, &a.Size, &a.CreatedAt, &projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, repository.ErrNotFound
	}
	return a, projectID, err
}

func (r *discussionRepository) GetComment(ctx context.Context, id int64) (*models.Comment, error) {
	return scanComment(r.db.QueryRowContext(ctx,
		`SELECT `+commentColumns+` FROM comments c LEFT JOIN users u ON u.id = c.author_id WHERE c.id = $1`, id))
}

func (r *discussionRepository) UpdateComment(ctx context.Context, id int64, body string, mentions []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE comments SET body = $2, edited_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id, body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comment_mentions WHERE comment_id = $1`, id); err != nil {
		return err
	}
	if err := insertMentions(ctx, tx, id, mentions); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *discussionRepository) DeleteComment(ctx context.Context, id, byUserID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var discussionID int64
	err = tx.QueryRowContext(ctx,
		`UPDATE comments SET deleted_at = NOW(), deleted_by = $2 WHERE id = $1 AND deleted_at IS NULL RETURNING discussion_id`,
		id, byUserID).Scan(&discussionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comment_mentions WHERE comment_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE comment_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE discussions SET comment_count = GREATEST(comment_count - 1, 0) WHERE id = $1`, discussionID); err != nil {
		return err
	}
	return tx.Commit()
}
