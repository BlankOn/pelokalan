package web

import (
	"errors"
	"math"
	"net/http"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// Home shows the project list, which is public
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// LoginPage shows the sign-in form
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUser(r.Context()) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next"), "/projects"), http.StatusSeeOther)
		return
	}
	data := h.page(r, "title.login")
	data.Data["Next"] = r.URL.Query().Get("next")
	data.Data["Email"] = r.URL.Query().Get("email")
	h.render(w, "login", data)
}

// Login sends a sign-in code
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	lang := middleware.GetLang(r.Context())
	next := r.FormValue("next")
	email, err := h.authService.StartLogin(r.Context(), r.FormValue("email"), lang)
	if err != nil {
		data := h.page(r, "title.login")
		data.Error = h.errorMessage(r, err)
		data.Data["Email"] = r.FormValue("email")
		data.Data["Next"] = next
		data.Data["NoAccount"] = errors.Is(err, service.ErrUserNotFound)
		h.render(w, "login", data)
		return
	}
	h.renderVerify(w, r, email, models.OTPPurposeLogin, next, "")
}

// RegisterPage shows the registration form
func (h *Handler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUser(r.Context()) != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	data := h.page(r, "title.register")
	data.Data["Email"] = r.URL.Query().Get("email")
	data.Data["Next"] = r.URL.Query().Get("next")
	h.render(w, "register", data)
}

// Register validates the form and sends a registration code
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	lang := middleware.GetLang(r.Context())
	next := r.FormValue("next")
	email, err := h.authService.StartRegistration(r.Context(), r.FormValue("email"), r.FormValue("username"), r.FormValue("name"), lang)
	if err != nil {
		data := h.page(r, "title.register")
		data.Error = h.errorMessage(r, err)
		data.Data["Email"] = r.FormValue("email")
		data.Data["Name"] = r.FormValue("name")
		data.Data["Username"] = r.FormValue("username")
		data.Data["Next"] = next
		data.Data["HasAccount"] = errors.Is(err, service.ErrUserExists)
		h.render(w, "register", data)
		return
	}
	h.renderVerify(w, r, email, models.OTPPurposeRegister, next, "")
}

func (h *Handler) renderVerify(w http.ResponseWriter, r *http.Request, email, purpose, next, errMsg string) {
	data := h.page(r, "title.verify")
	data.Error = errMsg
	data.Data["Email"] = email
	data.Data["Purpose"] = purpose
	data.Data["Next"] = next
	data.Data["Minutes"] = int(h.authService.OTPExpiry().Minutes())
	data.Data["Resend"] = h.resendView(r, email, purpose, "", false)
	h.render(w, "verify", data)
}

// Verify checks the code and signs the user in
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	lang := middleware.GetLang(r.Context())
	email := r.FormValue("email")
	purpose := r.FormValue("purpose")
	if purpose != models.OTPPurposeRegister {
		purpose = models.OTPPurposeLogin
	}
	next := r.FormValue("next")

	user, err := h.authService.Verify(r.Context(), email, purpose, r.FormValue("code"), lang)
	if err != nil {
		h.renderVerify(w, r, email, purpose, next, h.errorMessage(r, err))
		return
	}
	token, err := h.authService.GenerateToken(user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.setSession(w, r, token)
	target := safeNext(next, "/projects")
	if purpose == models.OTPPurposeRegister && target == "/projects" {
		target = "/projects?flash=welcome"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// resendView is the data of the "resend code" block: the address's own
// remaining wait drives the countdown
func (h *Handler) resendView(r *http.Request, email, purpose, message string, isErr bool) map[string]any {
	wait := h.authService.ResendWait(r.Context(), email)
	return map[string]any{
		"Lang":    middleware.GetLang(r.Context()),
		"Email":   email,
		"Purpose": purpose,
		"Wait":    int(math.Ceil(wait.Seconds())),
		"Message": message,
		"Error":   isErr,
	}
}

// ResendCode sends a new code and returns the resend block with a fresh countdown (HTMX)
func (h *Handler) ResendCode(w http.ResponseWriter, r *http.Request) {
	lang := middleware.GetLang(r.Context())
	purpose := r.FormValue("purpose")
	if purpose != models.OTPPurposeRegister {
		purpose = models.OTPPurposeLogin
	}
	email := r.FormValue("email")
	if err := h.authService.ResendCode(r.Context(), email, purpose, lang); err != nil {
		h.renderPartials(w, partial{"resend_form", h.resendView(r, email, purpose, h.errorMessage(r, err), true)})
		return
	}
	h.renderPartials(w, partial{"resend_form", h.resendView(r, email, purpose, i18n.T(lang, "auth.code_resent"), false)})
}

func (h *Handler) setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.TokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.authService.TokenExpiry().Seconds()),
	})
}

// Logout clears the session
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: middleware.TokenCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// SetLang switches the UI language
func (h *Handler) SetLang(w http.ResponseWriter, r *http.Request) {
	lang := i18n.Normalize(r.URL.Query().Get("l"))
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.LangCookie,
		Value:    lang,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	if user := middleware.GetUser(r.Context()); user != nil {
		_ = h.authService.SetLang(r.Context(), user, lang)
	}
	http.Redirect(w, r, safeNext(r.URL.Query().Get("next"), "/"), http.StatusSeeOther)
}

// UpdateIdentity saves how the user is credited in PO files
func (h *Handler) UpdateIdentity(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	err := h.authService.UpdateIdentity(r.Context(), user, r.FormValue("translator_name"),
		r.FormValue("translator_email"), r.FormValue("language_team"))
	if err != nil {
		data := h.page(r, "title.profile")
		data.Error = h.errorMessage(r, err)
		h.render(w, "profile", data)
		return
	}
	http.Redirect(w, r, "/profile?flash=identity_saved", http.StatusSeeOther)
}

// ProfilePage shows the profile form
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "profile", h.page(r, "title.profile"))
}

// UpdateProfile saves the profile
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if err := h.authService.UpdateProfile(r.Context(), user, r.FormValue("username"), r.FormValue("name"), r.FormValue("lang")); err != nil {
		data := h.page(r, "title.profile")
		data.Error = h.errorMessage(r, err)
		h.render(w, "profile", data)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: middleware.LangCookie, Value: user.Lang, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/profile?flash=profile_saved", http.StatusSeeOther)
}
