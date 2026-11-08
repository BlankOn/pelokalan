package models

import "time"

// User is a registered account. Users sign in with a one-time code sent by email.
type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Lang     string `json:"lang"`
	// Translator identity written to PO headers; empty values fall back to Name/Email
	TranslatorName  string     `json:"translator_name"`
	TranslatorEmail string     `json:"translator_email"`
	LanguageTeam    string     `json:"language_team"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// DisplayName returns the name, or the email when the name is empty
func (u *User) DisplayName() string {
	if u == nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// CreditName returns the name used to credit the user in PO files
func (u *User) CreditName() string {
	if u.TranslatorName != "" {
		return u.TranslatorName
	}
	return u.DisplayName()
}

// CreditEmail returns the email used to credit the user in PO files
func (u *User) CreditEmail() string {
	if u.TranslatorEmail != "" {
		return u.TranslatorEmail
	}
	return u.Email
}

// TranslatorIdentity returns "Name <email>" for the Last-Translator header
func (u *User) TranslatorIdentity() string {
	return u.CreditName() + " <" + u.CreditEmail() + ">"
}

// Initial returns the first letter of the display name, for avatars
func (u *User) Initial() string {
	for _, r := range u.DisplayName() {
		return string(r)
	}
	return "?"
}
