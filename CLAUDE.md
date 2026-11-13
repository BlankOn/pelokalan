# CLAUDE.md

This file provides guidance for Claude Code when working with this codebase.

## Project Overview

Pelokalan is a web-based gettext translation editor for teams. Users sign in with
email one-time codes (only whitelisted emails), create projects, upload POT/PO files,
invite collaborators and translate in a desktop-style editor.

## Tech Stack

- **Language:** Go 1.24
- **Web Framework:** Chi v5
- **Database:** PostgreSQL 16 (pgx stdlib driver)
- **Auth:** Email OTP + JWT session cookie (golang-jwt/jwt v5)
- **Email:** Mailtrap HTTP API
- **Frontend:** Server-side templates with HTMX, plain JS in `static/js/app.js`
- **Migrations:** golang-migrate (run automatically on server start)

## Project Structure

```
cmd/server/main.go   # Bootstrap, dependency wiring, routes
internal/
  config/            # Environment configuration, email whitelist
  i18n/              # UI strings (id = primary, en); T(lang, key, args...)
  ai/                # LLM client (OpenAI-compatible), glossary matching, prompts, verification, estimates
  netsafe/           # HTTP transport refusing private/local addresses (SSRF protection)
  gnomel10n/         # Read-only client of the GNOME translation site API (cached, rate limited)
  glossary/          # Read PO archives in memory and aggregate source/translation pairs
  po/                # PO/POT parser & writer, MO compiler, validation, plural rules
  models/            # User, OTP, Project, Invitation, Catalog, Entry
  repository/        # Interfaces (repository.go) + postgres/ implementations
  service/           # Business logic (auth, project, catalog) and Mailer
  handler/
    middleware/      # Auth (cookie), Lang, SameOrigin (CSRF), logging
    web/             # HTML + HTMX handlers
  templates/         # layouts/, partials/, pages/
migrations/          # SQL migrations (up/down)
static/              # CSS, JS, htmx, logo
```

## Common Commands

```bash
make run-postgres   # PostgreSQL in Docker
make run            # run server (migrations run on start)
make build
make test
```

## Architecture

`Handlers → Services → Repositories → Models`, constructor injection (`NewXxx`).

- **Whitelist:** `config.Whitelist` from `EMAIL_WHITELIST`; empty means open registration
  (`Whitelist.Enabled()` is false and `IsAllowed` accepts any address). Checked on registration,
  login, code verification, every authenticated request (`AuthService.Authenticate`)
  and on invitations.
- **OTP:** `AuthService` stores an HMAC (keyed by `JWT_SECRET`) of 6-digit codes in
  `otp_codes`; codes expire, are single-use and limited by `OTP_MAX_ATTEMPTS`.
- **Public read access:** project list, glossary (`/projects/{id}`, `/glossary`) and
  members are served with optional auth. `ProjectService.GetPublic` fills `Project.Role`
  (empty for visitors); handlers use `loadProject` (public) or `loadMemberProject`.
  Member emails and pending invitations are only shown to members/managers.
- **Roles:** `owner` (one per project), `manager` (glossary + members), `translator`.
  Checked in services via `Project.Role`.
- **Project slugs:** `projects.slug` (unique, lowercase/digits/dashes, must contain a letter).
  The `{id}` route segment is the slug; `loadProject` also accepts numeric ids and 301-redirects
  page views to the slug URL. Use `Project.Path()` / `Catalog.URL()` for links.
- **Usernames:** required, unique, `^[a-z0-9][a-z0-9._-]{1,28}[a-z0-9]$`, with reserved
  words that clash with routes (`service/auth.go`). Chosen at registration (stored on the
  OTP until verified), editable in the profile. Migration 013 backfilled existing users.
- **File URLs:** `/projects/{id}/{username}/po/{filename}` (+ `/history`, `/raw`) render the
  editor; `loadCatalog` resolves either that path or `/catalogs/{cid}`. Old `/catalogs/{cid}`
  page links redirect there (`CatalogRedirect`); API calls (list, entries, save, AI) still use
  `/catalogs/{cid}/...`. Use `Catalog.URL()` for links. File names are unique per owner and
  An uploaded file whose name says nothing about the program (every dot-separated part is a language, a
  placeholder, a domain or branch word such as ui/main/master, or a number: "id.po", "pt_BR.po", "messages.pot",
  "ui.main.id.po", "main.id.po"; `genericFilename`, `genericNameWords`, known languages via `po.IsKnownLanguage`)
  is renamed from Project-Id-Version (unless that name would be just as generic)
  (`uploadFilename`: "Constrict main" → "constrict.main.id.po"), so its module is the program's name.
  project (`uniqueFilename`).
- **Several PO files per member and language are allowed** (one per module); only file names
  are unique per member and project, plus one template per member (migration 016).
- **Deleting a PO file is a soft delete** (`catalogs.deleted_at/deleted_by`, migration 029): it leaves the
  lists, counts, translation memory and name/template uniqueness (partial unique indexes), but its entries,
  history and discussions are kept. It opens read-only for everyone at `/catalogs/{cid}` (`Catalog.URL()`;
  `CatalogRedirect` renders it instead of redirecting) with a banner. `Catalog.WritableBy` (owner and not
  deleted) guards every write; `Owned` is false for deleted files. Its entry discussions are `ReadOnly`.
- **Deleting a discussion:** its starter (`discussions.created_by`) or an owner/manager can delete a whole
  discussion with its comments (`DiscussionService.DeleteDiscussion`, `POST .../discussions/{type}/{key}/delete`);
  not on read-only discussions. A deleted topic sends the user back to the Diskusi list.
- **PO files ("Berkas PO") are shared read-only:** anyone who can read the project can open,
  download and view the history of any file; only the owner changes it. `CatalogService.Get`
  sets `Catalog.Owned`; write handlers use `loadOwnCatalog` and write services check
  `c.OwnerID == user.ID`. Read routes live in the public route group, writes in the signed-in one.
  `Build` stamps the translator identity only when the owner exports their own file.
