package web

import (
	"bytes"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"html"
	"html/template"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/i18n"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
	"pelokalan/internal/service"
)

// threadView is the data of the discussion component
type threadView struct {
	Lang       string
	Project    *models.Project
	Thread     *service.Thread
	User       *models.User
	CanComment bool
	CanManage  bool
	Members    []string        // usernames that can be mentioned
	Known      map[string]bool // usernames rendered as mentions
	ID         string          // DOM id of the component
	Error      string
}

func (h *Handler) threadView(r *http.Request, p *models.Project, t *service.Thread) threadView {
	v := threadView{
		Lang:       middleware.GetLang(r.Context()),
		Project:    p,
		Thread:     t,
		User:       middleware.GetUser(r.Context()),
		CanComment: p.Role.IsMember(),
		CanManage:  p.Role.CanManage(),
		Known:      map[string]bool{},
		ID:         "thread-" + t.Subject.Type + "-" + t.Subject.Key[:min(12, len(t.Subject.Key))],
	}
	if members, err := h.discuss.Members(r.Context(), p); err == nil {
		for name := range members {
			v.Known[name] = true
			v.Members = append(v.Members, name)
		}
		sort.Strings(v.Members)
	}
	return v
}

// markdown renders comments. It is goldmark's safe mode: raw HTML is escaped
// and dangerous link schemes (javascript:, vbscript:…) are dropped. GFM adds
// tables, strikethrough, task lists and auto-linking of bare URLs; hard wraps
// keep the line breaks people type.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(gmhtml.WithHardWraps()),
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	anchorOpenRe = regexp.MustCompile(`^<a\s`)
	escMentionRe = regexp.MustCompile(`(^|[^\w@./-])@([a-z0-9][a-z0-9._-]{1,28}[a-z0-9])`)
)

// commentHTML renders a comment as Markdown, then highlights @mentions of known
// usernames (not inside links or code) and makes links open in a new tab.
func commentHTML(body string, known map[string]bool) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(body), &buf); err != nil {
		return template.HTML(html.EscapeString(body))
	}
	out := buf.String()

	// walk tags and text; text inside <a>, <code> and <pre> is left alone
	var b strings.Builder
	depth := map[string]int{}
	last := 0
	for _, loc := range htmlTagRe.FindAllStringIndex(out, -1) {
		text := out[last:loc[0]]
		if depth["a"]+depth["code"]+depth["pre"] == 0 {
			text = escMentionRe.ReplaceAllStringFunc(text, func(m string) string {
				sub := escMentionRe.FindStringSubmatch(m)
				if !known[strings.ToLower(sub[2])] {
					return m
				}
				return sub[1] + `<span class="mention">@` + sub[2] + `</span>`
			})
		}
		b.WriteString(text)
		tag := out[loc[0]:loc[1]]
		name := strings.ToLower(strings.Trim(strings.Fields(strings.Trim(tag, "<>/"))[0], "/"))
		switch {
		case strings.HasPrefix(tag, "</"):
			if depth[name] > 0 {
				depth[name]--
			}
		case !strings.HasSuffix(tag, "/>"):
			depth[name]++
		}
		if anchorOpenRe.MatchString(tag) {
			tag = strings.Replace(tag, "<a ", `<a rel="noopener noreferrer nofollow" target="_blank" `, 1)
		}
		b.WriteString(tag)
		last = loc[1]
	}
	b.WriteString(out[last:])
	return template.HTML(b.String())
}

// GlossaryTermPage shows one glossary string with its translations and discussion
func (h *Handler) GlossaryTermPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	hash := strings.ToLower(chi.URLParam(r, "hash"))
	thread, err := h.discuss.Thread(r.Context(), p, models.SubjectGlossaryTerm, hash)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	term, err := h.glossary.Term(r.Context(), p, hash)
	if err != nil && err != service.ErrNotFound {
		h.fail(w, r, err)
		return
	}
	data := h.page(r, "")
	data.Title = thread.Subject.Title + " · " + i18n.T(data.Lang, "title.glossary") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "glossary"
	data.Data["Term"] = term
	data.Data["Thread"] = h.threadView(r, p, thread)
	if term != nil {
		data.Data["Summary"] = summarizeTerm(term)
		// longer glossary strings containing this one
		if page, err := h.glossary.Search(r.Context(), p, service.GlossarySearch{Query: term.Source, Language: firstLanguage(term)}); err == nil {
			var related []*models.GlossaryGroup
			for _, g := range page.Groups {
				if g.Hash != hash && len(related) < 12 {
					related = append(related, g)
				}
			}
			data.Data["Related"] = related
		}
	}
	h.render(w, "glossary_term", data)
}

