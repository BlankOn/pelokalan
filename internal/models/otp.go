package models

import "time"

// OTP purposes
const (
	OTPPurposeLogin    = "login"
	OTPPurposeRegister = "register"
)

// OTPCode is a one-time code sent by email
type OTPCode struct {
	ID        int64
	Email     string
	Purpose   string
	CodeHash  string
	Name      string // display name supplied at registration
	Username  string // username chosen at registration
	Attempts  int
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// IsExpired reports whether the code can no longer be used
func (o *OTPCode) IsExpired() bool { return time.Now().After(o.ExpiresAt) }