- **Settings** (`/projects/{id}/settings`) are for owners and managers only.
- **Project language** (`projects.language`, migration 034, backfilled from the most common file/import language;
  "" = not chosen): picked in Settings with a searchable `[data-combo]` of `po.LanguageNames` (code + native
  name, "id - Bahasa Indonesia"; plural rules stay in `po.languages`). `checkProjectLanguage` (`po.SameLanguage`:
  case and -/_ ignored, "pt" ≠ "pt_BR") refuses with `err.language_mismatch`: file uploads (`CatalogService.Upload`,
  also by link and "Ambil"), module group imports (checked before a group is created), sync links with a chosen
  language, and every downloaded language in `importInto` (so scheduled syncs fail with the message; stored sync
  errors are "key|arg|arg", shown by `errText`). Templates (no language) are always accepted.
- **Personal files (historical note):** catalogs have `owner_id`; `CatalogService.Get` returns
  ErrNotFound for other users. Any member can upload into their own workspace
  ("Berkas Anda", `/projects/{id}/files`). Translation memory uses the user's own files.
- **Project files** ("Berkas proyek", `project_files`, migration 027): every glossary import also
  stores its PO files (gzip) with stats, so the Penerjemahan tab has three sections: project files,
  your files, other members' files. Listed least complete first, 10 at a time (`ProjectFilePageSize`,
  "Muat lagi" via `GET /projects/{id}/project-files?offset=`); download, or "Ambil" copies one into
  the member's own files (`catalogService.Upload`). Synced module groups imported before 027 count as due
  (`DueGroupSyncs`) and are re-fetched to fill them in; groups uploaded by hand need a re-upload.
  Searchable (every word in file name, language or group label) and filterable by **module group**
  (= `ModuleGroup` with project files, e.g. gnome-51; `group_filter` partial, checkbox dropdown
  `[data-multi-filter]` with "Tampilkan semua" like the editor filter, shown when there is a group; `g` params).
  Search and filter live in `#pf-filter`; "Muat lagi" includes it (`hx-include`).
- Each of the three sections (`.files-section[data-sticky-section]`) keeps its title, card head, filter bar and
  column row on top while it scrolls (`[data-stick]`, position: sticky); app.js stacks them via `--stick-top` from
  the heights above (re-measured on resize and htmx swaps). They let go when the section ends; off on phones.
  Section titles are buttons (`.section-toggle`, chevron down / right) that fold the section; the state is kept in
  localStorage per project and section (`data-collapse-key` "files:<slug>:project|mine|others").
