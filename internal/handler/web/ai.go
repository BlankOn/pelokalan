package web

import (
	"fmt"
	"net/http"
	"strconv"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// aiSettingsData adds the AI section data to the settings page
func (h *Handler) aiSettingsData(r *http.Request, p *models.Project, data PageData) {
	if !p.Role.CanManage() {
		return
	}
	st, err := h.ai.Settings(r.Context(), p)
	if err != nil {
		return
	}
	data.Data["AI"] = st
	data.Data["AIDefaults"] = map[string]any{
		"Model":       service.DefaultAIModel,
		"PriceInput":  service.DefaultAIPriceInput,
		"PriceOutput": service.DefaultAIPriceOutput,
	}
	if spent, err := h.ai.MonthSpend(r.Context(), p); err == nil {
		data.Data["AISpent"] = spent
	}
}

func (h *Handler) renderSettings(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg, flash string) {
	h.renderSettingsWith(w, r, p, errMsg, flash, nil)
}

// renderSettingsWith renders the settings page; groups fills the module groups popup (nil = the
// default: closed, or opened by ?groups=list|add)
func (h *Handler) renderSettingsWith(w http.ResponseWriter, r *http.Request, p *models.Project, errMsg, flash string, groups func(*PageData)) {
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.settings") + " · " + p.Name
	data.Error = errMsg
	if flash != "" {
		data.Flash = flash
	}
	data.Data["Project"] = p
	data.Data["Tab"] = "settings"
	if n, err := h.discuss.NotificationSettings(r.Context(), middleware.GetUser(r.Context()), p); err == nil {
		data.Data["Notify"] = n
	}
	data.Data["NotifyGroups"] = []notifyGroup{
		{"notify.group_me", []string{models.NotifyMention, models.NotifyMyFile}},
		{"notify.group_follow", []string{models.NotifyStarted, models.NotifyParticipated}},
		{"notify.group_project", []string{models.NotifyNewTopic, models.NotifyOtherTopic, models.NotifyOtherGlossary, models.NotifyOtherEntry}},
	}
	if p.Role.CanManage() {
		h.aiSettingsData(r, p, data)
		if groups == nil {
			groups = func(d *PageData) { h.moduleGroupsData(r, p, d, "", "") }
		}
		groups(&data)
	}
	h.render(w, "settings", data)
}

// SaveAISettings stores the project's LLM settings
func (h *Handler) SaveAISettings(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	err := h.ai.SaveSettings(r.Context(), middleware.GetUser(r.Context()), p, service.AISettingsInput{
		BaseURL:       r.FormValue("base_url"),
		Model:         r.FormValue("model"),
		APIKey:        r.FormValue("api_key"),
		PriceInput:    r.FormValue("price_input"),
		PriceOutput:   r.FormValue("price_output"),
		MonthlyBudget: r.FormValue("monthly_budget"),
		ExtraContext:  r.FormValue("extra_context"),
	})
	if err != nil {
		h.renderSettings(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/settings?flash=ai_saved#ai", http.StatusSeeOther)
}

// TestAISettings checks the API key and model
func (h *Handler) TestAISettings(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.ai.TestConnection(r.Context(), p); err != nil {
		h.renderSettings(w, r, p, h.errorMessage(r, err), "")
		return
	}
	http.Redirect(w, r, p.Path()+"/settings?flash=ai_ok#ai", http.StatusSeeOther)
}

// DeleteAISettings removes the stored key
func (h *Handler) DeleteAISettings(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadMemberProject(w, r)
	if !ok {
		return
	}
	if err := h.ai.RemoveSettings(r.Context(), p); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, p.Path()+"/settings?flash=ai_removed#ai", http.StatusSeeOther)
}

func aiOptionsFromRequest(r *http.Request) service.AIOptions {
	opts := service.AIOptions{
		UseMemory:    r.FormValue("use_memory") == "1",
		UseAI:        r.FormValue("use_ai") == "1",
		IncludeFuzzy: r.FormValue("include_fuzzy") == "1",
		MarkFuzzy:    r.FormValue("mark_fuzzy") == "1",
	}
	switch v := r.FormValue("glossary_group"); v {
	case "none":
		opts.GroupID = -1
	case "", "all":
		opts.GroupID = 0
	default:
		opts.GroupID, _ = strconv.ParseInt(v, 10, 64)
	}
	return opts
}

// AIEstimate renders the cost estimate for the AI dialog (HTMX)
func (h *Handler) AIEstimate(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	est, err := h.ai.Estimate(r.Context(), p, c, aiOptionsFromRequest(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderPartials(w, partial{"ai_estimate", map[string]any{
		"Lang": middleware.GetLang(r.Context()), "E": est, "Project": p,
	}})
}

// AIStart begins an AI pre-translation job
func (h *Handler) AIStart(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	job, err := h.ai.Start(r.Context(), middleware.GetUser(r.Context()), p, c, aiOptionsFromRequest(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	redirect(w, r, c.URL()+fmt.Sprintf("?aijob=%d", job.ID))
}

// AIJobStatus renders the progress of a job (HTMX polling)
func (h *Handler) AIJobStatus(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	job, err := h.ai.Job(r.Context(), c, idParam(r, "jid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderPartials(w, partial{"ai_job", map[string]any{"Lang": middleware.GetLang(r.Context()), "J": job, "Catalog": c}})
}

// AICancel stops a running job
func (h *Handler) AICancel(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	jid := idParam(r, "jid")
	if err := h.ai.Cancel(r.Context(), c, jid); err != nil {
		h.fail(w, r, err)
		return
	}
	job, err := h.ai.Job(r.Context(), c, jid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderPartials(w, partial{"ai_job", map[string]any{"Lang": middleware.GetLang(r.Context()), "J": job, "Catalog": c}})
}

// editorAIData adds the AI dialog and job banner data to the editor page
func (h *Handler) editorAIData(r *http.Request, p *models.Project, c *models.Catalog, data PageData) {
	if st, err := h.ai.Settings(r.Context(), p); err == nil {
		data.Data["AIConfigured"] = st != nil
	}
	if groups, err := h.glossary.Groups(r.Context(), p); err == nil {
		var withData []*models.ModuleGroup
		for _, v := range groups {
			if v.ImportCount > 0 {
				withData = append(withData, v)
			}
		}
		data.Data["GlossaryGroups"] = withData
	}
	// show the latest job while it runs, or when the page was opened for it
	if job, err := h.ai.LatestJob(r.Context(), c); err == nil && job != nil {
		if job.IsRunning() || r.URL.Query().Get("aijob") == strconv.FormatInt(job.ID, 10) {
			data.Data["AIJob"] = map[string]any{"Lang": data.Lang, "J": job, "Catalog": c}
		}
	}
}

// notifyGroup is a block of switches on the notification settings form
type notifyGroup struct {
	Title string
	Kinds []string
}
