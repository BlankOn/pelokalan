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
- **PO files ("Berkas PO") are shared read-only:** anyone who can read the project can open,
  download and view the history of any file; only the owner changes it. `CatalogService.Get`
  sets `Catalog.Owned`; write handlers use `loadOwnCatalog` and write services check
  `c.OwnerID == user.ID`. Read routes live in the public route group, writes in the signed-in one.
  `Build` stamps the translator identity only when the owner exports their own file.
- **Settings** (`/projects/{id}/settings`) are for owners and managers only.
- **Personal files (historical note):** catalogs have `owner_id`; `CatalogService.Get` returns
  ErrNotFound for other users. Any member can upload into their own workspace
  ("Berkas Anda", `/projects/{id}/files`). Translation memory uses the user's own files.
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
  file URL + `?entry=<hash>`). Add a case there to attach discussions to something new.
- The glossary can be filtered by module (PO file, `GlossaryQuery.Module`); the term still shows
  all its translations. The module list (`GlossaryRepository.Modules`) feeds a searchable dropdown
  (`[data-combo]` in app.js).
- The sync status above the glossary always describes the newest version with data.
- Every new comment emails all other project members in the background (mentioned members
  get the mention wording).
- Comments: project members only; readable publicly. `@username` mentions of members are
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
- Thread component `discussion_thread` is used on the term page and lazily in the glossary
  list (`.gl-thread[data-thread-url]`); "Diskusi" tab lists all threads (`?mentions=1`).

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
- A version may have `glossary_url`; `GlossaryService.FetchDue` runs hourly from main.go and
  fetches URLs older than `GLOSSARY_FETCH_INTERVAL_HOURS`, using ETag/Last-Modified and a
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
  encrypted with `ai.Box` (AES-GCM, key from `AI_SECRET_KEY`), prices per 1M tokens, monthly budget.
- `AIService.plan` (service/ai.go): untranslated (optionally fuzzy) entries of the user's own
  catalog. Order of precedence: (1) `ai.Glossary.Exact` — whole string equals a glossary term
  whose top translation has >= 60% share: applied directly, no model call; (2) the model, with
  `ai.Glossary.Matches` (longest phrase first) sent as mandatory term hints.
- `ai.Verify` flags results that miss a required glossary translation or fail `po.Validate`;
  flagged results are always saved as fuzzy.
- Jobs (`ai_jobs`) run in a goroutine, batch by batch (25 items / 6000 chars), record real
  token usage and cost, honor cancel and the monthly budget, and skip entries edited meanwhile
  (entry version check). Running jobs are marked failed on startup (`RecoverJobs`).
- Cost estimate: `ai.EstimateTokens` (~3.5 chars/token) via `AIService.Estimate`.

## Editor glossary section

Above the source text the editor lists glossary terms found in the entry (`GlossaryService.ForEntry`,
all versions, preferred translation + share). Chips insert the translation at the cursor; ↗ opens
the term page. Orange text when nothing matches or the language has no glossary. Term matchers are
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
- History page `/catalogs/{cid}/history`: timeline, progress chart (SVG), diff panel (HTMX).
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
- Entry load/save deliberately use `fetch` (not htmx): a new load aborts the previous one,
  requests time out after 20 s, navigation during a save waits for it, and a failed save asks
  before moving on. htmx's per-element request queue could otherwise freeze the editor after
  one stalled request. The entry list filter still uses htmx.

## Conventions

- Do not mention other translation tools by name in code, comments, docs or commits.
- Keep both languages in sync when adding strings.
