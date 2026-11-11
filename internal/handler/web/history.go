package web

import (
	"fmt"
	"net/http"
	"strings"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
)

// chartPoint is one version in the progress chart of the history page
type chartPoint struct {
	X, Y, FY float64
	Commit   *models.Commit
}

const (
	chartW = 720.0
	chartH = 90.0
)

func progressChart(commits []*models.Commit) ([]chartPoint, string, string) {
	n := len(commits)
	if n == 0 {
		return nil, "", ""
	}
	var points []chartPoint
	var line, fuzzyLine strings.Builder
	for i := n - 1; i >= 0; i-- { // oldest first
		c := commits[i]
		idx := n - 1 - i
		x := chartW / 2
		if n > 1 {
			x = 8 + float64(idx)*(chartW-16)/float64(n-1)
		}
		y := chartH - 6 - float64(c.Percent())*(chartH-12)/100
		fy := chartH - 6 - float64(c.Percent()+c.FuzzyPercent())*(chartH-12)/100
		points = append(points, chartPoint{X: x, Y: y, FY: fy, Commit: c})
		fmt.Fprintf(&line, "%.1f,%.1f ", x, y)
		fmt.Fprintf(&fuzzyLine, "%.1f,%.1f ", x, fy)
	}
	return points, strings.TrimSpace(line.String()), strings.TrimSpace(fuzzyLine.String())
}

// History shows the version history of a catalog
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	c, p, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	h.catalogService.EnsureBaseline(r.Context(), c)
	commits, err := h.catalogService.History(r.Context(), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	working, err := h.catalogService.WorkingDiff(r.Context(), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "history.title") + " · " + c.Filename
	data.Data["Catalog"] = c
	data.Data["Project"] = p
	data.Data["Commits"] = commits
	data.Data["Working"] = working
	data.Data["Dirty"] = c.Owned && (!working.Empty() || len(commits) == 0)
	data.Data["Owned"] = c.Owned
	data.Data["FullPage"] = true // editor-style toolbar (back to the editor) above the page
	if len(commits) > 0 {
		data.Data["Original"] = commits[len(commits)-1]
	}
	points, line, fuzzyLine := progressChart(commits)
	data.Data["Chart"] = map[string]any{"Points": points, "Line": line, "FuzzyLine": fuzzyLine, "W": chartW, "H": chartH}
	if r.URL.Query().Get("restored") != "" {
		data.Flash = i18n.T(data.Lang, "flash.restored", r.URL.Query().Get("restored"))
	}
	h.render(w, "history", data)
}

// HistoryDiff renders the changes of one version, or of the working copy (HTMX)
func (h *Handler) HistoryDiff(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	lang := middleware.GetLang(r.Context())
	view := map[string]any{"Lang": lang, "Catalog": c, "Owned": c.Owned}
	if r.URL.Query().Get("working") == "1" {
		d, err := h.catalogService.WorkingDiff(r.Context(), c)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		view["Diff"] = d
		view["Working"] = true
	} else {
		cm, d, err := h.catalogService.CommitDiff(r.Context(), c, idParam(r, "hid"))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		view["Commit"] = cm
		view["Diff"] = d
	}
	h.renderPartials(w, partial{"history_diff", view})
}

// EditCommitMessage changes a version's message and re-renders its details, plus its
// timeline label out of band (HTMX)
func (h *Handler) EditCommitMessage(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	cm, err := h.catalogService.EditCommitMessage(r.Context(), middleware.GetUser(r.Context()), c, idParam(r, "hid"), r.FormValue("message"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	_, d, err := h.catalogService.CommitDiff(r.Context(), c, cm.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	lang := middleware.GetLang(r.Context())
	h.renderPartials(w,
		partial{"history_diff", map[string]any{"Lang": lang, "Catalog": c, "Owned": c.Owned, "Commit": cm, "Diff": d}},
		partial{"tl_msg", map[string]any{"Lang": lang, "C": cm, "OOB": true}},
	)
}

// CommitPreview renders the pending changes as a table for review before committing (HTMX)
func (h *Handler) CommitPreview(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	d, err := h.catalogService.WorkingDiff(r.Context(), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	lang := middleware.GetLang(r.Context())
	h.renderPartials(w, partial{"commit_diff", map[string]any{"Lang": lang, "Diff": d}})
}

// CommitCatalog saves the current state as a new version
func (h *Handler) CommitCatalog(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	_, err := h.catalogService.Commit(r.Context(), middleware.GetUser(r.Context()), c, r.FormValue("message"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	target := c.URL() + "?flash=committed"
	if r.FormValue("back") == "history" {
		target = c.URL() + "/history?flash=committed"
	}
	redirect(w, r, target)
}

// RestoreCommit brings the catalog back to an earlier version
func (h *Handler) RestoreCommit(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	cm, err := h.catalogService.Restore(r.Context(), middleware.GetUser(r.Context()), c, idParam(r, "hid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id := ""
	if cm.RestoredFrom != nil {
		id = fmt.Sprintf("%%23%d", *cm.RestoredFrom)
	}
	redirect(w, r, c.URL()+fmt.Sprintf("/history?restored=%s", id))
}

// DiscardChanges returns the catalog to its latest version
func (h *Handler) DiscardChanges(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadOwnCatalog(w, r)
	if !ok {
		return
	}
	cm, err := h.catalogService.Discard(r.Context(), middleware.GetUser(r.Context()), c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id := ""
	if cm.RestoredFrom != nil {
		id = fmt.Sprintf("%%23%d", *cm.RestoredFrom)
	}
	redirect(w, r, c.URL()+fmt.Sprintf("/history?restored=%s", id))
}

// DownloadCommit returns the PO file of a version
func (h *Handler) DownloadCommit(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.loadCatalog(w, r)
	if !ok {
		return
	}
	cm, data, err := h.catalogService.CommitSnapshot(r.Context(), c, idParam(r, "hid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	name := strings.TrimSuffix(c.Filename, ".po")
	name = strings.TrimSuffix(name, ".pot")
	ext := ".po"
	if c.IsTemplate {
		ext = ".pot"
	}
	sendFile(w, fmt.Sprintf("%s-v%d%s", name, cm.ID, ext), "text/x-gettext-translation; charset=utf-8", data)
}

// commitDirtyCount is used by the editor to show uncommitted changes
func (h *Handler) commitDirtyCount(r *http.Request, c *models.Catalog) int {
	d, err := h.catalogService.WorkingDiff(r.Context(), c)
	if err != nil {
		return 0
	}
	n := d.Total()
	if n == 0 && d.HeaderChanged {
		n = 1
	}
	return n
}