func firstLanguage(t *models.GlossaryTerm) string {
	if len(t.Rows) > 0 {
		return t.Rows[0].Language
	}
	return ""
}

// termVariant is one translation of a term summed over versions
type termVariant struct {
	Language    string
	Translation string
	Count       int
	Share       int
	Versions    []string
}

// termVersion lists the translations used in one version
type termVersion struct {
	Label        string
	Language     string
	Translations []string
	Changed      bool // differs from the previous (older) version
}

type termSummary struct {
	Total    int
	Variants []termVariant
	Versions []termVersion
	Modules  []string
	Contexts []string
}

func summarizeTerm(t *models.GlossaryTerm) termSummary {
	var s termSummary
	idx := map[string]int{}
	byVersion := map[string]*termVersion{}
	var versionOrder []string
	modSeen, ctxSeen := map[string]bool{}, map[string]bool{}
	for _, row := range t.Rows {
		s.Total += row.Count
		k := row.Language + "\x00" + row.Translation
		i, ok := idx[k]
		if !ok {
			i = len(s.Variants)
			idx[k] = i
			s.Variants = append(s.Variants, termVariant{Language: row.Language, Translation: row.Translation})
		}
		s.Variants[i].Count += row.Count
		s.Variants[i].Versions = append(s.Variants[i].Versions, row.VersionLabel)
		vk := row.VersionLabel + "\x00" + row.Language
		tv, ok := byVersion[vk]
		if !ok {
			tv = &termVersion{Label: row.VersionLabel, Language: row.Language}
			byVersion[vk] = tv
			versionOrder = append(versionOrder, vk)
		}
		tv.Translations = append(tv.Translations, row.Translation)
		for _, m := range row.Modules {
			if !modSeen[m] {
				modSeen[m] = true
				s.Modules = append(s.Modules, m)
			}
		}
		for _, c := range row.Contexts {
			if !ctxSeen[c] {
				ctxSeen[c] = true
				s.Contexts = append(s.Contexts, c)
			}
		}
	}
	sort.SliceStable(s.Variants, func(i, j int) bool { return s.Variants[i].Count > s.Variants[j].Count })
	for i := range s.Variants {
		if s.Total > 0 {
			s.Variants[i].Share = s.Variants[i].Count * 100 / s.Total
		}
	}
	// versions newest first (natural order), marking where the translation changed
	versions := make([]*models.ProjectVersion, 0, len(versionOrder))
	lookup := map[*models.ProjectVersion]*termVersion{}
	for _, vk := range versionOrder {
		tv := byVersion[vk]
		name, ver := splitLabel(tv.Label)
		pv := &models.ProjectVersion{Name: name + "\x00" + tv.Language, Version: ver}
		versions = append(versions, pv)
		lookup[pv] = tv
	}
	service.SortVersions(versions)
	for i, pv := range versions {
		tv := lookup[pv]
		sort.Strings(tv.Translations)
		if i+1 < len(versions) {
			older := lookup[versions[i+1]]
			tv.Changed = older.Language == tv.Language && strings.Join(older.Translations, "|") != strings.Join(tv.Translations, "|")
		}
		s.Versions = append(s.Versions, *tv)
	}
	sort.Strings(s.Modules)
	return s
}

func splitLabel(label string) (string, string) {
	if i := strings.LastIndex(label, "-"); i > 0 {
		return label[:i], label[i+1:]
	}
	return label, ""
}

