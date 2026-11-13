package web

import (
	"net/http"
	"time"

	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/service"
)

// SetActivity gives the handler the activity log for the Insight tab
func (h *Handler) SetActivity(a *service.ActivityService) { h.activity = a }

// insightView is the members' activity part of the Insight tab
type insightView struct {
	Range  string
	Ranges []string
	Rows   []*models.ActivitySummary
	Total  models.ActivitySummary
	Active int // members with any activity in the range
	Since  *time.Time
}

// InsightPage shows the translation statistics chart and what each member did (public)
func (h *Handler) InsightPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "project.tab_insight") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "insight"
	if groups, err := h.glossary.GroupsWithFiles(r.Context(), p); err == nil {
		data.Data["GroupChart"] = h.groupChart(r, p, groups)
	}
	v := insightView{Range: "30"}
	for _, rg := range service.ActivityRanges {
		v.Ranges = append(v.Ranges, rg.Key)
		if rg.Key == r.URL.Query().Get("r") {
			v.Range = rg.Key
		}
	}
	if h.activity != nil {
		rows, since, err := h.activity.Summary(r.Context(), p, v.Range)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		v.Rows, v.Since = rows, since
		for _, s := range rows {
			v.Total.Translated += s.Translated
			v.Total.TranslatedWords += s.TranslatedWords
			v.Total.Reviewed += s.Reviewed
			v.Total.Edited += s.Edited
			v.Total.DraftsApproved += s.DraftsApproved
			v.Total.DraftsFixed += s.DraftsFixed
			v.Total.Comments += s.Comments
			v.Total.Commits += s.Commits
		}
		v.Active = len(rows)
	}
	if h.activity != nil {
		if days, err := h.activity.HourlyTranslated(r.Context(), p); err == nil {
			data.Data["WordsChart"] = buildWordsChart(data.Lang, days, time.Now())
		}
	}
	data.Data["Insight"] = v
	h.render(w, "insight", data)
}
