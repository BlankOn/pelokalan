// Package apperr holds the errors the app shows to people (UserError: an i18n key with arguments) and the common ones
// every domain returns (not found, forbidden …)
package apperr

import "errors"

// UserError is an error that can be shown to the user. Key is an i18n message key.
type UserError struct {
	Key  string
	Args []any
}

func (e *UserError) Error() string { return e.Key }

// New returns an error shown to the user as the i18n message key (with arguments)
func New(key string, args ...any) *UserError { return &UserError{Key: key, Args: args} }

// AsUserError extracts a UserError from err
func AsUserError(err error) (*UserError, bool) {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}

var (
	ErrNotWhitelisted     = New("err.not_whitelisted")
	ErrInvalidEmail       = New("err.invalid_email")
	ErrNameRequired       = New("err.name_required")
	ErrUsernameInvalid    = New("err.username_invalid")
	ErrUsernameReserved   = New("err.username_reserved")
	ErrUsernameTaken      = New("err.username_taken")
	ErrUserExists         = New("err.user_exists")
	ErrUserNotFound       = New("err.user_not_found")
	ErrOTPInvalid         = New("err.otp_invalid")
	ErrOTPExpired         = New("err.otp_expired")
	ErrOTPTooManyAttempts = New("err.otp_too_many")
	ErrOTPTooSoon         = New("err.otp_too_soon")
	ErrInvalidToken       = errors.New("invalid token")

	ErrForbidden            = New("err.forbidden")
	ErrProjectPrivate       = New("err.project_private")
	ErrNotFound             = New("err.not_found")
	ErrProjectNameRequired  = New("err.project_name_required")
	ErrSlugInvalid          = New("err.slug_invalid")
	ErrSlugTaken            = New("err.slug_taken")
	ErrInvalidRole          = New("err.invalid_role")
	ErrAlreadyMember        = New("err.already_member")
	ErrAlreadyInvited       = New("err.already_invited")
	ErrInvitationInvalid    = New("err.invitation_invalid")
	ErrInviteLinkInvalid    = New("err.invite_link_invalid")
	ErrInviteLinkOptions    = New("err.invite_link_options")
	ErrCannotChangeOwner    = New("err.cannot_change_owner")
	ErrInvalidLanguage      = New("err.invalid_language")
	ErrFilenameTaken        = New("err.filename_taken")
	ErrTemplateExists       = New("err.template_exists")
	ErrNoTemplate           = New("err.no_template")
	ErrTemplateMismatch     = New("err.template_mismatch")
	ErrNoSyncLink           = New("err.no_sync_link")
	ErrSyncRunning          = New("err.sync_running")
	ErrInvalidFile          = New("err.invalid_file")
	ErrEmptyCatalog         = New("err.empty_catalog")
	ErrEntryConflict        = New("err.entry_conflict")
	ErrCannotRemoveSelfOnly = New("err.owner_cannot_leave")
)