// ThreadPartial renders the discussion component of a subject (HTMX/fetch)
func (h *Handler) ThreadPartial(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	thread, err := h.discuss.Thread(r.Context(), p, chi.URLParam(r, "type"), chi.URLParam(r, "key"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderPartials(w, partial{"discussion_thread", h.threadView(r, p, thread)})
}

func (h *Handler) renderThread(w http.ResponseWriter, r *http.Request, p *models.Project, subjectType, key, errMsg string) {
	thread, err := h.discuss.Thread(r.Context(), p, subjectType, key)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v := h.threadView(r, p, thread)
	v.Error = errMsg
	if !isHTMX(r) {
		http.Redirect(w, r, thread.Subject.URL+"#"+v.ID, http.StatusSeeOther)
		return
	}
	h.renderPartials(w, partial{"discussion_thread", v})
}

// PostComment adds a comment and returns the updated thread
func (h *Handler) PostComment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	subjectType, key := chi.URLParam(r, "type"), chi.URLParam(r, "key")
	uploads, err := readAttachments(w, r)
	if err != nil {
		h.renderThread(w, r, p, subjectType, key, h.errorMessage(r, err))
		return
	}
	_, _, err = h.discuss.Comment(r.Context(), middleware.GetUser(r.Context()), p, subjectType, key,
		r.FormValue("body"), middleware.GetLang(r.Context()), uploads)
	if err != nil {
		if _, isUser := service.AsUserError(err); !isUser || err == service.ErrForbidden {
			h.fail(w, r, err)
			return
		}
		h.renderThread(w, r, p, subjectType, key, h.errorMessage(r, err))
		return
	}
	h.renderThread(w, r, p, subjectType, key, "")
}

// readAttachments reads the files of a multipart comment form
func readAttachments(w http.ResponseWriter, r *http.Request) ([]service.Upload, error) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		return nil, nil
	}
	limit := int64(service.MaxAttachments*service.MaxAttachmentBytes + 1<<20)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, &service.UserError{Key: "err.attachment_size", Args: []any{service.MaxAttachmentBytes >> 20}}
	}
	files := r.MultipartForm.File["files"]
	if len(files) > service.MaxAttachments {
		return nil, &service.UserError{Key: "err.attachments_count", Args: []any{service.MaxAttachments}}
	}
	var out []service.Upload
	for _, fh := range files {
		if fh.Size > service.MaxAttachmentBytes {
			return nil, &service.UserError{Key: "err.attachment_size", Args: []any{service.MaxAttachmentBytes >> 20}}
		}
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, service.MaxAttachmentBytes+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, service.Upload{Filename: fh.Filename, Data: data})
	}
	return out, nil
}

// Attachment serves a comment attachment. Images are shown inline; every other
// type is downloaded. The sandbox policy keeps any served file from running
// scripts on this site.
func (h *Handler) Attachment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	a, data, err := h.discuss.Attachment(r.Context(), p, idParam(r, "aid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	hdr.Set("ETag", `"`+a.BlobKey+`"`)
	disposition := "attachment"
	ctype := "application/octet-stream"
	if a.IsImage() {
		disposition, ctype = "inline", a.ContentType
	}
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	if match := r.Header.Get("If-None-Match"); match == `"`+a.BlobKey+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(data)
}

// EditComment updates the author's comment
func (h *Handler) EditComment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	d, err := h.discuss.EditComment(r.Context(), middleware.GetUser(r.Context()), p, idParam(r, "cid"), r.FormValue("body"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderThread(w, r, p, d.SubjectType, d.SubjectKey, "")
}

// DeleteComment removes a comment
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	d, err := h.discuss.DeleteComment(r.Context(), middleware.GetUser(r.Context()), p, idParam(r, "cid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderThread(w, r, p, d.SubjectType, d.SubjectKey, "")
}

// discussionRow is one line of the discussions tab
type discussionRow struct {
	D   *models.Discussion
	URL string
}

// DiscussionsPage lists all discussions of a project
func (h *Handler) DiscussionsPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	user := middleware.GetUser(r.Context())
	f := repository.DiscussionFilter{Limit: 50}
	mine := r.URL.Query().Get("mentions") == "1" && user != nil
	if mine {
		f.MentionedUserID = user.ID
	}
	f.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if f.Offset < 0 {
		f.Offset = 0
	}
	list, total, err := h.discuss.List(r.Context(), p, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	rows := make([]discussionRow, len(list))
	for i, d := range list {
		rows[i] = discussionRow{D: d, URL: h.discuss.SubjectURL(r.Context(), p, d)}
	}
	data := h.page(r, "")
	data.Title = i18n.T(data.Lang, "title.discussions") + " · " + p.Name
	data.Data["Project"] = p
	data.Data["Tab"] = "discussions"
	data.Data["Rows"] = rows
	data.Data["Total"] = total
	data.Data["Mine"] = mine
	if f.Offset+len(list) < total {
		data.Data["NextOffset"] = f.Offset + len(list)
	}
	if f.Offset > 0 {
		data.Data["PrevOffset"] = max(0, f.Offset-f.Limit)
	}
	h.render(w, "discussions", data)
}

// bareURL matches http(s) links in plain text; trailing punctuation is left out.
var bareURL = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// linkify escapes plain text (e.g. a project description) and turns bare http(s)
// links into anchors that open in a new tab.
func linkify(s string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range bareURL.FindAllStringIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		u := html.EscapeString(s[m[0]:m[1]])
		b.WriteString(`<a href="` + u + `" rel="noopener noreferrer nofollow" target="_blank">` + u + `</a>`)
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
	return template.HTML(b.String())
}
