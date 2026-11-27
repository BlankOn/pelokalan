package web

import (
	"net/http"
	"strconv"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/web/middleware"
)

// Glossary extraction from long strings ("Ekstrak istilah dengan AI" on the glossary page, owners and coordinators):
// pretranslate.Service.EstimateExtract / StartExtract / LatestExtract / CancelExtract.

// GlossaryExtractEstimate: GET /projects/{id}/glossary-extract?group= → JSON estimate with texts for the dialog
func (h *Handler) GlossaryExtractEstimate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	groupID, _ := strconv.ParseInt(r.URL.Query().Get("group"), 10, 64)
	est, err := h.ai.EstimateExtract(r.Context(), p, groupID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	lang := middleware.GetLang(r.Context())
	resp := map[string]any{"configured": est.Configured, "running": est.Running, "to_send": est.ToSend,
		"over_budget": est.OverBudget}
	if est.Configured {
		resp["summary"] = i18n.T(lang, "extract.summary", est.Strings, est.Done, est.ToSend)
		if est.ToSend > 0 {
			resp["cost"] = i18n.T(lang, "extract.cost", est.Model, est.PromptTokens+est.CompletionTokens, usd(est.Cost))
		}
		if est.Budget > 0 {
			resp["budget"] = i18n.T(lang, "extract.budget", usd(est.MonthSpent), usd(est.Budget), usd(max(est.BudgetLeft, 0)))
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// GlossaryExtractStart: POST /projects/{id}/glossary-extract (group) → JSON {message}
func (h *Handler) GlossaryExtractStart(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	groupID, _ := strconv.ParseInt(r.FormValue("group"), 10, 64)
	if _, err := h.ai.StartExtract(r.Context(), middleware.GetUser(r.Context()), p, groupID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(middleware.GetLang(r.Context()), "extract.started")})
}

// GlossaryExtractCancel: POST /projects/{id}/glossary-extract/cancel → JSON {}
func (h *Handler) GlossaryExtractCancel(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.ai.CancelExtract(r.Context(), p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// GlossaryExtractStatus: GET /projects/{id}/glossary-extract/status → JSON {running, message, error, done}
func (h *Handler) GlossaryExtractStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if !p.Role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{})
		return
	}
	j, err := h.ai.LatestExtract(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": h.errorMessage(r, err)})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, extractStatus(middleware.GetLang(r.Context()), j))
}

// extractStatus describes the newest extraction run for the glossary page
func extractStatus(lang string, j *models.TermExtractionJob) map[string]any {
	resp := map[string]any{"running": false}
	if j == nil {
		return resp
	}
	resp["id"] = j.ID
	resp["running"] = j.IsRunning()
	switch j.Status {
	case models.AIJobRunning:
		resp["message"] = i18n.T(lang, "extract.progress", j.Processed, j.Total, j.Pairs, usd(j.Cost))
	case models.AIJobDone:
		resp["message"] = i18n.T(lang, "extract.done", j.Processed, j.Pairs, usd(j.Cost))
	case models.AIJobCancelled:
		resp["message"] = i18n.T(lang, "extract.cancelled", j.Processed, j.Total, j.Pairs)
	default:
		resp["error"] = i18n.T(lang, "extract.failed", storedText(lang, j.Error))
	}
	return resp
}
