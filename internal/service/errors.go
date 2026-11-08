package service

import "errors"

// UserError is an error that can be shown to the user. Key is an i18n message key.
type UserError struct {
	Key  string
	Args []any
}

func (e *UserError) Error() string { return e.Key }

func userErr(key string, args ...any) *UserError { return &UserError{Key: key, Args: args} }

// AsUserError extracts a UserError from err
func AsUserError(err error) (*UserError, bool) {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}

var (
	ErrNotWhitelisted     = userErr("err.not_whitelisted")
	ErrInvalidEmail       = userErr("err.invalid_email")
	ErrNameRequired       = userErr("err.name_required")
	ErrUsernameInvalid    = userErr("err.username_invalid")
	ErrUsernameReserved   = userErr("err.username_reserved")
	ErrUsernameTaken      = userErr("err.username_taken")
	ErrUserExists         = userErr("err.user_exists")
	ErrUserNotFound       = userErr("err.user_not_found")
	ErrOTPInvalid         = userErr("err.otp_invalid")
	ErrOTPExpired         = userErr("err.otp_expired")
	ErrOTPTooManyAttempts = userErr("err.otp_too_many")
	ErrOTPTooSoon         = userErr("err.otp_too_soon")
	ErrInvalidToken       = errors.New("invalid token")

	ErrForbidden            = userErr("err.forbidden")
	ErrNotFound             = userErr("err.not_found")
	ErrProjectNameRequired  = userErr("err.project_name_required")
	ErrSlugInvalid          = userErr("err.slug_invalid")
	ErrSlugTaken            = userErr("err.slug_taken")
	ErrInvalidRole          = userErr("err.invalid_role")
	ErrAlreadyMember        = userErr("err.already_member")
	ErrAlreadyInvited       = userErr("err.already_invited")
	ErrInvitationInvalid    = userErr("err.invitation_invalid")
	ErrInviteLinkInvalid    = userErr("err.invite_link_invalid")
	ErrInviteLinkOptions    = userErr("err.invite_link_options")
	ErrCannotChangeOwner    = userErr("err.cannot_change_owner")
	ErrInvalidLanguage      = userErr("err.invalid_language")
	ErrFilenameTaken        = userErr("err.filename_taken")
	ErrTemplateExists       = userErr("err.template_exists")
	ErrNoTemplate           = userErr("err.no_template")
	ErrInvalidFile          = userErr("err.invalid_file")
	ErrEmptyCatalog         = userErr("err.empty_catalog")
	ErrEntryConflict        = userErr("err.entry_conflict")
	ErrCannotRemoveSelfOnly = userErr("err.owner_cannot_leave")
)
