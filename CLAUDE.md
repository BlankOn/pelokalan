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
cmd/server/main.go     # Bootstrap: config, DB, migrations, background jobs, HTTP server
pkg/                   # Reusable libraries; never import internal/
  po/                  # PO/POT parser & writer, MO compiler, validation, plural rules
  gitpatch/            # Rewrite changed entries in a repository's PO file; git format-patch; header credit
  gnomel10n/           # Read-only client of the GNOME translation site (API + release pages, cached, rate limited)
  githubapp/           # GitHub App client (JWT, installation tokens, contents API); githubtest/ = fake for tests
  tpsite/              # Translation Project team pages (parser, file names, robot address)
  glossary/            # Read PO archives in memory, aggregate source/translation pairs, SSRF-safe fetcher
  ai/                  # LLM client (OpenAI-compatible), glossary matching, prompts, verification, estimates
  netsafe/ badgecard/ spelling/ thesaurus/
internal/
  app/                 # Wiring (app.New, app.Migrate) and all routes; *_test.go: HTTP integration tests
  platform/            # config/ i18n/ blobstore/ mailer/ apperr/ (errors shown to users) store/ (sentinels,
                       # SQL helpers) gz/ testdb/ (throwaway database for integration tests)
  models/              # Shared domain types (User, Project, Catalog, Entry …), module naming
  <domain>/            # service.go (+ more logic files), store.go (the storage interfaces it needs),
                       # postgres*.go (their Postgres implementation), *_test.go
    account/           # users, sign-in codes, profile, avatar, patch-link signing
    project/           # projects, members, invitations, invite links, notification settings
    catalog/           # PO files, entries, versions, git patches, accepted upstream work, file order
    source/            # module groups, imports, project files, upstream states, TP, GitHub, glossary
    review/ reservation/ discussion/ wiki/
    modulecontext/     # Konteks notes
    pretranslate/      # AI jobs and single-entry AI
    activity/          # activity log, profile stats, badges (and their emails/share data)
    langtools/         # spelling and thesaurus storage
  web/                 # delivery: handlers (grouped by domain in files), middleware/, templates/