- "Berkas Anda" and "Berkas anggota lain" (one list, owner under each file) show 5 files at a time
  (`FileListPageSize`, `GET /projects/{id}/my-files|member-files?q=&g=&offset=`, `Handler.fileList`, partial
  `file_rows`) sorted by last edit (`catalogs.updated_at`, newest first) with search and the same module group filter: a file is in a group when its module
  (`service.ModuleOf`) is among that group's project files (`GlossaryService.ModuleGroups`); `g=0` = no group.
  Managers get "Sinkronkan dengan upstream" (in Berkas proyek and beside "Tambah berkas" in Berkas Anda;
  `POST /projects/{id}/project-files/sync` → `GlossaryService.SyncAll`, which ends with `RefreshUpstream`: every
  file state is read again from the release pages, however recent; it also runs for projects without a sync link):
  fetches every module group with a sync link in a background goroutine (requests time out after 30 s);
  `GlossaryService.syncing` makes it and the hourly `FetchDue` run one at a time. app.js (`form[data-sync]`)
  spins the button and polls `GET .../project-files/sync` (`SyncState`, per project, in memory) until it is
  done, then shows what changed (nothing when no file changed) and refreshes the rows.
  A project file the member already has (`CatalogService.MatchOwnFiles`: same language and same file name,
  else same module = name before the first dot) shows "Perbarui" instead of "Ambil": after a confirm it
  replaces that file (`UpdateFromUpstream`: commit before, replace, commit "commit.from_upstream|label"),
  so the history continues. The editor shows "Tarik upstream" before Riwayat when `GlossaryService.UpstreamFor`
  finds a project file for the open file (same name, else same module from the newest module group); it posts to
  the same take route with `into` and the same confirm popup.
  Under the "Berkas proyek" heading: last sync time (newest `last_fetched_at` of module groups with a sync link,
  in the reader's time zone) and the automatic interval (`Handler.SetSyncInterval`).
- **Template merge only touches files of the same module:** `templateFits` requires at least half of
  a file's strings to be in the template; "Perbarui semua terjemahan" (and template re-upload) skips
  other modules and reports how many it skipped (`MergeResult.Skipped`).
- **Invitations:** stored by email; accepted from `/invitations` after signing in
  with that email (no tokens in links).
- **Open or private projects** (`projects.open`, migration 037; private by default): chosen under "Keanggotaan" in
  Settings → Umum (`access` = open|private). Signed-in non-members see "Gabung" in the project header on open projects
  (`POST /projects/{id}/join` → `ProjectService.JoinOpen`, joins as translator and returns to the same page) and
  "🔒 Privat" on private ones; a pending email invitation to the project is accepted instead (its role wins), so it also
  works on private projects. Private projects refuse with `err.project_private`.
- **Invite links** (`project_invite_links`, migration 018): owners/managers create shareable
  links on the members page with a role, expiry (1/7/30 days or none) and a use limit
  (`service.InviteLinkDays/Uses`). `/join/{token}` is public: visitors are sent to
  login/register with `next=/join/{token}` and come back to confirm with a POST. `JoinByInviteLink`
  counts the use and adds the member in one transaction (limits hold under concurrency); existing
  members are not counted. Revoking deletes the link.
- **Catalogs:** a project has at most one template (`is_template`, empty language)
  and one catalog per language. Entries store `msgstr` as a JSON array; fuzzy is a
  separate column, other flags are comma separated.
- **Concurrency:** entries have a `version`; `UpdateEntry` only succeeds when the
  submitted version matches (`ErrEntryConflict` otherwise).
- **Errors shown to users:** `service.UserError{Key}` where Key is an i18n key.

## Glossary

- Glossary data comes from module groups (below). Uploads accept .po, .tar(.gz)/.tgz, .zip.
  Archives are read in memory with size/file-count limits (`glossary.Limits`); only `.po` entries are used.
- `glossary.Build` skips fuzzy/untranslated/obsolete entries, placeholder-only strings and
  translator credits; `CleanPair` strips keyboard accelerators (also when moved in the
  translation) and trailing `…`/`:` so variants group together.
- Stored per import in `glossary_terms` (one row per distinct source/translation pair);
  deleting an import removes its terms. Search groups by `source_norm` across imports.
- Language comes from the form, else the PO `Language:` header, else the `.xx.po` filename suffix.

## Glossary strings and discussions

- A glossary string = all `glossary_terms` rows with the same `source_norm`; its stable id is
  `glossary.TermHash(source_norm)` (SHA-256 hex), stored in `glossary_terms.source_hash`.
  Page: `/projects/{id}/glossary/{hash}` (translations, modules, related).
- Discussions are generic: `discussions(project_id, subject_type, subject_key, title)` +
  `comments` + `comment_mentions`. Subject types are resolved in
  `DiscussionService.ResolveSubject` / `SubjectURL`: `glossary_term` (key = term hash) and
  `po_entry` (key = "<catalog id>-<Entry.KeyHash()>", stable across restores/merges; page =
  file URL + `?entry=<hash>`) and `topic` (free discussion: key = random 12 hex chars, title
  stored on the discussion, page `/projects/{id}/topics/{key}`, started from the Diskusi tab via
  `CreateTopic`, which stores nothing if the first post is invalid). Add a case there to attach
  discussions to something new.
- A `po_entry` discussion whose PO file was deleted stays reachable on its own page
  (`/projects/{id}/threads/{type}/{key}`, `ThreadURL`): `ResolveSubject` returns it with `ReadOnly`, so it can
  be read by everyone but nobody can comment, edit or delete comments there (`ErrDiscussionClosed`).
- The glossary can be filtered by module (PO file, `GlossaryQuery.Module`); the term still shows
  all its translations. The module list (`GlossaryRepository.Modules`) feeds a searchable dropdown
  (`[data-combo]` in app.js).
- The sync status above the glossary always describes the newest module group with data.
- New comments email other project members in the background, filtered by each member's
  notification settings (`notification_settings`, migration 030; Pengaturan tab, visible to every member,
  `POST /projects/{id}/notifications`). A member gets at most one email per comment, sent when any reason that
  applies to them (`models.Notify*`: mention, my_file, started, participated, new_topic, other_topic,
  other_glossary, other_entry; `DiscussionService.reasons`, strongest first) is on; "muted" turns off all
  project email. Everything is on by default (kinds are stored as "off"). Emails say which setting caused
  them and link to the settings. Sign-in codes and invitations are always sent. Email buttons use
  `emailButton` (small).
- Comments: project members only; readable publicly. Deletion is soft (`deleted_at`, `deleted_by`): the
  author, username and time stay visible with a note saying who deleted it and when. `@username` mentions of members are
  stored and emailed. Authors edit/delete their own; owners/managers can delete any. Bodies are
  Markdown rendered by `commentHTML` (goldmark GFM + hard wraps, safe mode: raw HTML and
  javascript: links dropped; bare URLs auto-linked; mentions outside a/code/pre).
- Project descriptions are plain text; `linkify` escapes them and links bare http(s) URLs.
  Project cards use a stretched link (`.project-card-link::after`) so description links stay clickable.

## Comment attachments

- Files live in `internal/blobstore` (S3-like `Store`: Put/Get/Delete/GC), implemented on
  Postgres (`blobs`, content-addressed by SHA-256, deduplicated). `attachments` rows link a
  comment to a blob (migration 017). Only images are accepted (png/jpeg/gif/webp, detected from
  the content); limits: 5 images, 10 MB each (`service.MaxAttachments*`). The client checks the
  same limits; images can be picked, pasted or dropped anywhere on a `.thread`.
- Clicking an image opens a lightbox (`dialog.lightbox`, app.js): fit to screen, click for the
  original resolution, arrows step through the comment's images.
- Served at `/projects/{id}/attachments/{aid}/{filename}` with a sandbox CSP, immutable cache
  and ETag; only png/jpeg/gif/webp are shown inline, everything else downloads as octet-stream.
- Deleting a comment removes its attachment rows; `blobs.GC` (hourly) deletes unreferenced
  blobs older than 24 h. Swap the `Store` implementation to move files to real object storage.
- The Diskusi tab is searchable (`DiscussionFilter.Query`): every word must appear in the title or a
  visible comment (ILIKE); results show the newest matching comment. If it gets slow, add a pg_trgm
  or full-text index rather than a separate search service.
- Thread component `discussion_thread` is used on the term page and lazily in the glossary
  list (`.gl-thread[data-thread-url]`); "Diskusi" tab lists all threads (`?mentions=1`).

## Konteks

- One shared note per **module** of a project (`module_contexts`, migration 028; module = file name
  before the first dot, lowercase, `service.ModuleOf`): every member's file of the module shows the
  same note. It does not belong to a file, so it survives file deletion and comes back when someone
  takes/uploads the module again. Kept in Pelokalan only (never exported). Readable by anyone who can
  read the project; any member edits it.
- Saves carry the `version` the dialog opened with; a save after someone else's is refused (409,
  `ErrContextConflict`) and the dialog keeps the user's text, with "Salin teks saya" and "Muat versi
  terbaru" (like the wiki). Every save is recorded in `module_context_edits`; the dialog lists them as
  "@username · time" in a collapsed `<details>` (`partials/context_history.html`).
- Editor: "Konteks" toolbar button right of Praterjemah opens `#context-dialog`; saved with
  `POST /catalogs/{cid}/context` (JSON). A dot on the button shows that the file has a note.
- Project tab "Konteks" (`/projects/{id}/contexts`) lists modules with a note and their files, searchable.
- AI pre-translation puts the file's note into the system prompt (`ai.SystemPrompt(..., fileContext)`,
  capped at `ai.MaxFileContext`); it counts in the cost estimate. The note outranks the glossary for
  the words it talks about: glossary terms containing a word the note writes in quotes or backticks (3+ letters,
  `ai.ContextWords`/`ai.Mentioned`; plain sentences and examples do not count) are sent as `optional` hints (not enforced by `ai.Verify`), and
  whole-string glossary matches containing such a word go to the model instead of being applied.
