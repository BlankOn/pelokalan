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
- **Personal files (historical note):** catalogs have `owner_id`; `CatalogService.Get` returns
  ErrNotFound for other users. Any member can upload into their own workspace
  ("Berkas Anda", `/projects/{id}/files`). Translation memory uses the user's own files.
- **Project files** ("Berkas proyek", `project_files`, migration 027): every glossary import also
  stores its PO files (gzip) with stats, so the Penerjemahan tab has three sections: project files,
  your files, other members' files. Listed least complete first, 10 at a time (`ProjectFilePageSize`,
  "Muat lagi" via `GET /projects/{id}/project-files?offset=`); download, or "Ambil" copies one into
  the member's own files (`catalogService.Upload`). Synced versions imported before 027 count as due
  (`DueFeeds`) and are re-fetched to fill them in; versions uploaded by hand need a re-upload.
  Searchable (every word in file name, language or version label) and filterable by glossary version
  (checkbox dropdown `[data-multi-filter]` with "Tampilkan semua" like the editor filter, shown with 2+ versions;
  `v` params). Search and filter live in `#pf-filter`; "Muat lagi" includes it (`hx-include`).
  Managers get "Sinkronkan" (`POST /projects/{id}/project-files/sync` → `GlossaryService.SyncAll`):
  fetches every version with a sync link in a background goroutine (requests time out after 30 s);
  `GlossaryService.syncing` makes it and the hourly `FetchDue` run one at a time. app.js (`form[data-sync]`)
  spins the button and polls `GET .../project-files/sync` (`SyncState`, per project, in memory) until it is
  done, then shows the result and refreshes the rows.
  A project file the member already has (`CatalogService.MatchOwnFiles`: same language and same file name,
  else same module = name before the first dot) shows "Perbarui" instead of "Ambil": after a confirm it
  replaces that file (`UpdateFromUpstream`: commit before, replace, commit "commit.from_upstream|label"),
  so the history continues. The editor shows "Tarik upstream" before Riwayat when `GlossaryService.UpstreamFor`
  finds a project file for the open file (same name, else same module from the newest version); it posts to
  the same take route with `into` and the same confirm popup.
  Under the "Berkas proyek" heading: last sync time (newest `last_fetched_at` of versions with a sync link,
  in the reader's time zone) and the automatic interval (`Handler.SetSyncInterval`).
- **Template merge only touches files of the same module:** `templateFits` requires at least half of
  a file's strings to be in the template; "Perbarui semua terjemahan" (and template re-upload) skips
  other modules and reports how many it skipped (`MergeResult.Skipped`).
- **Invitations:** stored by email; accepted from `/invitations` after signing in
  with that email (no tokens in links).
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

- Upload (`POST /projects/{id}/glossary/import`, managers) accepts .po, .tar(.gz)/.tgz, .zip.
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
- The sync status above the glossary always describes the newest version with data.
- Every new comment emails all other project members in the background (mentioned members
  get the mention wording).
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

## Thesaurus

- Indonesian synonyms/antonyms (`internal/thesaurus`, table `thesaurus`, migration 024). On start,
  if the table is empty, `THESAURUS_URL` (default: victoriasovereigne/tesaurus dict.json) is
  downloaded with the SSRF-safe glossary fetcher and stored; lookups are served from memory.
- The source repo has no licence (data from Pusat Bahasa's 2008 Tesaurus), so lookups are for
  project members only: `GET /projects/{id}/thesaurus?q=` (JSON). Do not expose it publicly.
- `LookupForms` falls back to meN-/di-/-nya verb forms ("Tampilkan" → "menampilkan").
- Editor: selecting text in the translation box shows a popup (app.js); clicking replaces the
  selection, keeping a leading capital. The /licenses page credits the source.

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
- Uploads: `wiki_files` share the blob store with comment attachments (same image-only, 10 MB
  rules); they are uploaded immediately and Markdown is inserted at the cursor. Blob GC keeps
  blobs referenced by `wiki_files`. Images in `.wiki-body` open in the lightbox.

## Versions and automatic downloads

- `project_versions` (name + version, label "gnome-51") group glossary imports; sorted
  naturally (`service.SortVersions`), newest first. Re-importing a version+language replaces it.
- A version may have `glossary_url`; `GlossaryService.FetchDue` runs every 15 minutes from main.go and
  fetches URLs older than `GLOSSARY_FETCH_INTERVAL_HOURS` (default 3), using ETag/Last-Modified and a
  content hash to skip unchanged archives. `glossary.Fetcher` refuses private/local
  addresses at dial time (SSRF protection) unless `GLOSSARY_ALLOW_PRIVATE_HOSTS=true`.

## Raw editor and translator identity

- `/catalogs/{cid}/raw`: the file built by `CatalogService.Build` in a textarea; saving
  (`SaveRaw`) re-parses it and replaces header + entries. The form carries the catalog's
  `updated_at` (UnixNano); a newer change is refused (`err.raw_conflict`).
- Users have `translator_name/email` and `language_team` (profile page). `Build` sets
  `Last-Translator`, appends/updates the contributor line with `po.AddContributor`, and
  fills `Language-Team` only when the file has none (file properties win).
- `/catalogs/{cid}/properties` edits Project-Id-Version, Report-Msgid-Bugs-To,
  Language-Team and the header comment lines.

## Glossary management UI

- Managers get a "Kelola glosarium" dialog (`partials/glossary_manage.html`) with tabs:
  list (versions, imports, sync link actions) and add (one form: file or URL,
  `POST /projects/{id}/glossary/sources`). `?manage=list|add` reopens it after redirects.

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
- Editor "Terjemahkan AI" button (Ctrl+Shift+A, owner only, project with an AI key):
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

Above the source text the editor lists glossary terms found in the entry (`GlossaryService.ForEntry`,
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
- History page `/catalogs/{cid}/history`: timeline, progress chart (SVG), diff panel (HTMX). It is a full
  page with the editor-style toolbar (back to the editor, PO download); the content scrolls below it.
- The editor's Commit button is disabled while the badge shows no changes (app.js follows the badge,
  which is refreshed out-of-band after saves and commits).
- Diffs compare text, needs-work flag and origin; an AI/glossary/memory translation saved by a
  person without text changes is a "reviewed" item (counted in `catalog_commits.reviewed`).

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
`minmax(0, 1fr)`, and fields use 16px text so iOS does not zoom in.

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
- Plain Up/Down move between entries, also from the translation box (Left/Right stay there);
  in multi-line text they only leave the entry from the first/last line (`arrowNavigates`).
- While a pre-translation job runs (`#ai-job.ai-running`), the editor is read-only (`applyLock` in
  app.js: boxes readonly, controls and editing shortcuts disabled, navigation allowed); when it ends
  the list and current entry reload. "Terjemahkan AI" locks the box with a shimmer (`.ai-filling`).
- "Urungkan" (`#btn-undo`) undoes the last change to the translation: typing bursts and every tool
  (copy source, AI, glossary chip, thesaurus via `window.trBeforeChange`, suggestions) push a snapshot.
- Validation flags `control_chars` and `markup` (tags such as <gui>, <key> differing from the source);
  `ai.UserPrompt` sends markup unescaped, and AI results with control characters are never saved.
- The editor's Bantuan button (and `?` outside text fields) opens `#help-dialog` with all shortcuts;
  update it when adding shortcuts.
- Save buttons: the main one is green with a check and is what Ctrl+Enter presses: "Lepas pratinjau,
  simpan & lanjut" (`#btn-unfuzzy-save`) while the entry needs work, else "Simpan & lanjut" (CSS
  `:has(#btn-unfuzzy-save[hidden])`). Ctrl+Shift+Enter always saves keeping the flag.
- "Unggah berkas" accepts a file or a URL; URLs are fetched server-side with an SSRF-safe
  `glossary.Fetcher` (`Handler.SetPOFetcher`, upload size limit) and then handled like uploads.
- The status filter is a dropdown of checkboxes (`#filter-status`, `EntryFilter.Statuses`): an entry
  shows when it matches any ticked status; none = all. Shortcuts (status bar, Validasi) tick one.
- Entry load/save deliberately use `fetch` (not htmx): a new load aborts the previous one,
  requests time out after 20 s, navigation during a save waits for it, and a failed save asks
  before moving on. htmx's per-element request queue could otherwise freeze the editor after
  one stalled request. The entry list filter still uses htmx.

## Conventions

- Do not mention other translation tools by name in code, comments, docs or commits.
- Keep both languages in sync when adding strings.
- Never use the browser's built-in `confirm()`/`alert()`/`prompt()` (blocked in embedded browsers). Ask with
  `data-confirm` on a form or `hx-confirm` (both open `#confirm-dialog` from base.html; optional
  `data-confirm-ok` button label; actions ending in /delete, /revoke, /remove get a red "Hapus"), or from JS
  with `window.askConfirm(msg, {ok, danger})` / `window.showMessage(msg)` (Promises). Only the
  `beforeunload` leave-page warning stays native: pages cannot replace it.