migrations/            # SQL migrations (up/down)
static/                # CSS, JS, htmx, logo
docs/                  # notes (structure-plan.md: how this layout came about)
```

## Common Commands

```bash
make prep           # .env, PostgreSQL in Docker (data volume, free port → DB_PORT), Go modules; safe to rerun
make run            # run server (migrations run on start)
make build
make test           # unit tests (integration tests skip without TEST_DATABASE_URL)
make test-integration  # all tests against a throwaway DB on the make prep PostgreSQL
make check          # gofmt, go vet and every test: run before committing
make cover          # coverage of every package by every test (bin/coverage.html)
```

## Architecture

`web` handlers → domain services → their stores (Postgres) → `models`; constructor injection (`New`, `NewRepository`),
wired in `app.New`. Rules:
- `pkg/*` imports only the standard library and other `pkg/*`.
- A domain imports `models`, `platform/*`, `pkg/*` and lower domains only. Order from the bottom: activity, langtools,
  modulecontext → account → project → reservation, source → catalog → discussion → wiki, pretranslate → review.
  A call that would point upwards goes through a small interface (e.g. `source.LandingDetector`, `activity.UserGetter`)
  or a function set in `app` (`discussion.SetReviews`).
- Each domain defines the storage interfaces it needs in `store.go`; `web` and `app` never touch SQL.
- Errors shown to people are `apperr.New("i18n.key", args…)` (`apperr.UserError`); stores return `store.ErrNotFound` /
  `store.ErrConflict`.
- `make check` (gofmt, go vet, all tests including integration) must pass before a commit.

- **Whitelist:** `config.Whitelist` from `EMAIL_WHITELIST`; empty means open registration
  (`Whitelist.Enabled()` is false and `IsAllowed` accepts any address). Checked on registration,
  login, code verification, every authenticated request (`account.Service.Authenticate`)
  and on invitations.
- **OTP:** `account.Service` stores an HMAC (keyed by `JWT_SECRET`) of 6-digit codes in
  `otp_codes`; codes expire, are single-use and limited by `OTP_MAX_ATTEMPTS`.
- **Public read access:** project list, glossary (`/projects/{id}`, `/glossary`) and
  members are served with optional auth. `project.Service.GetPublic` fills `Project.Role`
  (empty for visitors); handlers use `loadProject` (public) or `loadMemberProject`.
  Member emails and pending invitations are only shown to members/managers.
- **Role changes** (`project.Service.ChangeRole`, members page) email the member in their language (`mail.role_*`: who
  changed it, from which role to which, a button to the project), in the background; nothing when the role stays.
- **Roles:** `owner` (one per project), `manager` (shown as "Koordinator" / "Coordinator": glossary, members, approving and
  sending files to repositories), `reviewer` ("Peninjau", migration 051: translates like a translator and reviews: sees the
  review queue and review editor, leaves review comments, but does not approve or send; `ProjectRole.CanReview`),
  `translator`. Checked in services via `Project.Role`.
- **Project slugs:** `projects.slug` (unique, lowercase/digits/dashes, must contain a letter).
  The `{id}` route segment is the slug; `loadProject` also accepts numeric ids and 301-redirects
  page views to the slug URL. Use `Project.Path()` / `Catalog.URL()` for links.
- **Usernames:** required, unique, `^[a-z0-9][a-z0-9._-]{1,28}[a-z0-9]$`, with reserved
  words that clash with routes (`account/service.go`). Chosen at registration (stored on the
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
- **Deleting a discussion:** the project owner, or its starter (`discussions.created_by`) while every comment in it is
  theirs, can delete a whole discussion with its comments (`discussion.Service.DeleteDiscussion`, `POST .../discussions/{type}/{key}/delete`);
  not on read-only discussions. A deleted topic sends the user back to the Diskusi list.
- **PO files ("Berkas PO") are shared read-only:** anyone who can read the project can open,
  download and view the history of any file; only the owner changes it. `catalog.Service.Get`
  sets `Catalog.Owned`; write handlers use `loadOwnCatalog` and write services check
  `c.OwnerID == user.ID`. Read routes live in the public route group, writes in the signed-in one.
  `Build` stamps the translator identity only when the owner exports their own file.
- **Settings** (`/projects/{id}/settings`) are for owners and managers only.
- **Project language** (`projects.language`, migration 034, backfilled from the most common file/import language;
  "" = not chosen): picked in Settings with a searchable `[data-combo]` of `po.LanguageNames` (code + native
  name, "id - Bahasa Indonesia"; plural rules stay in `po.languages`). `checkProjectLanguage` (`po.SameLanguage`:
  case and -/_ ignored, "pt" ≠ "pt_BR") refuses with `err.language_mismatch`: file uploads (`catalog.Service.Upload`,
  also by link and "Ambil"), module group imports (checked before a group is created), sync links with a chosen
  language, and every downloaded language in `importInto` (so scheduled syncs fail with the message; stored sync
  errors are "key|arg|arg", shown by `errText`). Templates (no language) are always accepted.
- **Personal files (historical note):** catalogs have `owner_id`; `catalog.Service.Get` returns
  ErrNotFound for other users. Any member can upload into their own workspace
  ("Berkas Anda", `/projects/{id}/files`). Translation memory uses the user's own files.
- **Project files** ("Berkas proyek", `project_files`, migration 027): every glossary import also
  stores its PO files (gzip) with stats, so the Penerjemahan tab has three sections: project files,
  your files, other members' files. Listed 10 at a time (`ProjectFilePageSize`; the "Urutkan"
  select in `#pf-filter`, param `s`, `source.ProjectFileSorts`; default "Sisa terjemahan terbanyak" (`remaining`): files with
  work left, most left first, then files nobody started, then complete files; same rule in SQL (`projectFileOrders`) and Go
  (`catalog.CompareProgress`); also least/most complete, most/fewest strings, name, and with reservations on "Direservasi dulu"/"Belum direservasi dulu", which `projectFileOrder` ranks in Go
  after the database order (`ProjectFileOrder.Rank`);
  "Muat lagi" via `GET /projects/{id}/project-files?offset=`); download, or "Ambil" copies one into
  the member's own files (`catalogService.Upload`). Synced module groups imported before 027 count as due
  (`DueGroupSyncs`) and are re-fetched to fill them in; groups uploaded by hand need a re-upload.
  Searchable (every word in file name, language or group label) and filterable by **module group**
  (= `ModuleGroup` with project files, e.g. gnome-51; `group_filter` partial, checkbox dropdown
  `[data-multi-filter]` with "Tampilkan semua" like the editor filter, shown when there is a group; `g` params).
  Search and filter live in `#pf-filter`; "Muat lagi" includes it (`hx-include`).
- The black top bar (`.topbar`) sticks at the top of every page except full pages (`body.full`: editor, raw, history,
  file viewer, which use `100dvh` and scroll inside) (48px, `html { scroll-padding-top }` keeps anchors below
  it); the project tabs (`.tabs-nav`, `top: 48px`) stick right under it on every project page. Each of the three sections
  (`.files-section[data-sticky-section]`) keeps its title, card head, filter bar and column row on top while it scrolls
  (`[data-stick]`, position: sticky); app.js stacks them below the tabs via `--stick-top` from the heights above (re-measured on resize and htmx swaps). They let go when the section ends; off on phones.
  Section titles are buttons (`.section-toggle`, chevron down / right) that fold the section; the state is kept in
  localStorage per project and section (`data-collapse-key` "files:<slug>:project|mine|others").
- "Berkas Anda" and "Berkas anggota lain" (one list, owner under each file) show 5 files at a time
  (`FileListPageSize`, `GET /projects/{id}/my-files|member-files?q=&g=&offset=`, `Handler.fileList`, partial
  `file_rows`) (columns: Berkas, Status Terjemahan, actions; the last edit is the last line under the file name, `files.updated`, and the
  translated · fuzzy · untranslated counts sit under the progress bar in green, orange and red, `file_counts` partial,
  also used by Berkas proyek) with the same "Urutkan" select (`source.FileListSorts`: the project file orders plus "Terakhir disunting" = `recent`, last
  edit first; default `remaining`), search and the same module group filter: a file is in a group when its module
  (`models.ModuleOf`) is among that group's project files (`source.Service.ModuleGroups`); `g=0` = no group.
  Managers get "Sinkronkan dengan upstream" (in Berkas proyek; beside "Tambah berkas manual" in Berkas Anda the same
  button is labelled "Sinkronkan status upstream" / "Sync upstream status", `files.sync_status`, since there it only shows new states;
  `POST /projects/{id}/project-files/sync` → `source.Service.SyncAll`, which ends with `RefreshUpstream`: every
  file state is read again from the release pages, however recent; it also runs for projects without a sync link):
  fetches every module group with a sync link in a background goroutine (requests time out after 30 s);
  `source.Service.syncing` makes it and the hourly `FetchDue` run one at a time. app.js (`form[data-sync]`)
  spins the button and polls `GET .../project-files/sync` (`SyncState`, per project, in memory) until it is
  done, then shows what changed (nothing when no file changed) and refreshes the rows.
  A project file the member already has (`catalog.Service.MatchOwnFiles`: same language and same file name,
  else same module = name before the first dot) shows "Perbarui" instead of "Ambil", which opens the merge popup
  below. The editor shows "Tarik upstream" before Riwayat when `source.Service.UpstreamFor` finds a project file
  for the open file (same name, else same module from the newest module group); it opens the same popup.
- **Pulling upstream merges, never replaces** ("Tarik upstream" in the editor and the review editor, "Perbarui" in
  Berkas proyek; `#merge-dialog`, partials/merge_dialog.html, app.js `[data-merge-upstream]`; `catalog/merge.go`,
  `review/merge.go`, `web/merge.go`). Upstream = the branch's file for repositories Pelokalan writes to (GitHub App),
  else the site's current PO (`UpstreamPO`), else the copy saved at the last sync (`review.Service.UpstreamFile`).
  `GET /catalogs/{cid}/upstream-merge` plans a three-way merge per string against the member's starting point (what
  Pelokalan last committed for it, else the reference version): only upstream changed → upstream's; only the member
  → the member's (text, fuzzy and AI origin); both, differently → a conflict; upstream decides which strings exist.
  Nothing changes until `POST` with the plan's `version` (hash of the upstream file; a different one → `err.merge_stale`,
  "Periksa ulang") and a choice `c_<key>` = mine|upstream for every conflict. Applying saves a version first, then the
  upstream copy (`commit.from_upstream`, the new starting point, so the conflict check of "Setujui & commit" passes
  for the chosen strings), then the result (`commit.upstream_merged|user|kept|conflicts`). The file's owner may do it
  (claims the module), and owners/coordinators for any member (`review.CanMergeUpstream`), who is then emailed. The
  old replace (`UpdateFromUpstream`, take route with `into`) is kept for the API but no longer used by the pages.
  Under the "Berkas proyek" heading: last sync time (newest `last_fetched_at` of module groups with a sync link,
  in the reader's time zone) and the automatic interval (`Handler.SetSyncInterval`).
- **Upstream update status:** for the user's own files (Berkas Anda, `attachUpstreamNew`) and in the editor (badge on
  "Tarik upstream"), `catalog.Service.UpstreamChanges` compares the newest upstream copy (`source.Service.UpstreamData`:
  the project file with the same repository path, else `UpstreamFor`) with the file: strings it does not have, and
  strings whose upstream translation changed since the file's starting point (`reference`) and differs from the
  member's. The member's own unsent work does not count. Shown as a blue chip "⟳ Pembaruan upstream · N" (`.up-new`).
  Pulling is "Perbarui"/"Tarik upstream" (the merge above).
- **Template merge only touches files of the same module:** `templateFits` requires at least half of
  a file's strings to be in the template; "Perbarui semua terjemahan" (and template re-upload) skips
  other modules and reports how many it skipped (`MergeResult.Skipped`).
- **Invitations:** stored by email; accepted from `/invitations` after signing in
  with that email (no tokens in links).
- **Open or private projects** (`projects.open`, migration 037; private by default): chosen under "Keanggotaan" in
  Settings → Umum (`access` = open|private). Visitors see no button in the project header (the sign-in notice under the tabs asks them). Signed-in non-members see "Gabung" in the project header on open projects
  (`POST /projects/{id}/join` → `project.Service.JoinOpen`, joins as translator and returns to the same page) and
  "🔒 Privat" on private ones; a pending email invitation to the project is accepted instead (its role wins), so it also
  works on private projects. Private projects refuse with `err.project_private`.
- **Invite links** (`project_invite_links`, migration 018): owners/managers create shareable
  links on the members page with a role, expiry (1/7/30 days or none) and a use limit
  (`project.InviteLinkDays/Uses`). `/join/{token}` is public: visitors are sent to
  login/register with `next=/join/{token}` and come back to confirm with a POST. `JoinByInviteLink`
  counts the use and adds the member in one transaction (limits hold under concurrency); existing
  members are not counted. Revoking deletes the link.
- **Catalogs:** a project has at most one template (`is_template`, empty language)
  and one catalog per language. Entries store `msgstr` as a JSON array; fuzzy is a
  separate column, other flags are comma separated.
- **Concurrency:** entries have a `version`; `UpdateEntry` only succeeds when the
  submitted version matches (`ErrEntryConflict` otherwise).
- **Errors shown to users:** `apperr.UserError{Key}` where Key is an i18n key.

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
  `discussion.Service.ResolveSubject` / `SubjectURL`: `glossary_term` (key = term hash) and
  `po_entry` (key = "<catalog id>-<Entry.KeyHash()>", stable across restores/merges; title "owner/file — source
  text", migration 052 turned older titles around; page =
  file URL + `?entry=<hash>`) and `topic` (free discussion: key = random 12 hex chars, title
  stored on the discussion, page `/projects/{id}/topics/{key}`, started from the Diskusi tab via
  `CreateTopic`, which stores nothing if the first post is invalid). Add a case there to attach
  discussions to something new.
- A `po_entry` discussion whose PO file was deleted stays reachable on its own page
  (`/projects/{id}/threads/{type}/{key}`, `ThreadURL`): `ResolveSubject` returns it with `ReadOnly`, so it can
  be read by everyone but nobody can comment, edit or delete comments there (`ErrDiscussionClosed`).
- The glossary can be filtered by module (PO file, `GlossaryQuery.Module`); the term still shows
  all its translations. The module list (`source.Repository.Modules`) feeds a searchable dropdown
  (`[data-combo]` in app.js).
- The sync status above the glossary always describes the newest module group with data.
- New comments email other project members in the background, filtered by each member's
  notification settings (`notification_settings`, migration 030; Pengaturan tab, visible to every member,
  `POST /projects/{id}/notifications`). A member gets at most one email per comment, sent when any reason that
  applies to them (`models.Notify*`: mention, my_file, started, participated, new_topic, other_topic,
  other_glossary, other_entry; `discussion.Service.reasons`, strongest first) is on; "muted" turns off all
  project email. Everything is on by default (kinds are stored as "off"). Emails say which setting caused
  them and link to the settings. Sign-in codes and invitations are always sent. Email buttons use
  `emailButton` (small).
- Comments: project members only; readable publicly. Deletion is soft (`deleted_at`, `deleted_by`): the
  author, username and time stay visible with a note saying who deleted it and when. `@username` mentions of members are
  stored and emailed. Typing "@" in a comment box opens a small member popup (app.js, the thread's `data-members`;
  arrows, Enter/Tab picks, Esc or typing on ignores it), so a plain "@" stays possible. Authors edit/delete their own; only the project owner deletes someone else's. "Sunting"/"Hapus" sit below the comment (`.comment-actions`, shown on hover; always on touch screens). Bodies are
  Markdown rendered by `commentHTML` (goldmark GFM + hard wraps, safe mode: raw HTML and
  javascript: links dropped; bare URLs auto-linked; mentions outside a/code/pre).
- Project descriptions are plain text; `linkify` escapes them and links bare http(s) URLs.
  Project cards use a stretched link (`.project-card-link::after`) so description links stay clickable. Members' cards
  on /projects have a "Kontribusi" button beside the role badge (to `files#my-files`, Berkas Anda).

## Comment attachments

- Files live in `internal/platform/blobstore` (S3-like `Store`: Put/Get/Delete/GC), implemented on
  Postgres (`blobs`, content-addressed by SHA-256, deduplicated). `attachments` rows link a
  comment to a blob (migration 017). Only images are accepted (png/jpeg/gif/webp, detected from
  the content); limits: 5 images, 10 MB each (`discussion.MaxAttachments*`). The client checks the
  same limits; images can be picked, pasted or dropped anywhere on a `.thread`.
- Clicking an image opens a lightbox (`dialog.lightbox`, app.js): fit to screen, click for the
  original resolution, arrows step through the comment's images.
- Served at `/projects/{id}/attachments/{aid}/{filename}` with a sandbox CSP, immutable cache
  and ETag; only png/jpeg/gif/webp are shown inline, everything else downloads as octet-stream.
- Deleting a comment removes its attachment rows; `blobs.GC` (hourly) deletes unreferenced
  blobs older than 24 h. Swap the `Store` implementation to move files to real object storage.
- The Diskusi tab is searchable (`DiscussionFilter.Query`): every word must appear in the title or a
  visible comment (ILIKE). Each discussion previews its newest visible comment (when searching, the newest matching one),
  as plain text (`plainMarkdown`, 180 characters). If it gets slow, add a pg_trgm
  or full-text index rather than a separate search service.
- Thread component `discussion_thread` is used on the term page and lazily in the glossary
  list (`.gl-thread[data-thread-url]`); "Diskusi" tab lists all threads (`?mentions=1`).

## Konteks

- One shared note per **module** of a project (`module_contexts`, migration 028; module = file name
  before the first dot, lowercase, `models.ModuleOf`): every member's file of the module shows the
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
- **General items** (migration 054, `module_contexts.title`): a Konteks item that belongs to no module (style, agreed terms…).
  Stored as a row whose module is "~" + 12 random hex chars (`ModuleContext.IsGeneral`/`GeneralKey`), with a title
  (required, at most 120). Members add them with "+ Konteks baru" beside the search box on the Konteks tab (a popup, `#context-new`) (`POST /projects/{id}/contexts`,
  `modulecontext.Service.CreateGeneral`) and edit them in a popup per item ("Sunting", `#ctx-edit-<key>`; `POST /projects/{id}/contexts/{key}`, `SaveGeneral`, carries
  `version`; a stale save answers 409 and reopens the popup with the person's text and the message). Emptying the note, or the red "Hapus" after Sunting (`POST …/contexts/{key}/delete`, `DeleteGeneral`, asks first), hides
  the item; its edits stay recorded.
  Not sent to the AI yet (only the file's module note is).
- AI pre-translation puts the file's note into the system prompt (`ai.SystemPrompt(..., fileContext)`,
  capped at `ai.MaxFileContext`); it counts in the cost estimate. The note outranks the glossary for
  the words it talks about: glossary terms containing a word the note writes in quotes or backticks (3+ letters,
  `ai.ContextWords`/`ai.Mentioned`; plain sentences and examples do not count) are sent as `optional` hints (not enforced by `ai.Verify`), and
  whole-string glossary matches containing such a word go to the model instead of being applied.
- Project tabs carry monochrome line icons (`tab_icon` in project_header.html).
- The project header shows the overall progress of all modules in the project files after the name (and AI badge):
  `source.Service.ProjectProgress` → `ProjectFileTotals` counts each module per language once, from its newest copy;
  filled by `loadProject` on full pages (`Project.Progress`), and on each card of /projects (`renderProjects`).
- Statistik / Stats tab (`/projects/{id}/insight`, before Anggota, `InsightPage` in insight.go, pages/insight.html):
  the module group chart (moved here from Penerjemahan) and "Aktivitas anggota" (public, like the rest of the
  project): totals and a per-member table. Above the module chart, "Pertumbuhan terjemahan" (`wordschart.go`,
  `buildWordsChart` from `activity.Repository.HourlyTranslated`): **strings** people saved with a translation, added up
  hour by hour (one point per hour; quiet stretches of more than a day, `wcQuiet`, are compressed to a fixed-width
  dashed break `.wc-gap` whose tooltip gives its length, so active periods fill the width; labels sit where active
  stretches start; times are shown when the span is 3 days or less). Four lines on one scale, all counted in the green total: typed by hand (blue; empty →
  translated, no machine origin), AI drafts saved unchanged (purple; origin ai, text unchanged), AI drafts edited before
  saving (orange), existing translations edited (teal; translated, no machine origin, text changed, every such save
  counts). Glossary/memory drafts are not counted, and AI drafts nobody saved are not logged. Hovering shows the nearest
  hour in the same floating card as the module chart
  (app.js handles `.group-chart` and `.words-chart`; the card's rows come from `.gc-tip[data-rows]`).
- Activity log (`activity_events`, migration 033, append-only, no FK on catalog/entry so history survives):
  `activity.Service.Record` (errors only logged, nil-safe) is called from `catalog.Service.SaveEntry` (`EntrySaved`:
  status before/after, machine origin before, text changed, source word count), manual commits, comments
  (`discussion.Service.post`) and sign-ups. Services get it via `SetActivity`. Counting rules (same in the view
  `activity_daily` and `activity.Repository.Summary`): translated = untranslated → anything, reviewed = fuzzy →
  translated, edited = translated with text changed, drafts approved/fixed = origin set and text unchanged/changed.
  Add a `models.Activity*` kind and a Record call to log something new.
- Module group progress chart (Statistik tab; `group_chart` partial, `groupchart.go`): `group_stats`
  (migration 031) records each group's project file totals per language at every 15-minute check when they
  changed, and at least hourly (`source.Service.RecordStats`, `StatsInterval`), plus after uploads. Groups are
  chosen with the same `group_filter` dropdown (none = all; several are added up per hour, `sumByHour`);
  ranges Hari ini (last 24 h, hourly points) / 7 / 30 days / all (daily points, `lastPerBucket`). Lines (translated green, fuzzy orange, untranslated red) share one scale with tidy value labels by default (`niceStep`), so distances are real; "Skala per garis" (`cs=each`) zooms each line to its own range and shows [min–max] in the legend; a
  floating card (app.js) shows the nearest point. History starts on the day it was deployed.
- The "Templat sumber (POT)" section on Penerjemahan is hidden for now (`showTemplateSection` in web/project.go);
  its template upload/merge code stays so it can come back.
- Penerjemahan (`/projects/{id}/files`) is the first tab and the landing page: `/projects/{id}` redirects
  there (`ProjectHome`, query kept); the glossary lives at `/projects/{id}/glossary`.

## Thesaurus

- Indonesian synonyms/antonyms (`pkg/thesaurus`, table `thesaurus`, migration 024). On start,
  if the table is empty, `THESAURUS_URL` (default: victoriasovereigne/tesaurus dict.json) is
  downloaded with the SSRF-safe glossary fetcher and stored; lookups are served from memory.
- The source repo has no licence (data from Pusat Bahasa's 2008 Tesaurus), so lookups are for
  project members only: `GET /projects/{id}/thesaurus?q=` (JSON). Do not expose it publicly.
- `LookupForms` falls back to meN-/di-/-nya verb forms ("Tampilkan" → "menampilkan").
- Editor: selecting text in the translation box shows a popup (app.js); clicking replaces the
  selection, keeping a leading capital. In the review and view-only layouts and the project file viewer the table's text can be
  selected (`.review-mode .entry-row { user-select: text }`; a click that ends a selection does not pick the row), and
  a word selected in a translation shows the same popup for members, read-only: clicking a synonym copies it. The
  pages carry `thesaurus_attrs` (partial) only for the owner or for members. The /licenses page credits the source.

## Spelling check (Indonesian)

- `pkg/spelling`: `Parse` reads the KBBI v6.1.0 CSV extract (`SPELLING_URL`, default the aryakdaniswara/kbbi-v6-full-csv
  repo; columns kata, turunan, varian, gabungan_kata) into ~119k single words; only words are kept, never meanings.
  `Dict.Known` adds the forms KBBI leaves out: passive di- and bare imperatives from the meN- forms it lists
  ("ditampilkan", "Tampilkan"), clitics (-nya -lah -kah -pun -ku -mu, ku-/kau-), root + -kan/-i/-an, doubled and
  hyphenated words. `Words` skips placeholders, markup, entities, URLs/addresses, mnemonics (`_Berkas`), digits,
  acronyms and words with inner capitals; `Misspelled` also leaves words that appear in the source alone.
  `Suggest` = edit distance ≤ 2 (swaps count once), longest shared start first.
- Tables (migration 041): `spelling_words` (filled once on start when empty, like the thesaurus; `langtools.SpellingService.Load`)
  and `spelling_extra` (a project's own words: "Kamus proyek"). The source has **no licence**: checks are for project
  members only (never public), and /licenses credits the source.
- Only Indonesian files (`langtools.Indonesian`) of signed-in members. Unknown words are **not** a problem in Validasi
  (no issue, no warning box, not counted in "bermasalah"): the underline and its popup are the only marks.
- Editor (owner's files): the browser's spellcheck is off (`spellcheck="false"`). app.js posts the boxes' text to
  `POST /catalogs/{cid}/spell` (debounced) and draws `.spell-layer` behind each transparent `.tr-input` (metrics
  copied, scroll mirrored; re-placed when the box or its container resizes, so it follows the box when AI alternatives or
  warnings appear above it) with unknown words in `.typo` (red wavy underline). Clicking inside one opens `.spell-pop`
  (`GET /projects/{id}/spell/suggest?w=`): suggestions replace the word keeping its case (undoable), and
  "Tambahkan ke kamus proyek" posts `POST /projects/{id}/dictionary` (any member). Settings lists the words
  (`#dictionary`, every member); owners/managers remove them (`/dictionary/delete`).

## Public API

`internal/web/api.go`, routes under `/api/` in the public group: read-only JSON, no key, the same answer
signed in or not, `Access-Control-Allow-Origin: *`, `Cache-Control: public, max-age=60` (`apiJSON`).
`GET /api/projects/{id}/members` (slug or numeric id; `APIMembers`): project slug, name, URL (`SITE_BASE_URL`), and
members with username, name, **sign-in email** (the operator chose to publish it here, although the members page
shows it only to members), role, joined_at. Documented in README.md.

## Module reservations

- Project setting "Reservasi modul untuk dikerjakan" (Settings → Umum, `projects.reservations` + `reservation_days` 0/14/30/60, default off/30; migration
  046, `module_reservations` with one active holder per project + module by a partial unique index). Unit = module
  (`models.ModuleOf` / `ModuleKey`, the file name before the first dot). `reservation.Service` (reservation/service.go):
  `Claim` (free → yours; held by another → `ErrModuleReserved`), `Check`, `Release` (holder, or owner/manager who
  releases someone else's → the holder is emailed), `ReleaseMerged`, `Expire` (hourly in main.go: reminder email
  `ReminderDays` (3) before, released after `reservation_days` without saves; last save = newest `updated_at` of the
  holder's files of the module).
- Hard lock: content changes go through `loadEditableCatalog` (web/reservation.go: SaveEntry, AI, pre-translate,
  raw, properties, template update, replace, restore, discard), which claims a free module and refuses a held one.
  `catalog.Service.Upload` (take, manual add, link) and "Perbarui" claim too. Commit, messages, delete, downloads and
  the spell check do not. The editor/raw/entry pane call `applyLock`: a module another member holds opens read-only
  (`Owned` false) with a banner (`editor-hold`; the holder sees `editor-hold-mine` with "Lepas").
- Released as "merged" by `catalog.Service.landingsIn` after each sync (GNOME archive or GitHub branch) when every string
  the holder changed is upstream word for word. Lists: a red lock (`res_lock`, `.res-lock`: red circle, white lock; tooltip "Direservasi oleh @user") right after the file name (blue `.res-lock-mine` when the viewer holds it) and `res_chip` under it, "Reservasi modul" (takes the file; asks first: it reserves the module) / "Sedang direservasi"
  in Berkas proyek, "Reservasi modul" / "Lepas reservasi tugas" in the ⋯ menu, "Reservasi & Terjemahkan" instead of "Terjemahkan" on free modules in Berkas Anda (asks, reserves, opens the editor), `reserve_filter` (`rv` = mine, other, free; Berkas
  proyek uses `source.Service.ProjectFiles` with a keep filter, `KeepStates` for upstream states). Owners/managers release
  someone else's reservation with a "Lepas reservasi" button (`res_release`, holder emailed) in the action column of Berkas proyek
  (instead of "Sedang direservasi") and Berkas anggota lain, and from the ⋯ menu ("Lepas reservasi @user"); Settings no
  longer lists held modules. In the ⋯ menu the review items (Ajukan/Lihat/Batalkan tinjauan, Kirim ke repositori) follow
  the reservation items.

## Profile page

`/profile` (pages/profile.html) is one form with one "Simpan" beside the title (`account.Service.UpdateAccount` checks
every field first and saves only when all are valid): Akun, Identitas penerjemah, Profil publik and Email lencana, in
two columns from 1000px, one full-width column below (iPad). Profil publik: a short description (`users.bio`, `MaxBio` 300) and links (`users.links`, JSON
kind → URL, migration 053; `models.LinkKinds` website, github, x, linkedin, mastodon). `account.NormalizeLink` accepts
a handle or an address and stores the full https address (else `err.link_invalid`). The public page shows them under
the name (`user-bio`, `user-links`, `link_icon`). The old `/profile/identity` and `/profile/badge-emails` routes stay.

## Profile pictures

- `users.avatar` (migration 055) = blob key of a 256×256 JPEG in the Postgres blob store ('' = none: the initial shows).
  Profile page (Profil publik): "Pilih foto…" opens `#avatar-dialog` (app.js): a square canvas with a round mask, drag to
  move, slider / wheel / arrow and +/- keys to zoom; "Simpan foto" uploads a 512px JPEG (`POST /profile/avatar`, multipart
  `avatar`, JSON answer) and swaps every avatar on the page; "Hapus foto" (`POST /profile/avatar/delete`, asks first).
  Saved right away, apart from the profile form's Simpan.
- The server never trusts the upload: `account.MakeAvatar` checks size (10 MB) and dimensions before decoding (JPEG, PNG,
  GIF, WebP), cuts a centred square, scales with CatmullRom and encodes JPEG q85 (`account.Service.SetAvatar`). The old blob is
  left to `blobs.GC`, which keeps blobs referenced by `users.avatar`.
- Served publicly at `/avatars/{key}.jpg` (`Handler.Avatar`; immutable cache, ETag, sandbox CSP) only while some user uses
  that key, so the address never serves other blobs. "avatars" is a reserved username.
- Shown with the `avatar` partial (dict Key, Name, Class; template func `avatarURL`): top bar, comments
  (`Comment.AuthorAvatar`), members page, and the public page head.

## Public profiles and badges

- `/users/{username}` (public, `Handler.UserPage`, pages/user.html; "users" is a reserved username): a person's work
  across all projects, never their email. Linked from the user menu ("Halaman publik saya"), members page, Statistik
  table and comment authors. Data: `activity.Repository.UserDays` (per calendar day in `SITE_TIMEZONE`, default
  Asia/Jakarta; `siteLocation`; main.go embeds time/tzdata) and `UserTotals` (projects, files, languages, completed
  files, comments, topics, wiki/Konteks edits, manual commits) in activity/postgres_profile.go. A **contribution** =
  an entry save that translated, reviewed (fuzzy/AI → translated), changed a translated string, or saved a machine
  draft (`evContribute`, the Statistik rules); streaks count days with at least one. Words ("Kata dikerjakan", the
  Statistik "Kata" column and the Juru Kata badge) are the source words of every contribution; "String diterjemahkan dari
  kosong" counts only strings filled from empty.
- `activity/profile.go`: `ComputeProfileStats` (current/best streaks of days, Monday–Sunday weeks and months; the period
  not over yet does not break a streak; active days, best day, night 00–05 / early 05–07, weekends, member days) and the
  badge catalog `Badges`: groups (streak, work, reach, community, habits) of badges with up to 5 tiers
  (`BadgeTierNames` bronze…diamond; strings `badges.*`, `badge.<key>` + `badge.<key>_desc` with the target).
  Add a badge there (plus strings and a `badge_icon` case). Completed files = own files the person worked on that are
  fully translated, deleted ones included (a soft delete never takes a badge away). The page: head with streak, tiles, a 53-week calendar (`buildHeatmap`, levels `heatLevels`),
  badges (earned first, pips, progress to the next tier) and projects. Work before the activity log began is not counted.

- **AI and hand-work badges** (group "work"): "Mitra AI" / "AI Partner" (`ai`, robot icon) counts AI drafts the person
  reviewed and saved (`evAI`: entry saves whose machine origin was ai); "Racikan Tangan" / "Handcrafted" (`manual`, keyboard
  icon) counts empty strings translated by hand with no AI, glossary or memory draft (`evManual` = `evNew` with no origin).
  Both come from `ProfileDay.AI` / `Manual` and have badge details like the other counting badges.
- **Medals:** `partials/badge_medal.html` (`badge_medal`, `medal_defs` gradients): an earned medal has a tier-coloured rim,
  shaded face and white icon; an unearned badge is an empty dashed slot. Icons live once in `badgecard.Icons`
  (template func `badgeIcon`); tier colours in `badgecard.Tiers` and the `.tier-*` CSS must stay in step.
- **Sharing:** every earned tier has a page `/users/{username}/badges/{key}/{tier}` (`BadgeSharePage`; the tier is in the
  address so a shared Silver stays Silver; not or no longer earned → back to the profile) with Open Graph / Twitter tags
  (`pageMeta` in `Data.Meta`, base.html) and a 1200×630 PNG at the same address + `.png` (`pkg/badgecard`: gg + Go
  fonts, cached in memory, `Cache-Control` one day). Preview texts are in the owner's language (`ownerLang`); achievement
  lines are `badge.<key>_done`. The owner sees a share button on earned badges (`#share-dialog`, app.js: LinkedIn,
  X, Facebook share URLs, "Salin ke papan klip" (the post text, a blank line, then the link), "Unduh gambar" (`.png?download=1`, to attach the picture by hand: X often draws
  the card only after posting), and Web Share, which attaches the PNG as a file where the device can). The profile's own
  preview uses its highest badge's image. `chimiddleware.GetHead` answers HEAD like GET for crawlers.
  Image addresses carry `?v=` (a hash of the card's text) so renamed badges never show a cached old picture.
- **Veteran** = a member for N years (1/2/3/5) with work in every month of an unbroken run of N years: counted in
  months (`min(BestStreakMonths, MemberMonths)`, tiers 12/24/36/60); `BadgeDef.Div` shows thresholds in years and
  `Unit` adds "bulan" to the progress count. Badge defs use `ID` (not `Key`: the i18n test reads `Key: "…"` literals).

- **Accepted upstream** (`upstream_landings`, migration 043; catalog/landing.go): after every module group import (sync or
  upload), `source.Service` hands the downloaded files to `catalog.Service.DetectLandings` in the background
  (`SetLandings`). For each member file of the same module (git: same `repo_path`; else same name or module, same
  language) a string counts when the member wrote it (it differs from the newest "initial"/"from upstream" snapshot,
  the git patch reference), it is translated, not fuzzy and not an unreviewed AI draft, it is long enough to tell apart
  (`landingWorthy`: 3+ words or 15+ characters), and the new upstream file has exactly that translation. One row per
  member, file and string (insert once). Works the same for GNOME (release archives follow the modules' git) and
  GitHub branches, however the work got in. Badges `landed` / `landed_modules` (group "upstream"), tile "Diterima upstream".

- **Badge details** on the share page (`activity.Service.BadgeDetail`, activity/badgedetail.go): when the tier was reached
  and the work behind it per project and module (`activity.Repository.UserModuleWork`; module = file name before the first
  dot, deleted files keep their name). Streak badges use the run that earned the tier (`earnedPeriod`: days, Monday–Sunday
  weeks, months), counting badges the work up to the day the count was reached, community badges per project
  (`UserCommunityWork`, date from `NthTime`), upstream badges the accepted modules (`UserLandingWork`).
- **One-time jobs** (`one_time_jobs`, migration 044): `ClaimOneTimeJob` marks a start-up job as run. The first one,
  `BackfillLandingsJob` (main.go), recorded work accepted upstream during the month before detection existed
  (`catalog.Service.BackfillLandings`: stored project files, member work measured against their starting point a month
  earlier via `referenceAt`, dated when that upstream file was stored). It can be removed once it has run.

- **Badge emails** (activity/badgemail.go, `badge_awards` + `users.badge_emails_off`, migration 045): badges are computed,
  so `activity.Service.AwardBadges` (main.go: once silently as `BadgeAwardsBaselineJob`, then hourly) compares everyone
  with work (`BadgeUsers`) against the tiers recorded in `badge_awards` and emails the new ones in one message per person
  (`badgeMessage`: picture, achievement, "Lihat lencana", LinkedIn/X/Facebook share links, encouragement to share, link
  to turn them off). A tier is recorded once and never announced again, even if the count drops and rises. Recorded
  silently when the person turned them off (profile page `#badge-emails`, `POST /profile/badge-emails`) or is no longer
  whitelisted. Share texts and links come from `activity.BadgeShareFor` (also used by the share page and dialog).

## Translation Project (translationproject.org)

- The site has no API. A module group can be synced from a **team page** (`/team/<code>.html`): "Tim Translation
  Project" in the add form (`source=tp`, `tp_team` = code or team page address, `tpsite.ParseTeam`); stored in
  `module_groups.tp_team` (migration 047), sync URL = the team page, sync language = the team code.
- `pkg/tpsite`: `ParseTeamPage` reads the domain table with regexes only (untrusted text): domain, version, the
  team's PO file for that version (`PO-files/<team>/…`, already merged with the newest template) or the POT when
  untranslated, translated/total, last submission, assigned translator, restricted (disclaimer needed). Fixture
  `testdata/team_id.html`; if the site changes its markup, save a new copy and adjust the regexes. Do not use
  `latest/` or `backups/` (they hold the last submission, often for an old version). `Prepare` turns Latin-1 into
  UTF-8 and a POT into an empty PO of the team's language (Language, Plural-Forms, plural form count).
- `source.Service.fetchTP` (source/tp.go): team page with ETag; files are downloaded only for rows whose
  `Fingerprint` changed (version, file, counts, date), `tpPause` apart; the rest come from `tp_domains` (shared by
  every project, file kept gzip). All files go into one in-memory .tar.gz (`tpsite.FileName`: "bash.id.po", module =
  domain) handed to `importInto`, so glossary, project files, Ambil/Perbarui and upstream landings work as for other
  sources (terms capped at `GitTermMaxWords` like git groups). A failed download keeps the last good copy and makes
  the next round read the page again. Adding a TP source and "Ambil sekarang" run in the background
  (`fetchInBackground`, flash `feed_started`); the popup hides "Ubah tautan" for TP groups.
- Display: `UpstreamInfo.TP` (`attachTP`, called by `AttachUpstream`/`AttachUpstreamToCatalogs`; module + same
  language as the team) → `tp_badge` ("TP <version> · <team %>", tooltip with counts, last submission, translator,
  disclaimer). The editor's "Modul" opens the domain page.
- Sending back: the robot only accepts email from the assigned translator's own address, so Pelokalan never sends.
  The ⋯ menu of Berkas Anda has "Unduh untuk Translation Project" (`GET /catalogs/{cid}/tp`, owner only, `TPSubmit`:
  the built file named `<domain>-<version>.<team>.po` with that Project-Id-Version) and a `mailto:` to
  robot@translationproject.org with that subject (the translator attaches the file).

## Git-only projects

- When any module group of a project is synced from GitHub (`source.Service.HasGitRepo`), Penerjemahan hides
  "Tambah berkas manual": files come from the repository. (`IsGitProject`, every group from GitHub, still hides the
  template section.)

## Home page introduction

`/projects` starts with a landing introduction (`#home-intro`, pages/projects.html, `home.*` strings): an illustration
right above the card (`static/img/home-hero.png`, transparent background, `.home-art`), headline, pitch,
buttons (visitors: sign up / sign in; members: jump to the list), six feature cards (`homeFeatures`, badge
icons), the connected sources (GNOME l10n.gnome.org, GitHub repositories and the Translation Project, `home.src_*`) and three "how it works"
steps. It is always shown (no hide button).

## Footer

`layouts/base.html` (a fine line on top, `.site-footer` border-top): "Sebuah proyek dari" / "A project by" with the BlankOn Foundation logo (linking to blankon.id) on
top, then the site name and tagline and the Tentang/Lisensi links. The logo (`static/img/blankon-foundation.png`, the light version: dark letters) shows when
`static/img/blankon-foundation.svg` (or `.png`) exists
(`PageData.ByLogo`, checked at start-up); otherwise the name is written in text.

## About page

`/about` (footer link "Tentang", before Lisensi; `AboutPage`, pages/about.html, `about.*` strings) explains the
goals (easy upstream contributions for newcomers, letting contributors focus on reviewing) and that the
project is funded by the BlankOn Foundation, and links the source code (https://github.com/BlankOn/pelokalan, MIT License, see LICENSE). The Pendanaan card ends with a donation call
and a button to the foundation's donate page (https://blankon.id/id/donate, or /en/donate in English).

## Licenses page

`/licenses` (footer link) lists third-party software and data (`licenseCredits` in
web/licenses.go). Keep it in sync with go.mod and static/ when adding dependencies.

## Wiki

- `wiki_pages` (migration 019): Markdown pages per project, nested at most `models.WikiMaxDepth`
  (3) levels via `parent_id`, ordered by `position`. Slugs are unique per project, derived from the
  title on create (`wikiSlug`) and can be edited; old addresses keep redirecting
  (`wiki_redirects`, migration 020). "new", "files", "preview" are reserved.
- Every page has "Bagikan" (copies the page address, `data-copy-here` on the shared `[data-copy]` handler in app.js,
  with a hidden-field fallback where the clipboard API is missing).
- Readable publicly; members create/edit; owners, managers and the page author delete (pages
  with subpages cannot be deleted). Edits carry the page `Version()`; if someone saved in
  between, the save is refused (409) and the editor keeps the user's text with steps to resolve:
  copy it, reload the latest version, edit again. It never overwrites. Tree moves are
  last-write-wins.
- Every create/save is recorded in `wiki_edits` (migration 021, backfilled from created/updated);
  the article lists the latest 50 as "@username · time" in a collapsed `<details>`, converted to the reader's time zone
  (`time[data-local]` in app.js). Moves are not edits.
- The tree is reorganized only by drag and drop in the sidebar (`wiki.Service.Move`, modes
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

- `pkg/gnomel10n` reads the public read-only API of l10n.gnome.org (`/api/v1/releases/`,
  `/releases/{release}/languages/{lang}`, `/modules/{m}/branches/{b}/domains/{d}/languages/{lang}`) over
  `netsafe`. Write actions (reserving, uploading) need a signed-in session there and are out of scope.
- The site throttles anonymous API clients (~100 requests/hour per address, then a 429 for most of an hour), and
  the API has no endpoint returning many modules' states at once. So **pages never ask the site for states**: they
  are stored in `upstream_states` (migration 035; shared by all projects, key release + language + file name stem;
  `listed = false` = the release has no such file) and read from there.
- `source.Service.WarmUpstream` (source/upstream.go; at start-up before the first `FetchDue`, then at the end of
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
- Fixtures: `pkg/gnomel10n/testdata` has recorded API JSON and a saved release page
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
  `source.Service.ProjectFiles` loads all matching rows and pages in Go) and in "Berkas Anda" / "Berkas anggota
  lain" (`file_filter`, `fileList`). Shown when a module group is a release upstream (`HasUpstream`). Both lists show the
  badge (`upstream_badge` partial) under the file name.
- The module group filter is hidden when the project has only one module group (file lists and the Statistik chart).
- "Berkas Anda" order with "Terakhir disunting" (`fileList`, `ownFileRank`): upstream state "ToReview" (labelled "Requested rework" /
  "Diminta perbaikan"), then Proofreading, then Translating, then everything else; last edit (newest first)
  within each group, also across "Muat lagi" pages and with search/filters. "Berkas anggota lain" sorts by last
  edit only.
- **A file's own upstream page** (`catalogs.upstream_page`, migration 038): "Tambah berkas manual" and "Perbarui berkas…"
  have an optional "Halaman modul upstream" field (`upstream_page`, e.g. `…/vertimus/gnome-calculator/main/po/id/`;
  checked and made canonical by `source.Service.UpstreamPageURL` via `gnomel10n.ParsePageURL`, else
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
  "Tarik upstream", same take route) first download the current `po_file` from the site (`source.Service.UpstreamPO`: the stored `po_file`, looked up
  live only when the file was never checked;
  upload size limit; history label "l10n.gnome.org module/branch") and fall back to the copy saved at the last sync
  when the site has no entry or cannot be reached (throttled, down).
- A file with a release but no stored state shows a grey dashed "Status belum ada ↗" label (`.up-unknown`) linking
  to the page built from its stem (`source.Service.guessPageURL`, `UpstreamInfo.UpstreamLink`), to check by hand.
  Colours: Translating orange, Requested rework (ToReview) red, None/unknown grey.
- Tests use API JSON and the release page recorded from the site (`pkg/gnomel10n/testdata`); never call the site from tests.

## Module groups ("Kelompok modul") and automatic downloads

- Formerly "glossary versions"; renamed everywhere in migration 039 (tables `module_groups`, `module_group_imports`;
  columns `group_id`, `sync_url`, `sync_language`) and in code (`models.ModuleGroup`, `ModuleGroupImport`,
  `ModuleGroupOption`; `GroupID`/`GroupLabel` fields; `ListGroups`, `ImportGroup`, `SetGroupSync`, `FetchGroup`…).
  A module group (name + version, label "gnome-51") holds the PO archives imported per language; they feed the
  glossary terms and the project files. Sorted naturally (`source.SortGroups`), newest first. Re-importing a
  group + language replaces it.
- A group may have a `sync_url`; `source.Service.FetchDue` runs every 15 minutes from main.go and
  fetches URLs older than `GLOSSARY_FETCH_INTERVAL_HOURS` (default 1), using ETag/Last-Modified and a
  content hash to skip unchanged archives. `glossary.Fetcher` refuses private/local
  addresses at dial time (SSRF protection) unless `GLOSSARY_ALLOW_PRIVATE_HOSTS=true`.

## Raw editor and translator identity

- `/catalogs/{cid}/raw`: the file built by `catalog.Service.Build` in a textarea; saving
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
  for the file owner, validation (`validateEntry` in catalog/service.go, `po.CheckCredits`) adds
  `credits_missing` when their name/email is not in it and `credits_year` when their line lists years but
  not this one (lines without years are fine). The editor alert offers "Tambahkan identitas saya" / "Tambahkan
  tahun ini" (`creditFix` → `po.AddCredit`, same year rule), which fills the box (undoable, not saved).
- `/catalogs/{cid}/properties` edits Project-Id-Version, Report-Msgid-Bugs-To,
  Language-Team and the header comment lines.

## Git repositories (GitHub module groups and patches)

- A module group can be synced from a **GitHub repository branch** (e.g. python/python-docs-id, 3.14): "Repositori
  GitHub" in the add form (`source=git`, `git_repo`, `git_branch`; `source.ParseGitHubRepo` accepts owner/name or
  the github.com address, `/tree/<branch>` too). Stored in `module_groups.git_repo/git_branch` (migration 042);
  the sync URL is the branch archive on codeload.github.com (`GitHubArchiveURL`: no API calls, no key), so FetchDue,
  ETag and "Sinkronkan" work as for any archive.
- Import (`importInto`, `gitFiles`): the archive's top folder is dropped; every file keeps its path as
  `project_files.repo_path` ("library/os.po") and gets a flat, collision-free Pelokalan name (`RepoFileName`:
  folders joined with "__", inner dots → "-": "library__os-path.po"), so modules stay unique. Only strings of at
  most `GitTermMaxWords` (12) words become glossary terms (documentation paragraphs are not terms).
- "Ambil"/"Perbarui" copy `repo_path` to the member's file (`catalogs.repo_path`, `catalog.Service.SetRepoPath`).
- **Patch export** (`pkg/gitpatch`, `catalog.Service.GitPatch`): starts from the repository's file as last synced
  (newest project file with that path, `ProjectFileByRepoPath`) and rewrites only entries the member changed in
  Pelokalan since taking/updating the file (compared with the newest "initial" or "from upstream" version in its
  history), so newer repository translations are never reverted; unreviewed AI drafts and emptied strings stay out.
  Rewritten entries keep comments and source lines, gain/lose "fuzzy" (dropping `#|` lines), and msgstr is wrapped
  like gettext (`gitpatch.writeString`/`wrap`: 79 columns, Unicode-like break rules; matches msgcat on 99.6% of
  python-docs entries, and msgcat leaves a patched file unchanged). Within an entry, lines are diffed (LCS) so
  unchanged lines stay context. Output is one `git format-patch` commit (author = translator identity) for `git am`.
- **One module at a time:** each module (file) goes on its own branch and pull request, like files are taken per module.
  There is no all-files patch. Each repository file's ⋯ menu in Berkas Anda has "Unduh patch git" (`GET /catalogs/{cid}/patch`)
  and "Salin perintah git patch", which copies (`Handler.PatchCommand`):
  `git fetch https://github.com/<repo>.git <branch> && git switch -C pelokalan/<module> FETCH_HEAD && curl -fsSL '<link>' | git am`
  (repo/branch from the group the file was last synced from, `source.Service.GitGroupFor`; branch name
  `source.GitBranchName`, "library/os.po" → "pelokalan/library-os"; fetched by address, so remote names do not matter;
  `-C` restarts the branch from the newest upstream on a later round, since the patch holds all the member's changes).
  The list shows each file's repository path. Nothing to send → back to Penerjemahan with `flash.git_no_changes`.
  The editor's "Modul" opens the file on GitHub.
- **Patch links** (`account/patchlink.go`, `GET /git/{token}.patch`, public route, `Handler.GitPatchByLink`): token =
  "c" + catalog id, user id and expiry (base 36, "-" separated) + "." + HMAC (JWT secret), valid `PatchLinkTTL` (24 h);
  access is checked again when fetched and the patch is built then. Failures are plain text: 403 expired/invalid,
  404 nothing to send.

## Full GitHub integration (GitHub App)

- Three ways to send work back to a GitHub repository: (1) git patch (above), (2) pull requests through the app
  (planned), (3) **full integration**: Pelokalan commits approved modules straight to the branch. (3) is built.
- `pkg/githubapp`: the Pelokalan GitHub App (`GITHUB_APP_ID`, `GITHUB_APP_SLUG`, `GITHUB_APP_PRIVATE_KEY[_FILE]`,
  `config.GitHubConfig`; unset = feature hidden). RS256 app JWT, `Installation` per repo, installation tokens cached
  until 5 min before expiry, `GetFile` (contents API, blob API over 1 MB), `PutFile` (contents API with the blob
  sha; `ErrConflict` when the branch moved). `githubtest` is a fake of those endpoints for tests (never call GitHub).
- Module group (migration 048): `git_direct` (on/off), `git_installation`, `git_write`, `git_checked_at`,
  `git_access_error`; `ModuleGroup.CanPush`. The popup's `github_integration` block (git groups): install link,
  "Periksa akses" (`source.Service.CheckGitAccess`), "Aktifkan/Matikan integrasi penuh" (`SetGitDirect`, needs write).
  Git groups edit repository + branch in "Ubah repositori atau cabang" instead of the archive link.
- Reviews (`module_reviews`, `review/service.go`, `web/review.go`): a member asks for a review of their file
  ("Ajukan tinjauan" in the ⋯ menu, and as the main button in Berkas Anda when `Catalog.ReviewReady`: fully translated,
  nothing fuzzy or AI, no open request, and not unchanged since its last commit to the repository; `#review-dialog` with an optional note; `POST /catalogs/{cid}/review`, sent in the background by app.js with
  `X-Requested-With: fetch` → JSON, so the page stays where it is: the popup closes, a toast shows and the file lists
  refresh with their current filters); owners and
  managers are emailed and see "Menunggu tinjauan" on top of Penerjemahan (each request is a clickable row, highlighted on hover, saying "diajukan <time>" and, when the file changed more than a minute after the request, ", diperbarui <time>" (`ModuleReview.UpdatedAfter`, `FileUpdatedAt` = the file's `updated_at`); search by file
  or translator and an order select, `[data-review-queue]` in app.js, filtered in the page; a request whose review
  comments are all resolved shows a green "✓ Sudah diperbaiki" (`ModuleReview.ResolvedComments`), also on file rows); `/projects/{id}/reviews/{rid}` shows the
  changes (`review.Service.Changes`: `catalog.Service.ChangedTargets` = the git patch rule, against the last synced file)
  with "Setujui & commit" / "Kembalikan dengan catatan" (note required, emailed). Nobody approves their own request.
  Owners/managers may also "Kirim ke repositori" any file directly (`PushNow`), unless the project setting "Wajibkan
  tinjauan" (Settings → Umum, `projects.review_required`, migration 050) is on: then the button is hidden and
  `PushNow` refuses (`err.review_required`). One open request per file.
  Owners and coordinators may also cancel someone else's open request: "Lepas tinjauan" in the review editor toolbar
  (before "Setujui & commit") and in Berkas anggota lain (row order: Lepas reservasi, Lepas tinjauan, Tinjau) (same `POST …/reviews/{rid}/withdraw`;
  `review.Service.Withdraw` records who and emails the translator, `mail.review_cancelled`). Reviewers cannot.
- Commit (`review.Service.commit`): reads the branch's current file, `gitpatch.ApplyCredit` (the whole file with only the
  member's changed entries rewritten, and the header crediting the translator: their "Name <email>, year." line among
  the header comments via `po.AddContributor`, and an existing Last-Translator field set to them; `TranslatorCredit`,
  only when some string changes). The review diff and "Lihat" (`review.Service.Changes`, `gitpatch.DiffCredit`) and the
  git patch export show the same header lines. The member's other header edits in Pelokalan are not sent. `PutFile` (one retry on conflict), author = translator's credit identity,
  message "Update translation of <path>" + count + note + `Reviewed-by:` + project link. Only the strings a commit
  rewrote are kept (`pushed_data`, `sentEntries`) and never sent again. Before committing, `UpstreamConflicts` refuses
  (`err.review_upstream_conflict`, naming the strings) when the branch has a translation that differs from both the
  member's starting point for that string (what Pelokalan last sent for it, else the reference version) and the
  member's text: someone changed it upstream meanwhile and the member pulls first. A refused direct send withdraws the
  request it created. Then the
  reservation is released quietly (`ReleasePushed`), the group re-synced (if it has a feed), and the translator emailed.
  A failure keeps the request open with `error` shown on the review page and in the queue.
- File rows show `review_chip` (Menunggu tinjauan / Perlu perbaikan · N (open review comments) / ☑ Sudah masuk repositori · sha
  linking the commit).
- **Review editor** (`/projects/{id}/reviews/{rid}` = `editorPage` with the review, `reviewData`; class `review-mode`):
  read-only, lists only the review's strings (`EntryFilter.Only`, `review.Service.ChangedHashes`), no edit pane (the
  status bar stays, without shortcuts or filter links), toolbar `review_toolbar` (back, file + requester, Tabel/Diff `[data-review-view]` toggling
  `#review-diff`, Validasi, Teks, PO, Modul, then the editor's search/filter/order (`editor_filter`, shared with the full
  editor; it carries `review=<id>` so the list stays on the review's strings; the banner's "Tampilkan" ticks its
  "Komentar tinjauan" option), "Setujui & commit" disabled while review comments are open), banner
  `review_banner` (translator's note, open comments in yellow with a "Tampilkan" checkbox that lists only the strings
  with unresolved review comments (`.rv-open-filter`: `review=<id>&status=review`), last commit error). The sidebar works as usual. The file's owner
  (the translator) is redirected from the review page to their full editor, filtered to open review comments
  (`?status=review`) when there are any, so they fix things right away.
- **View-only layout** ("Lihat" on another member's file, `?view=1`; `viewData`, `view_toolbar`, class `view-mode`
  with `review-mode`): the review layout without decisions or discussion (`.sb-thread` hidden): back, file + owner,
  "Hanya lihat", and for repository files the Tabel/Diff toggle with the member's changes against the last synced file.
  Links without `?view=1` (discussion emails, `?entry=`) open the normal read-only editor, except that someone who may
  review a file waiting for review (`review.Service.OpenFor`) is sent to the review editor with the same query. The
  read-only editor has a yellow bar on top (`othersFileData`, `.editor-others`): whose file it is, and a link to the
  viewer's own copy of the module (`MatchOwnFiles`) or, for members without one, to the module's project file to take it. For owners, coordinators and
  reviewers a file waiting for review shows "Tinjau" (to the review editor) instead of "Lihat". In review and view layouts long
  strings wrap in the table, and the selected row is only lightly marked (pale blue, dark text) since there is no edit
  pane below.
- **Raw view**: the view-only layouts (another member's file with `?view=1`, the project file viewer) have a "Tabel | Raw"
  switch (Diff in between for repository files); Raw loads the file's PO text once (its download address) into
  `#raw-view`, highlighted by `window.poHighlight` (shared with the raw editor; plain text over 3 MB).
- **Project file viewer** (`/projects/{id}/project-files/{fid}/view`, public; "Lihat" in Berkas proyek; `ProjectFileView`,
  pages/project_file.html): the stored file read-only in the view layout (table, `?q=` search, status bar, PO
  download). Glossary module chips (list and term page) link to `/projects/{id}/project-files/module/{name}?q=<term>`
  (`ProjectFileByModule`, `source.Service.ProjectFileForModule` matches `glossary.ModuleName`, newest group first).
- The Berkas proyek and Berkas anggota lain rows have no download button: "Lihat" opens the view-only layout, whose toolbar
  has the PO download. View-only toolbars (project files and other members' files) show "Modul" (`UpstreamInfo.ModulePage`, or the
  file on GitHub), then PO, then end with `view_action` (partials/view_action.html, `viewAction`): "Buka berkas Anda" when the viewer has the module, who holds it when reserved, else "Reservasi
  modul" (reservations on, asks first) or "Ambil", which take the project file; visitors see "Hanya lihat".
- The GitHub integration block always shows the app's install link (to install it on more repositories or change
  its access); it is the main button only until write access is confirmed.
- **Comments on a passage** (migration 056, `comments.anchor_text/anchor_start/anchor_form`): in the review editor,
  people who review (not the requester; `Data.CanAnchor` → `data-anchor-comment` on `#editor-app`) select part of a
  translation in the table and the selection popup (thesaurus) starts with "💬 Komentari bagian ini": the row is
  selected, the side panel opens, and the comment form gets the passage as a quote (`.comment-anchor`, hidden
  inputs, ✕ drops it; app.js `startAnchorComment`). The comment shows the quote (`.comment-quote`). While such a
  review comment is open, the passage is marked pink with a red underline in every table showing the file (review
  editor, the translator's full editor): `Entry.Anchors` from `discussion.OpenReviewAnchors` (attachComments),
  drawn by `anchorMarks` (the occurrence nearest the original offset; not marked once the text no longer has it).
  Resolved review comments stay visible in the review editor only (`.review-mode:not(.view-mode)`): their rows get a
  light green background (`Entry.ReviewResolved`, `discussion.ResolvedReviewEntries`, class `rv-resolved` when nothing
  is open on the row) and their passages a green mark (`discussion.ReviewAnchors` returns resolved anchors too,
  `rv-done`, unstyled elsewhere), so reviewers coming back see what they commented on. app.js turns the row green
  as soon as its last open comment is resolved.
  The review editor's filter has "Komentar tinjauan selesai" (`status=resolved`, `EntryFilter.Resolved` /
  `ResolvedHashes`: entries with resolved review comments and none open, i.e. the green rows; only with `Reviewer` in
  `editor_filter`), and its Validasi button (`data-resolved`) ticks it too, so the green rows stay listed after
  everything was resolved.
- **Review comments** (migration 049, `comments.review_id/resolved_at/resolved_by`): a comment by an owner, coordinator
  or reviewer (not the requester) on an entry of a file with an open request is tied to it (`review.Service.OpenFor` via
  `discussion.Service.SetReviews`). It is open until resolved (green "Sudah diselesaikan" button with a check, `POST /projects/{id}/comments/{cid}/resolve`,
  file owner, the author or a manager): red dot on the entry row (`Entry.ReviewOpen`, `mark-review`, kept in sync by
  app.js from the thread's `data-review-open`), a "Komentar tinjauan" status filter (`EntryFilter.Review`) and status bar
  count in the translator's editor. Approving or sending (`push`/`PushNow`) waits until none is open
  (`err.review_open_comments`). The reject route stays but the UI works through comments.

## Module groups UI

- Settings has a "Kelompok modul" section (`#module-groups`, owners and managers) with the group labels and buttons
  that open the `#module-groups-dialog` popup (`partials/module_groups.html`, handlers in `module_group.go`) with
  tabs: list (groups, imports, sync link actions) and add (one form: file or URL). Routes live under
  `/projects/{id}/module-groups/…` (`sources`, `import`, `sync`, `{gid}/delete|fetch|unsync`,
  `imports/{iid}/delete`); they redirect to `/settings?groups=list|add#module-groups`, which reopens the popup
  (`moduleGroupsData`). The glossary page no longer manages groups; its empty state links to that popup.
  The glossary's group filter uses `?group=` (old `?version=` links still work); the AI dialog sends `glossary_group`.

## AI pre-translation

- Settings per project in `project_ai_settings` (owner/manager): base URL, model (a dropdown of
  `pretranslate.AIModels`, OpenAI chat models, plus "Lainnya…" for any other name; `[data-model-pick]` in app.js). Prices of
  those models are built in (`pretranslate.AIPrices`, USD per 1M tokens from OpenAI's pricing page, updated by hand; shown
  under the dropdown) and replace stored prices on load and save (`applyModelPrice`); the price fields only appear for
  "Lainnya…". API key
  encrypted with `ai.Box` (AES-GCM, key from `AI_SECRET_KEY`), prices per 1M tokens, monthly budget,
  and `extra_context` (migration 023): project-wide guidance sent as `<project_context>` before the
  file's `<file_context>` (the file note is more specific and wins). Words in either can make
  glossary hints optional.
- `pretranslate.Service.plan` (pretranslate/service.go): untranslated (optionally fuzzy) entries of the user's own
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
  by side (`#ai-alts`), each marking (`<mark>`, `altDiffs`) where the alternatives differ: every alternative is aligned word by word with #1
  (`wordOps`), and a change is keyed by where it starts in #1, so the same place has the same colour in every card
  (golden-angle hues via `--h`; spaces between changed words stay in one change);
  clicking a box (or Enter/Space on it) puts it in the translation box (undoable). With one result it goes straight in.
- Per-entry AI details:
  `POST /catalogs/{cid}/entries/{eid}/ai` → `pretranslate.Service.TranslateEntry` translates one entry with the
  editor's current text as draft and returns it unsaved; cost is recorded as an `ai_jobs` row with
  `single = true` (migration 025), which counts toward the budget but is not shown as the file's job.
- Glossary hints are re-cased to the sentence (`ai.MatchCase`): a term used in lowercase in the
  source gets a lowercase hint ("Kontras" → "kontras"); the prompt says glossary fixes the word,
  not its capitalization.
- When AI replaces existing text, the old text is kept in `entries.replaced_msgstr` (migration 026;
  the last person-written text wins over earlier AI attempts) until a person saves. The editor no longer shows it
  (the "Bandingkan" comparison was removed at the community's request; "Urungkan" brings the old text back).
- Requests use temperature 0.2 (`ai.chatTemperature`); if a model rejects it, the call is retried
  without a temperature.
- `ai.Verify` flags results that miss a required glossary translation or fail `po.Validate`;
  flagged results are always saved as fuzzy.
- Jobs (`ai_jobs`) run in a goroutine, batch by batch (25 items / 6000 chars), record real
  token usage and cost, honor cancel (`watchCancel` polls every 1.5 s and aborts the in-flight
  model request; finished batches are kept) and the monthly budget, and skip entries edited meanwhile
  (entry version check). Running jobs are marked failed on startup (`RecoverJobs`).
- Cost estimate: `ai.EstimateTokens` (~3.5 chars/token) via `pretranslate.Service.Estimate`.

## Glossary extraction from long strings (AI)

- Short strings become glossary terms as they are; documentation keeps its vocabulary in long strings. Owners and
  coordinators press "✨ Ekstrak istilah dengan AI" on the glossary page (`#extract-dialog`, app.js): pick a module group,
  see the estimate (`GET /projects/{id}/glossary-extract?group=`: long strings, already extracted, to send, tokens, cost,
  budget), start (`POST`). While a run is going the button is hidden (`Data.ExtractRunning`, app.js) and its progress
  shows instead (`GET …/glossary-extract/status`, polled) with "Hentikan" beside it (`POST …/cancel`; a run the
  process no longer has, only marked running in the database, is closed as cancelled).
- `pkg/ai/extract.go`: `ExtractSystemPrompt` (precise rules: copy both sides verbatim, keep the inflected form, skip
  function words, names, code, markup, untranslated terms; JSON `{"items":[{"id","terms":[{"s","t"}]}]}`),
  `ExtractCandidate` (>= `ExtractMinWords` (4) words outside markup, <= 3000 chars), `CheckPairs` (each side must occur as
  whole words in its text with code, ``literals``, tags and placeholders removed; reST links keep their visible text;
  stop words, long sides, code-like identical pairs dropped; at most 15 per string).
- `pretranslate/extract.go`: strings come from the import's stored project files (`source.Repository.ImportFiles`); results
  are stored per string in `term_extractions` (migration 057, key language + SHA-256 of source and translation, shared by
  every project, empty lists too), so each string is sent once. Runs (`term_extraction_jobs`, 3 batches of 20 at a time,
  cost counted in `Spend`, so the monthly budget covers them; one run per project) end by rebuilding the group's AI rows:
  `glossary_terms.origin = 'ai'` per import (`ReplaceAITerms`, `module_group_imports.ai_term_count` feeds `Stamp`).
- After every import `source.Service` calls `TermExtractor.AfterImport` (app wires `pretranslate.Service`) in the background:
  for groups where extraction was started (`module_groups.ai_terms`) the new import gets its AI rows from stored results,
  then only the files whose content changed since the group's previous copy (`GroupFileHashes`: SHA-256 of the stored
  gzip, compared in `importInto`; a first copy counts as all changed) have their unextracted strings sent (within the
  budget), then rows are rebuilt. A backlog in unchanged files (a stopped first run) is left to the button.
- A translation is "AI" when every row behind it is (`bool_and(origin = 'ai')`): a small purple "AI" mark
  (`ai_term_mark`) in the glossary list and term page. AI-only terms are never applied to a whole string
  (`ai.Term.AI`, `Glossary.Exact`) and are only optional hints for pre-translation.

- **Consistency check in the review editor** ("Konsistensi" after Validasi, `Data.CanCheck`: people who review, not the
  requester): `#cons-panel` below the table, `GET|POST /projects/{id}/reviews/{rid}/consistency` (HTML partial
  `consistency_panel`, polls itself every 2 s while running). `pretranslate.Service.CheckConsistency` takes the review's
  strings (translated, not fuzzy), reuses stored extraction results, sends the rest like an extraction run (cost in
  `term_extraction_jobs`, budget checked; one-word strings are their own pair), then lists terms whose translation
  differs from the glossary's agreed one (people's, share >= 50%, not AI-only) or varies among the strings. Chips are
  green (= glossary) or orange; clicking one selects its strings in turn. Kept in memory per file (not stored).

## Editor glossary section

At the top of the right sidebar (`entry_glossary` partial in editor.html, rendered by sidebar.html; while the
sidebar is closed, and on phones, the copy in the editor pane above the source text shows instead) the editor lists glossary terms found in the entry (`source.Service.ForEntry`,
all versions, preferred translation + share). Chips insert the translation at the cursor; ↗ opens
the term page. Orange text when nothing matches or the language has no glossary. Chips the project AI
context or the file's Konteks talks about (`ai.Mentioned`, same rule as pre-translation) are orange
(`EntryTerm.Overridden`). Term matchers are
cached per project+language and rebuilt only when `source.Repository.Stamp` (imports) changes.

## Entry origin

`entries.origin` is '', 'ai', 'glossary' or 'memory' for machine translations not yet saved
by a person (`SaveEntry` clears it).

**"AI perlu ditinjau" / "AI needs review"** (`Entry.Status()` = "ai", ahead of fuzzy): an AI draft no person has
saved. AI results are stored without the fuzzy flag (migration 040 cleared it on existing drafts) and
`catalog.Service.Build` exports them as `#, fuzzy`, so unreviewed AI text never leaves as final. Counted as "needs
review" in `CatalogStats.Fuzzy` (progress bars, SQL `Stats`, history), but the editor status bar shows plain fuzzy
(`EntryListing.FuzzyOnly`) and AI drafts (`AI`) apart; purple rows (`.st-ai`), filter/status value "ai", on by default
in Validasi (`account.EditorStatuses`). In the editor the review box is ticked and labelled "AI perlu ditinjau", so
"Lepas pratinjau, simpan & lanjut" approves it (clears fuzzy and origin). Translation memory skips AI drafts
(`origin <> 'ai'`). Diffs compare "needs review" (fuzzy or AI) so older snapshots with the flag show no change. The
activity log records an AI draft's status as "fuzzy" (`logStatus`) so its counting rules stay as before. The
pre-translation checkbox `mark_fuzzy` now only applies to memory and glossary results. The editor marks 'ai' with a purple bot (the colour of AI draft text), 'glossary' with a green book and
'memory' with blue cards (`icon_origin` in entry_list.html), with status-bar counts and filters for each. Snapshots keep it as an `x-origin-*` flag, stripped from downloads.

## Version history

- The UI calls a manual commit "Simpan Versi" / "Save Version" (button, dialog, counts); code and tables keep "commit".

- `catalog_commits`: linear history per catalog, gzip PO snapshot + stats + diff counts.
  Kinds: manual, auto (safety), restore, initial. Auto messages are i18n keys ("key|arg").
- `catalog.Service.SafetyCommit` runs before replace/raw save/template merge/memory
  pre-translate/AI job, and after AI jobs; `EnsureBaseline` gives files without history a
  first version. `Restore` commits the current state first, then records a restore commit, so
  every step can be undone. `Discard` = restore latest commit.
- The Commit button badge (`commit_badge`/`commit_status` partials) is refreshed out-of-band after
  every entry save, so needs-work and origin changes show up immediately.
- The owner can change a version's message: select it on the history page, "Ubah pesan" (before Unduh) turns the
  title into a field (`POST /catalogs/{cid}/history/{hid}/message`, `catalog.Service.EditCommitMessage`, same
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
needs project AI settings). Implemented by `pretranslate.Service.Start` / jobs in `ai_jobs`.

## Top bar

`partials/nav.html` centers a title: "Project · file" over the language when the page sets
`Data.Catalog` (editor, raw, history), else the project name when it sets `Data.Project`.
The brand links to /projects; Undangan lives in the user menu. In the editor toolbar the
search box fills the space after the buttons, followed by the filter (funnel) and order selects.

## Phones

File list toolbars on phones: search on its own row, filters two per row, the sort select last, filter popups as wide
as the bar. All phone rules live in one `@media (max-width: 700px)` block at the end of style.css (it must
stay last to override). Rules of thumb: nothing may be wider than the screen (tabs scroll
sideways, tables become stacked blocks: `.files-table`, `.gl-table`), grid tracks use
`minmax(0, 1fr)`, and fields use 16px text so iOS does not zoom in. Tablets (701–960px, e.g. iPad mini
portrait 744px): the file tables hide their three count columns; the project tabs scroll sideways at
every width so they never widen the page (a page wider than the screen makes iPad zoom out).

## Static assets

Served with `Cache-Control: no-cache`; templates add `?v={{.Asset}}` (hash of static/) so
deploys never run stale JS/CSS.

## i18n

- All UI strings live in `internal/platform/i18n/messages.go`; Bahasa Indonesia is primary.
- Templates: `{{t .Lang "key"}}` (use `$.Lang` inside range/with).
- Tests in `internal/platform/i18n` fail when a key is missing in one language or used but undefined.
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
  Tablets and phones (≤1180px: iPad mini portrait and landscape, phones): the sidebar floats over the table (absolute,
  shadow), starts closed and is opened with the same handle, which is bigger and bluish there (no dragging, state not
  stored: `floating` media query in app.js). On phones (≤700px) the open sidebar covers the whole width and the
  handle becomes a round × close button at its top right (`.side-close-icon`); the toolbar (also the review/view
  toolbar), banners and status bar are hidden while it is open. While the sidebar is closed, a red count beside the handle (`#side-review-badge`)
  shows the file's open review comments; app.js keeps it in step from each thread's `data-review-open`.
  The review editor always shows the sidebar on wide screens (no handle) and hides suggestions and the translator's
  note (`.sb-suggestions`, `.sb-tcomment`).
- The entry table always shows its scroll bar (`overflow-y: scroll` with a styled `::-webkit-scrollbar`, so overlay
  scroll bars on macOS/iPadOS/touch Chrome do not hide it).
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
  Terjemahan label row, "Terjemahkan dengan AI" + count sit on the right. The Perlu ditinjau checkbox
  sits in `.ed-actions` right before the save buttons.
  English calls Perlu ditinjau "Fuzzy".
- Validation flags `control_chars` and `markup` (tags such as <gui>, <key> differing from the source);
  `ai.UserPrompt` sends markup unescaped, and AI results with control characters are never saved.
- Toolbar order: Proyek | Tarik upstream, Riwayat, Simpan Versi | Konteks, Praterjemah, Validasi | Teks, Metadata |
  PO, Modul | Bantuan, Pengaturan. "Modul" (`Data.ModulePage`) opens the file's module page on l10n.gnome.org (its
  stored upstream state's page, else its own/guessed link; hidden when there is none). MO is downloaded from the
  file menu on Penerjemahan only.
- Editor settings ("Pengaturan" after Bantuan, `#editor-settings-dialog`, signed-in users) are per account and
  apply to every file: `users.editor_checks_off` (migration 032, `User.ChecksOff`/`CheckOn`, everything on by
  default so new checks start on). Users untick validation rules (`po.Checks`; switched-off issues are dropped in
  `validateEntry` via `Catalog.ChecksOff`, so they are not marked or counted) and the statuses Validasi ticks
  (`account.EditorStatuses`; `#btn-validate[data-statuses]`). Saved with `POST /editor-settings` (back to `next`).
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
  Bermasalah, Belum diterjemahkan, Perlu ditinjau and Komentar tinjauan; when nothing matches, the filter goes back to
  what it was (all strings stay) and a toast says all is clear (`editor.validate_all_clear`).
- Entry load/save deliberately use `fetch` (not htmx): a new load aborts the previous one,
  requests time out after 20 s, navigation during a save waits for it, and a failed save asks
  before moving on. htmx's per-element request queue could otherwise freeze the editor after
  one stalled request. The entry list filter still uses htmx.

## Stored messages

Version messages, sync errors and review errors are stored as "key|arg|arg"; `storedText` (template funcs `errText`,
`commitMsg`) renders them and turns the arguments a message formats with `%d` back into numbers.

## Toasts and background forms

- Flash messages (`?flash=<key>`, `flash.*` strings) are floating toasts on every page (`#toast` in base.html: centred at
  the top below the top bar, light green, light yellow for errors (`toast-error`); removed after 4.5 s; app.js drops `flash` from the address so a reload does not repeat it). From JS: `window.toast(msg, isError)`
  (defined for every page: the `#toast` element would otherwise be `window.toast`).
- Forms with `data-ajax` (⋯ menu: reserve, release, withdraw a review; the coordinators' "Lepas") and the review popup
  are sent in the background with `X-Requested-With: fetch`; handlers answer JSON through `Handler.finish` /
  `failAction` (redirect + flash otherwise). The page shows the message as a toast and `window.refreshFileLists()`
  reloads the three file lists with their filters. `data-confirm` still asks first (it resubmits on OK).

## Slow requests

An htmx request still waiting after 1 s covers the page with a half transparent layer and a centred spinner
(`.busy-overlay`, app.js; a manual popover so it also covers open dialogs; label `common.loading` from
`body[data-loading]`). Polling (`hx-trigger="every …"`) and anything inside `[data-no-busy]` never show it; it hides
when the last request ends and after 30 s at most. The editor's entry load/save use `fetch`, not htmx, so they do not
show it.

## Integration tests

- `internal/app` tests run the real router (`app.New` with `Options{Offline: true}`: no GNOME site client; a capture
  mailer) on a database made by `testdb.Create` (`pelokalan_test_<random>` on `TEST_DATABASE_URL`, migrated, dropped in
  `TestMain`; only names with that prefix are ever dropped). Without `TEST_DATABASE_URL` they skip.
- Harness (`harness_test.go`): `signUp` registers through the forms and reads the code from the captured email;
  `user.get/post/hx/upload` (Origin set, redirects not followed), `response.status/redirectsTo/contains/lacks`,
  `scalar[T]` reads the DB, `poFile`/`targz` build files, `mails.waitFor` waits for background emails. Tests share one
  database, so give everything unique names (`unique`).
- Add a test there when adding a route or a rule that touches the database. Never call external sites from tests.

## Conventions

- Do not mention other translation tools by name in code, comments, docs or commits.
- Keep both languages in sync when adding strings.
- Never use the browser's built-in `confirm()`/`alert()`/`prompt()` (blocked in embedded browsers). Ask with
  `data-confirm` on a form or `hx-confirm` (both open `#confirm-dialog` from base.html; optional
  `data-confirm-ok` button label; actions ending in /delete, /revoke, /remove get a red "Hapus"), or from JS
  with `window.askConfirm(msg, {ok, danger})` / `window.showMessage(msg)` (Promises). Only the
  `beforeunload` leave-page warning stays native: pages cannot replace it.
