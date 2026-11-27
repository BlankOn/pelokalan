package account

import (
	"context"

	"pelokalan/internal/models"
)

// UserRepository defines methods for user data access
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id int64) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	TouchLogin(ctx context.Context, id int64) error
	// SetBadgeEmails turns the emails about new badges off (true) or on
	SetBadgeEmails(ctx context.Context, id int64, off bool) error
	// SetPublicProfile stores the public profile's description and links (kind → URL)
	SetPublicProfile(ctx context.Context, id int64, bio string, links map[string]string) error
	// SetAvatar stores the blob key of the user's profile picture ("" = none)
	SetAvatar(ctx context.Context, id int64, key string) error
	// AvatarInUse reports whether some user's profile picture is that blob
	AvatarInUse(ctx context.Context, key string) bool
}

// OTPRepository defines methods for one-time code data access
type OTPRepository interface {
	Create(ctx context.Context, otp *models.OTPCode) error
	GetLatest(ctx context.Context, email, purpose string) (*models.OTPCode, error)
	// LatestForEmail returns the most recent code of an address, any purpose
	LatestForEmail(ctx context.Context, email string) (*models.OTPCode, error)
	IncrementAttempts(ctx context.Context, id int64) error
	MarkUsed(ctx context.Context, id int64) error
	InvalidateAll(ctx context.Context, email string) error
	CleanupExpired(ctx context.Context) error
}