- Project tabs carry monochrome line icons (`tab_icon` in project_header.html).
- Statistik / Stats tab (`/projects/{id}/insight`, before Anggota, `InsightPage` in insight.go, pages/insight.html):
  the module group chart (moved here from Penerjemahan) and "Aktivitas anggota" (public, like the rest of the
  project): totals and a per-member table. Above the module chart, "Pertumbuhan terjemahan" (`wordschart.go`,
  `buildWordsChart` from `ActivityRepository.HourlyTranslated`): **strings** people saved with a translation, added up
  hour by hour (one point per hour from the first recorded one to now, flat in hours without any; axis labels show times
  when the span is 3 days or less). Four lines on one scale, all counted in the green total: typed by hand (blue; empty →
  translated, no machine origin), AI drafts saved unchanged (purple; origin ai, text unchanged), AI drafts edited before
  saving (orange), existing translations edited (teal; translated, no machine origin, text changed, every such save
  counts). Glossary/memory drafts are not counted, and AI drafts nobody saved are not logged. Hovering shows the nearest
  hour in the same floating card as the module chart
  (app.js handles `.group-chart` and `.words-chart`; the card's rows come from `.gc-tip[data-rows]`).
- Activity log (`activity_events`, migration 033, append-only, no FK on catalog/entry so history survives):
  `ActivityService.Record` (errors only logged, nil-safe) is called from `CatalogService.SaveEntry` (`EntrySaved`:
  status before/after, machine origin before, text changed, source word count), manual commits, comments
  (`DiscussionService.post`) and sign-ups. Services get it via `SetActivity`. Counting rules (same in the view
  `activity_daily` and `ActivityRepository.Summary`): translated = untranslated → anything, reviewed = fuzzy →
  translated, edited = translated with text changed, drafts approved/fixed = origin set and text unchanged/changed.
  Add a `models.Activity*` kind and a Record call to log something new.
- Module group progress chart (Statistik tab; `group_chart` partial, `groupchart.go`): `group_stats`
  (migration 031) records each group's project file totals per language at every 15-minute check when they
  changed, and at least hourly (`GlossaryService.RecordStats`, `StatsInterval`), plus after uploads. Groups are
  chosen with the same `group_filter` dropdown (none = all; several are added up per hour, `sumByHour`);
  ranges Hari ini (last 24 h, hourly points) / 7 / 30 days / all (daily points, `lastPerBucket`). Lines (translated green, fuzzy orange, untranslated red) share one scale with tidy value labels by default (`niceStep`), so distances are real; "Skala per garis" (`cs=each`) zooms each line to its own range and shows [min–max] in the legend; a
  floating card (app.js) shows the nearest point. History starts on the day it was deployed.
- Penerjemahan (`/projects/{id}/files`) is the first tab and the landing page: `/projects/{id}` redirects
  there (`ProjectHome`, query kept); the glossary lives at `/projects/{id}/glossary`.

## Thesaurus

- Indonesian synonyms/antonyms (`internal/thesaurus`, table `thesaurus`, migration 024). On start,
  if the table is empty, `THESAURUS_URL` (default: victoriasovereigne/tesaurus dict.json) is
  downloaded with the SSRF-safe glossary fetcher and stored; lookups are served from memory.
- The source repo has no licence (data from Pusat Bahasa's 2008 Tesaurus), so lookups are for
  project members only: `GET /projects/{id}/thesaurus?q=` (JSON). Do not expose it publicly.
- `LookupForms` falls back to meN-/di-/-nya verb forms ("Tampilkan" → "menampilkan").
- Editor: selecting text in the translation box shows a popup (app.js); clicking replaces the
  selection, keeping a leading capital. The /licenses page credits the source.

## About page

`/about` (footer link "Tentang", before Lisensi; `AboutPage`, pages/about.html, `about.*` strings) explains the
goals (easy upstream contributions for newcomers, letting contributors focus on reviewing) and that the
project is funded by the BlankOn Foundation, and links the source code (https://github.com/BlankOn/pelokalan, MIT License, see LICENSE).

## Licenses page

`/licenses` (footer link) lists third-party software and data (`licenseCredits` in
handler/web/licenses.go). Keep it in sync with go.mod and static/ when adding dependencies.

## Wiki

- `wiki_pages` (migration 019): Markdown pages per project, nested at most `models.WikiMaxDepth`
  (3) levels via `parent_id`, ordered by `position`. Slugs are unique per project, derived from the
  title on create (`wikiSlug`) and can be edited; old addresses keep redirecting
  (`wiki_redirects`, migration 020). "new", "files", "preview" are reserved.
- Readable publicly; members create/edit; owners, managers and the page author delete (pages
  with subpages cannot be deleted). Edits carry the page `Version()`; if someone saved in
  between, the save is refused (409) and the editor keeps the user's text with steps to resolve:
  copy it, reload the latest version, edit again. It never overwrites. Tree moves are
  last-write-wins.
- Every create/save is recorded in `wiki_edits` (migration 021, backfilled from created/updated);
  the article lists the latest 50 as "@username · time", converted to the reader's time zone
  (`time[data-local]` in app.js). Moves are not edits.
- The tree is reorganized only by drag and drop in the sidebar (`WikiService.Move`, modes
  before/after/inside, depth checked for the whole subtree; `Reorder` does not touch updated_at).
- Rendering: `wikiHTML` (goldmark GFM, safe mode, heading ids). Images are only shown when
  served by this site (`localImages` turns others into links); external links open in a new tab.
  The editor preview posts to `/wiki/preview`, so it matches the saved page.
- Uploads: `wiki_files` share the blob store with comment attachments (10 MB each, 5 per upload) but
  accept any file type. They are uploaded immediately and Markdown is inserted at the cursor: images as
  `![…](…)` (shown in the page), other files as `[📎 name (size)](…)` download links (`fileLinks` marks
  them `.wiki-file` with `download`; served as octet-stream attachments under a sandbox CSP). Blob GC keeps
  blobs referenced by `wiki_files`. Images in `.wiki-body` open in the lightbox.

## Upstream state (GNOME translation site)

- `internal/gnomel10n` reads the public read-only API of l10n.gnome.org (`/api/v1/releases/`,
  `/releases/{release}/languages/{lang}`, `/modules/{m}/branches/{b}/domains/{d}/languages/{lang}`) over
  `netsafe`. Write actions (reserving, uploading) need a signed-in session there and are out of scope.
- The site throttles anonymous API clients (~100 requests/hour per address, then a 429 for most of an hour), and
  the API has no endpoint returning many modules' states at once. So **pages never ask the site for states**: they
  are stored in `upstream_states` (migration 035; shared by all projects, key release + language + file name stem;
  `listed = false` = the release has no such file) and read from there.
- `GlossaryService.WarmUpstream` (service/upstream.go; at start-up before the first `FetchDue`, then at the end of
  every `FetchDue` round, i.e. every 15 min; one round at a time) refreshes states older than `UpstreamFresh` (2 h).
  Because the API has no bulk endpoint, it reads the site's **HTML release page** per release and language
  (`/languages/{lang}/{release}/ui/`, plus `/doc/` when a due file is documentation, "…-help.<branch>"):
  `gnomel10n.ParseReleasePage` takes, per `<tr>`, the `/vertimus/{module}/{branch}/{domain}/{lang}/` link, the
  counts in `pre.statistics_workflow` (blank = 0), the state badge (English labels mapped to the API names: "To
  Review" → ToReview; none = "None") and `<time datetime>`. The page is untrusted text: regexes only, nothing
  executed. One request stores every module of the page; due files it does not list become "missing". The page
  has no PO file address, so `po_file` from an earlier API answer is kept, and `UpstreamPO` asks the API when none
  is stored.
- **The API is the fallback**: when a page cannot be read (HTTP error, throttled, or `ErrLayout` = the layout no
  longer parses; logged once per round), that release's due files are checked one API request each (never
  checked first, then the oldest). Page and API requests share the client's hourly budget
  (`gnomel10n.DefaultBudget` 40, minus `upstreamReserve` 8 left for people). The client also caches API answers in
  memory (stats 1 h, release lists 12 h, 404s too) and obeys the site's wait time. Pages may only ask for the
  release list (once per 12 h) for module groups named after a release (`upstreamWait` 3 s).
- Fixtures: `internal/gnomel10n/testdata` has recorded API JSON and a saved release page
  (`page_gnome-51_id_ui.html`); if the site changes its markup, save a new copy and adjust the regexes in page.go.
- Mapping (`upstreamLookup`): release = the module group's sync link on the site
  (`…/languages/id/gnome-51/ui.tar.gz`), else the group label when the site lists a release of that name;
  language = `projects.language`, else the file's. A project file matches a release entry by file name stem
  (`gnomel10n.FileStem` vs `Entry.Stem`): `<module>.<branch>` for the "po" domain, `<module>-help.<branch>` for
  "help", `<domain>.<branch>` otherwise ("locations.main.id.po"), case-insensitive. This avoids one request per
  module just to learn its file name. Members' files (`AttachUpstreamToCatalogs`) use the project file with the
  same name, else the same module (newest module group first).
- `models.UpstreamInfo` (embedded in `ProjectFile` and `Catalog`): `Upstream` + `UpstreamChecked`;
  `UpstreamClass()` is a state class, "missing" (not on the site) or "unknown" (not checked yet / no release).
- State filter (`upstream_filter` partial, checkbox dropdown `[data-multi-filter]`, param `u`, values
  `models.UpstreamFilters`, none ticked = all, with a note that states are checked gradually): next to the module
  group filter in "Berkas proyek" (`#pf-filter`, so "Muat lagi" keeps it; with a state filter
  `GlossaryService.ProjectFiles` loads all matching rows and pages in Go) and in "Berkas Anda" / "Berkas anggota
  lain" (`file_filter`, `fileList`). Shown when a module group is a release upstream (`HasUpstream`). Both lists show the
  badge (`upstream_badge` partial) under the file name.
- "Berkas Anda" order (`fileList`, `ownFileRank`): upstream state "ToReview" (labelled "Requested rework" /
  "Diminta perbaikan"), then Proofreading, then Translating, then everything else; last edit (newest first)
  within each group, also across "Muat lagi" pages and with search/filters. "Berkas anggota lain" sorts by last
  edit only.
- **A file's own upstream page** (`catalogs.upstream_page`, migration 038): "Tambah berkas manual" and "Perbarui berkas…"
  have an optional "Halaman modul upstream" field (`upstream_page`, e.g. `…/vertimus/gnome-calculator/main/po/id/`;
  checked and made canonical by `GlossaryService.UpstreamPageURL` via `gnomel10n.ParsePageURL`, else
  `err.upstream_page`; the update dialog is pre-filled, so emptying it removes the link). Such a file takes its state
  from that page instead of the project file match (`AttachUpstreamToCatalogs`), also in projects without a release.
  The state is fetched right after saving (`CheckUpstreamPage`, one API request, in the background) and refreshed by
  `WarmUpstream` straight through the API (no release page), stored in `upstream_states` with release ""
  (`linkedRelease`). The Sync button (`RefreshUpstream`, which waits for a running background round instead of
  skipping) asks for these fresh (`StatsMaxAge`, skipping the client's 1 h cache). Sync never changes the contents of
  members' files; only "Perbarui berkas…" does. A linked file also turns on the state filter in the file lists.
- No assignee is shown: the API does not say who reserved a file (only the site's HTML workflow page does).
- "Berkas proyek" rows (`AttachUpstream`, from the stored states) show the upstream state as a coloured badge under
  the file name (`.up-badge.up-<UpstreamStats.StateClass>`, grey for "None" = no activity and unknown states), linking
  to the site's page with the upstream % and counts in its tooltip; labels are `upstream.state_*`
  (`UpstreamStats.StateKey`; unknown states are shown as sent). The progress column shows only Pelokalan's copy. There is one button per row: "Ambil" / "Perbarui" (and the editor's
  "Tarik upstream", same take route) first download the current `po_file` from the site (`GlossaryService.UpstreamPO`: the stored `po_file`, looked up
  live only when the file was never checked;
  upload size limit; history label "l10n.gnome.org module/branch") and fall back to the copy saved at the last sync
  when the site has no entry or cannot be reached (throttled, down).
- A file with a release but no stored state shows a grey dashed "Status belum ada ↗" label (`.up-unknown`) linking
  to the page built from its stem (`GlossaryService.guessPageURL`, `UpstreamInfo.UpstreamLink`), to check by hand.
  Colours: Translating orange, Requested rework (ToReview) red, None/unknown grey.
- Tests use API JSON and the release page recorded from the site (`internal/gnomel10n/testdata`); never call the site from tests.

## Module groups ("Kelompok modul") and automatic downloads

- Formerly "glossary versions"; renamed everywhere in migration 039 (tables `module_groups`, `module_group_imports`;
  columns `group_id`, `sync_url`, `sync_language`) and in code (`models.ModuleGroup`, `ModuleGroupImport`,
  `ModuleGroupOption`; `GroupID`/`GroupLabel` fields; `ListGroups`, `ImportGroup`, `SetGroupSync`, `FetchGroup`…).
  A module group (name + version, label "gnome-51") holds the PO archives imported per language; they feed the
  glossary terms and the project files. Sorted naturally (`service.SortGroups`), newest first. Re-importing a
  group + language replaces it.
- A group may have a `sync_url`; `GlossaryService.FetchDue` runs every 15 minutes from main.go and
  fetches URLs older than `GLOSSARY_FETCH_INTERVAL_HOURS` (default 1), using ETag/Last-Modified and a
  content hash to skip unchanged archives. `glossary.Fetcher` refuses private/local
  addresses at dial time (SSRF protection) unless `GLOSSARY_ALLOW_PRIVATE_HOSTS=true`.

## Raw editor and translator identity

- `/catalogs/{cid}/raw`: the file built by `CatalogService.Build` in a textarea; saving
  (`SaveRaw`) re-parses it and replaces header + entries. The form carries the catalog's
  `updated_at` (UnixNano); a newer change is refused (`err.raw_conflict`).
  Leaving with unsaved changes (Penyunting, any link, browser Back via an extra history entry) opens
  `#raw-leave-dialog` with a line diff (app.js, LCS on the changed middle, 2 lines of context, at most 300 lines):
  Batal stays, "Abaikan perubahan" leaves, "Simpan dulu" saves with `next` and then goes there (`SaveRaw`).
  Syntax highlighting: `#raw-hl` (a `<pre>` behind the transparent textarea in `.raw-wrap.hl-on`) is rebuilt from the
  text on input (one frame), with the textarea's font metrics copied and its scroll mirrored by a transform.
  Keywords, msgid vs msgstr strings, escapes, format placeholders, markup tags and each comment kind
  (`#:` `#.` `#,` with fuzzy in bold, `#|`, `#~`) get their own `.po-*` colour. Skipped for files over 3 MB.
- Users have `translator_name/email` and `language_team` (profile page). `Build` sets
  `Last-Translator`, adds/updates the contributor line with `po.AddContributor` (placed right after the
  last existing "Name <email>, year." credit, never after the trailing bare `#`; a credit an earlier export
  put there is moved back; years merge via `po.AddYear`: 2025 + 2026 → "2025-2026", 2024 + 2026 → "2024, 2026"), and
  fills `Language-Team` only when the file has none (file properties win).
- Translator credits entry (`po.IsCreditsEntry`: translator-credits / translator_credits / translator-credit):
  for the file owner, validation (`validateEntry` in service/catalog.go, `po.CheckCredits`) adds
  `credits_missing` when their name/email is not in it and `credits_year` when their line lists years but
  not this one (lines without years are fine). The editor alert offers "Tambahkan identitas saya" / "Tambahkan
  tahun ini" (`creditFix` → `po.AddCredit`, same year rule), which fills the box (undoable, not saved).
- `/catalogs/{cid}/properties` edits Project-Id-Version, Report-Msgid-Bugs-To,
  Language-Team and the header comment lines.

## Module groups UI

- Settings has a "Kelompok modul" section (`#module-groups`, owners and managers) with the group labels and buttons
  that open the `#module-groups-dialog` popup (`partials/module_groups.html`, handlers in `module_group.go`) with
  tabs: list (groups, imports, sync link actions) and add (one form: file or URL). Routes live under
  `/projects/{id}/module-groups/…` (`sources`, `import`, `sync`, `{gid}/delete|fetch|unsync`,
  `imports/{iid}/delete`); they redirect to `/settings?groups=list|add#module-groups`, which reopens the popup
  (`moduleGroupsData`). The glossary page no longer manages groups; its empty state links to that popup.
  The glossary's group filter uses `?group=` (old `?version=` links still work); the AI dialog sends `glossary_group`.

## AI pre-translation

- Settings per project in `project_ai_settings` (owner/manager): base URL, model, API key
  encrypted with `ai.Box` (AES-GCM, key from `AI_SECRET_KEY`), prices per 1M tokens, monthly budget,
  and `extra_context` (migration 023): project-wide guidance sent as `<project_context>` before the
  file's `<file_context>` (the file note is more specific and wins). Words in either can make
  glossary hints optional.
- `AIService.plan` (service/ai.go): untranslated (optionally fuzzy) entries of the user's own
  catalog. Order of precedence: (1) `ai.Glossary.Exact` — whole string equals a glossary term
  whose top translation has >= 60% share: applied directly, no model call; (2) the model, with
  `ai.Glossary.Matches` (longest phrase first) sent as mandatory term hints.
- Re-translating entries that need review sends their existing text as `Item.Current` (a draft):
  the model must return it unchanged when correct and otherwise fix only real errors (no synonym
  swaps or restyling). For drafts all glossary hints are optional and whole-string glossary matches
  are not applied directly, so a person's word choice outranks the glossary.
- Items are built by `buildItem` (shared by jobs and the editor button), so glossary/Konteks/draft
  rules are identical everywhere.
- Editor "Terjemahkan dengan AI" button (Ctrl+Shift+A, owner only, project with an AI key), with a count
  selector beside it (1–4 alternatives, default 2, remembered in localStorage `aiAltCount`). With n > 1 the
  entry is sent n times in one call, numbered by `Item.Alternative`, plus `ai.AlternativesNote`; only
  alternative 1 carries the draft. Unusable and duplicate answers are dropped; the editor shows the rest side
  by side (`#ai-alts`); clicking a box (or Enter/Space on it) puts it in the translation box (undoable). With one result it goes straight in.
- Per-entry AI details:
  `POST /catalogs/{cid}/entries/{eid}/ai` → `AIService.TranslateEntry` translates one entry with the
  editor's current text as draft and returns it unsaved; cost is recorded as an `ai_jobs` row with
  `single = true` (migration 025), which counts toward the budget but is not shown as the file's job.
- Glossary hints are re-cased to the sentence (`ai.MatchCase`): a term used in lowercase in the
  source gets a lowercase hint ("Kontras" → "kontras"); the prompt says glossary fixes the word,
  not its capitalization.
- When AI replaces existing text, the old text is kept in `entries.replaced_msgstr` (migration 026;
  the last person-written text wins over earlier AI attempts) until a person saves. The editor's
  "Bandingkan" button shows old vs new side by side with a word diff; "Pakai yang sebelumnya"
  restores it (undoable). The per-entry AI button fills the comparison client-side.
- Requests use temperature 0.2 (`ai.chatTemperature`); if a model rejects it, the call is retried
  without a temperature.
- `ai.Verify` flags results that miss a required glossary translation or fail `po.Validate`;
  flagged results are always saved as fuzzy.
- Jobs (`ai_jobs`) run in a goroutine, batch by batch (25 items / 6000 chars), record real
  token usage and cost, honor cancel (`watchCancel` polls every 1.5 s and aborts the in-flight
  model request; finished batches are kept) and the monthly budget, and skip entries edited meanwhile
  (entry version check). Running jobs are marked failed on startup (`RecoverJobs`).
- Cost estimate: `ai.EstimateTokens` (~3.5 chars/token) via `AIService.Estimate`.

## Editor glossary section

At the top of the right sidebar (`entry_glossary` partial in editor.html, rendered by sidebar.html; below
1000px, where the sidebar is hidden, the copy in the editor pane above the source text shows instead) the editor lists glossary terms found in the entry (`GlossaryService.ForEntry`,
all versions, preferred translation + share). Chips insert the translation at the cursor; ↗ opens
the term page. Orange text when nothing matches or the language has no glossary. Chips the project AI
context or the file's Konteks talks about (`ai.Mentioned`, same rule as pre-translation) are orange
(`EntryTerm.Overridden`). Term matchers are
cached per project+language and rebuilt only when `GlossaryRepository.Stamp` (imports) changes.

## Entry origin

`entries.origin` is '', 'ai', 'glossary' or 'memory' for machine translations not yet saved
by a person (`SaveEntry` clears it). The editor marks 'ai' with an orange bot, 'glossary' with a green book and
'memory' with blue cards (`icon_origin` in entry_list.html), with status-bar counts and filters for each. Snapshots keep it as an `x-origin-*` flag, stripped from downloads.

## Version history

- `catalog_commits`: linear history per catalog, gzip PO snapshot + stats + diff counts.
  Kinds: manual, auto (safety), restore, initial. Auto messages are i18n keys ("key|arg").
- `CatalogService.SafetyCommit` runs before replace/raw save/template merge/memory
  pre-translate/AI job, and after AI jobs; `EnsureBaseline` gives files without history a
  first version. `Restore` commits the current state first, then records a restore commit, so
  every step can be undone. `Discard` = restore latest commit.
- The Commit button badge (`commit_badge`/`commit_status` partials) is refreshed out-of-band after
  every entry save, so needs-work and origin changes show up immediately.
- The owner can change a version's message: select it on the history page, "Ubah pesan" (before Unduh) turns the
  title into a field (`POST /catalogs/{cid}/history/{hid}/message`, `CatalogService.EditCommitMessage`, same
  limits as Commit, no "|"); the response re-renders the details and the timeline label (`tl_msg`, out of band).
- History page `/catalogs/{cid}/history`: timeline, progress chart (SVG), diff panel (HTMX). It is a full
  page with the editor-style toolbar (back to the editor, PO download); the content scrolls below it.
- The editor's Commit button is disabled while the badge shows no changes (app.js follows the badge,
  which is refreshed out-of-band after saves and commits).
- Diffs compare text, needs-work flag and origin; an AI/glossary/memory translation saved by a
  person without text changes is a "reviewed" item (counted in `catalog_commits.reviewed`).
- Diffs also report metadata: `Diff.HeaderChanges` (each changed header field "Name: old → new", and
  header comment lines added/removed, e.g. translator credits) and entries where only comments,
  references, flags, `#|` lines or the plural source changed (kind "meta", `Diff.Meta`, `DiffItem.Meta`;
  counted in `Total`, so they enable Commit). Shown by `diff_header`/`diff_field` in the commit dialog and history.

## Pre-translation

One dialog (`#ai-dialog` in the editor): sources in order memory → glossary → AI (optional,
needs project AI settings). Implemented by `AIService.Start` / jobs in `ai_jobs`.

## Top bar

`partials/nav.html` centers a title: "Project · file" over the language when the page sets
`Data.Catalog` (editor, raw, history), else the project name when it sets `Data.Project`.
The brand links to /projects; Undangan lives in the user menu. In the editor toolbar the
search box fills the space after the buttons, followed by the filter (funnel) and order selects.

## Phones

All phone rules live in one `@media (max-width: 700px)` block at the end of style.css (it must
stay last to override). Rules of thumb: nothing may be wider than the screen (tabs scroll
sideways, tables become stacked blocks: `.files-table`, `.gl-table`), grid tracks use
`minmax(0, 1fr)`, and fields use 16px text so iOS does not zoom in. Tablets (701–960px, e.g. iPad mini
portrait 744px): the file tables hide their three count columns; the project tabs scroll sideways at
every width so they never widen the page (a page wider than the screen makes iPad zoom out).

## Static assets

Served with `Cache-Control: no-cache`; templates add `?v={{.Asset}}` (hash of static/) so
deploys never run stale JS/CSS.

## i18n

- All UI strings live in `internal/i18n/messages.go`; Bahasa Indonesia is primary.
- Templates: `{{t .Lang "key"}}` (use `$.Lang` inside range/with).
- Tests in `internal/i18n` fail when a key is missing in one language or used but undefined.
- Language comes from the `lang` cookie (`/lang?l=en`), overridden by the user's saved preference.

## Editor (HTMX)

- `GET /catalogs/{cid}` page; `GET /catalogs/{cid}/list` filtered list;
  `GET|POST /catalogs/{cid}/entries/{eid}` editor pane. Responses return the editor
  plus out-of-band swaps for the sidebar, the edited row and the status bar.
- Client behaviour (selection, shortcuts, dirty tracking, splitter) is in `static/js/app.js`.
- The sidebar is resizable: drag `#side-splitter` (220px to 60% of the width; arrow keys too). Dragging it under
  140px or clicking the small `#side-toggle` handle in the middle of the divider hides it (`.workspace.sidebar-hidden`);
  the same handle, then at the right edge, brings it back. Width and state live in localStorage (`sidebarWidth`,
  `sidebarHidden`). While hidden, the glossary shows in the editor pane (`.ed-glossary-narrow`).
- Plain Up/Down move between entries, also from the translation box (Left/Right stay there);
  in multi-line text they only leave the entry from the first/last line (`arrowNavigates`).
- While a pre-translation job runs (`#ai-job.ai-running`), the editor is read-only (`applyLock` in
  app.js: boxes readonly, controls and editing shortcuts disabled, navigation allowed); when it ends
  the list and current entry reload. "Terjemahkan AI" locks the box with a shimmer (`.ai-filling`).
- "Urungkan" (`#btn-undo`) undoes the last change to the translation: typing bursts and every tool
  (copy source, AI, glossary chip, thesaurus via `window.trBeforeChange`, suggestions) push a snapshot.
- Plural placeholders are checked like `msgfmt -c` (`po.ValidateWith` + `po.PluralFor(c.PluralForms, …)`, which
  evaluates the Plural-Forms formula for n = 0…1000): every `msgstr[i]` is compared with `msgid_plural`; only a
  form used for at most one number, in a file with several forms, may drop a placeholder. With one form
  (Indonesian) `msgstr[0]` must have the placeholders of `msgid_plural` (issue `plural_placeholders`).
  Downloading PO/MO from the editor while the file has problem entries asks first ("Lihat masalah" filters them).
- Source text marks easy-to-lose whitespace at its ends (`sourceWS`, template func `srcWS`): "␣ spasi di
  akhir/awal" for spaces/tabs and a blue "↵ baris baru di akhir/awal" for line breaks (×N for several);
  line breaks at the ends are shown only by their label. Copy uses the raw `data-text`.
- Search boxes in htmx filter forms must not reload on their own `change` (fired on blur): triggers use
  `change[target.type!='search']`, otherwise the rows are swapped under the user's next click.
- Problem warnings (`ed-alert-warn`) sit right below the translation box, before the save buttons. In the
  Terjemahan label row, Bandingkan / "Terjemahkan dengan AI" + count sit on the right, just before Perlu ditinjau.
  English calls Perlu ditinjau "Fuzzy".
- Validation flags `control_chars` and `markup` (tags such as <gui>, <key> differing from the source);
  `ai.UserPrompt` sends markup unescaped, and AI results with control characters are never saved.
- Toolbar order: Proyek | Tarik upstream, Riwayat, Commit | Konteks, Praterjemah, Validasi | Teks, Metadata |
  PO, MO, Bantuan, Pengaturan.
- Editor settings ("Pengaturan" after Bantuan, `#editor-settings-dialog`, signed-in users) are per account and
  apply to every file: `users.editor_checks_off` (migration 032, `User.ChecksOff`/`CheckOn`, everything on by
  default so new checks start on). Users untick validation rules (`po.Checks`; switched-off issues are dropped in
  `validateEntry` via `Catalog.ChecksOff`, so they are not marked or counted) and the statuses Validasi ticks
  (`service.EditorStatuses`; `#btn-validate[data-statuses]`). Saved with `POST /editor-settings` (back to `next`).
- The editor's Bantuan button (and `?` outside text fields) opens `#help-dialog` with all shortcuts;
  update it when adding shortcuts.
- Save buttons: the main one is green with a check and is what Ctrl+Enter presses: "Lepas pratinjau,
  simpan & lanjut" (`#btn-unfuzzy-save`) while the entry needs work, else "Simpan & lanjut" (CSS
  `:has(#btn-unfuzzy-save[hidden])`). Ctrl+Shift+Enter always saves keeping the flag.
- Files downloaded by link keep it in `catalogs.source_url` (migration 036, `uploadedURL`). The file menu's
  "Perbarui berkas…" opens `#replace-dialog` (project.html, filled by app.js from `[data-replace]`) with that link
  pre-filled: download again, or pick a file. A re-download keeps the file's name (links often end in "id.po").
- "Unggah berkas" accepts a file or a URL; URLs are fetched server-side with an SSRF-safe
  `glossary.Fetcher` (`Handler.SetPOFetcher`, upload size limit) and then handled like uploads.
- The status filter is a dropdown of checkboxes (`#filter-status`, `EntryFilter.Statuses`): an entry
  shows when it matches any ticked status; none = all. Status bar counts tick one; Validasi ticks
  Bermasalah, Belum diterjemahkan and Perlu ditinjau.
- Entry load/save deliberately use `fetch` (not htmx): a new load aborts the previous one,
  requests time out after 20 s, navigation during a save waits for it, and a failed save asks
  before moving on. htmx's per-element request queue could otherwise freeze the editor after
  one stalled request. The entry list filter still uses htmx.

## Slow requests

An htmx request still waiting after 1 s covers the page with a half transparent layer and a centred spinner
(`.busy-overlay`, app.js; a manual popover so it also covers open dialogs; label `common.loading` from
`body[data-loading]`). Polling (`hx-trigger="every …"`) and anything inside `[data-no-busy]` never show it; it hides
when the last request ends and after 30 s at most. The editor's entry load/save use `fetch`, not htmx, so they do not
show it.

## Conventions

- Do not mention other translation tools by name in code, comments, docs or commits.
- Keep both languages in sync when adding strings.
- Never use the browser's built-in `confirm()`/`alert()`/`prompt()` (blocked in embedded browsers). Ask with
  `data-confirm` on a form or `hx-confirm` (both open `#confirm-dialog` from base.html; optional
  `data-confirm-ok` button label; actions ending in /delete, /revoke, /remove get a red "Hapus"), or from JS
  with `window.askConfirm(msg, {ok, danger})` / `window.showMessage(msg)` (Promises). Only the
  `beforeunload` leave-page warning stays native: pages cannot replace it.
