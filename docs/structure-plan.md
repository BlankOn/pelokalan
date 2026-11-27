# Plan: package structure by domain

Status: **done** (November 2026). Every step was its own commit and passed `make check`. Where the result differs from
the proposal below:
- the mail package is `internal/platform/mailer` (a package named `mail` clashes with the standard `net/mail`);
- two more platform packages: `store` (`ErrNotFound`/`ErrConflict`, SQL helpers such as `EscapeLike`, `Arg`,
  `UntranslatedExpr`) and `gz` (gzip helpers); module naming (`ModuleKey`, `ModuleOf`, `CleanFilename`) and
  `ReservationDays` live in `models`;
- final order from the bottom: activity, langtools, modulecontext → account → project → reservation, source → catalog →
  discussion → wiki, pretranslate → review (activity sits below account: it takes a one-method `UserGetter`);
- the `repository.Repositories` aggregate became `app.Repositories`.

The original proposal follows.

## Where we are

| Layer | Package | Size | Notes |
|---|---|---|---|
| Logic | `internal/service` | 32 files, ~10,200 lines | every domain in one package, wired with setters |
| Storage | `internal/repository` (interfaces) + `repository/postgres` | ~4,000 lines | one interface file for all domains |
| Delivery | `internal/handler/web` + `handler/middleware` + `internal/templates` | ~7,000 lines | one package, pages mix domains |
| Libraries | `po`, `gitpatch`, `gnomel10n`, `githubapp`, `tpsite`, `netsafe`, `glossary`, `ai`, `badgecard`, `spelling`, `thesaurus` | | no imports of app code |
| Platform | `config`, `i18n`, `blobstore`, `testdb`, `app` | | |

Database access outside `repository/postgres`: only `blobstore` (the Postgres file store), `app` (migrations) and `testdb`.
The real problem is size: one service package and one handler package hold everything.

## Target

```
pkg/                         reusable libraries, never import internal/
  po/ gitpatch/ gnomel10n/ githubapp/ (githubtest/) tpsite/ netsafe/
  glossary/                  archive reading, term building, matching (the library half of today's glossary)
  ai/                        LLM client, prompts, verification
  badgecard/ spelling/ thesaurus/
internal/
  platform/                  config/ i18n/ blobstore/ mail/ apperr/ testdb/
  models/                    shared domain types (unchanged; splitting them would create import cycles)
  <domain>/                  one package per domain:
    service.go …             logic (today's service/<x>.go files)
    store.go                 the storage interface this domain needs (today's part of repository.go)
    postgres.go …            its Postgres implementation (today's repository/postgres/<x>.go)
    *_test.go
  web/                       delivery: router setup helpers, handlers grouped by domain in files, middleware/, templates/
  app/                       wiring (app.New), migrations, integration tests (unchanged role)
cmd/server/
migrations/ static/
```

Why one package per domain instead of `<domain>/usecase` + `<domain>/repo`: every package would be named `usecase` or
`repo`, so every import needs an alias (`reviewuc`, `catalogrepo` …). Separate files in one package give the same
separation, and the storage interface stays next to the code that uses it (Go's "consumer defines the interface").

Why one `web` package instead of delivery per domain: the editor page alone shows the file, its review, the reservation,
glossary terms and discussions; templates are parsed as one set with shared layouts and partials. Handler files stay
grouped by domain inside `web`.

### Domains and what moves into them

| Domain | Service files | Postgres files | Handler files (stay in web) |
|---|---|---|---|
| `account` (users, sign-in codes, profile, avatar) | auth, profile_links, avatar | user, otp | auth, avatar |
| `project` (projects, members, invitations, invite links, notification settings) | project, invite_link | project, notification | project (members/settings parts), api |
| `catalog` (PO files, entries, versions, git patches, accepted upstream, file order) | catalog, history, gitpatch, landing, patchlink, filesort | catalog, commit | catalog, history, gitpatch, project (file lists) |
| `source` (module groups, imports, project files, upstream states, TP, GitHub, glossary search) | glossary, glossary_match, upstream, tp, gitrepo, github | glossary, tp | glossary, module_group, project_file_view, groupchart |
| `review` | review | review | review |
| `reservation` | reservation | reservation | reservation |
| `discussion` (threads, comments, attachments, mentions) | discussion | discussion | discussion |
| `wiki` | wiki | wiki | wiki, wikimd |
| `modulecontext` (Konteks) | context | context | context |
| `pretranslate` (AI jobs and single-entry AI) | ai | ai | ai, ai_entry |
| `activity` (activity log, profile stats, badges, badge mail/share) | activity, profile, badgedetail, badgemail, badgeshare | activity, profile | userpage, badgeshare, insight, wordschart |
| `langtools` (spelling and thesaurus storage) | spelling, thesaurus | spelling, thesaurus | spelling, thesaurus |

Shared helpers leave `service` first: `errors.go` (UserError, ErrNotFound …) → `platform/apperr`; `mailer.go` (Mailer,
Message, emailButton) → `platform/mail`.

Names that would clash with `pkg/` are avoided on purpose: `source` (not `glossary`), `pretranslate` (not `ai`),
`langtools` (not `spelling`), `modulecontext` (not `context`, which shadows the standard library).

### Dependency rules

- `pkg/*` imports only the standard library and other `pkg/*`.
- A domain may import `models`, `platform/*`, `pkg/*` and other domains that sit below it (order below). Anything that
  would point upwards goes through a small interface defined in the lower domain and wired in `app`.
- `web` imports domains; domains never import `web`.
- Only `app` knows every concrete type.

Order from the bottom up: platform → account → project → langtools, modulecontext, wiki, activity → reservation →
source → catalog → discussion → review → pretranslate.

Links that point upwards today and their interface:
- `source` → `catalog` (detect accepted work after a sync): already an interface (`LandingDetector`), stays.
- `catalog` → `source` (project files for patches, upstream changes): a small `ProjectFiles` interface in `catalog`.
- `discussion` → `review` (tie review comments): already a function (`SetReviews`), stays.
- `project` → `account` (whitelist check): a small interface in `project`.

## Steps

### 0. Preparation
- `make check`: gofmt, go vet, unit and integration tests in one command.
- Tag the starting point (`git tag pre-structure`).
- Pause feature work while a step is open (the moves touch many files and would conflict).

### 1. Libraries to `pkg/` (mechanical, one commit)
`git mv` po, gitpatch, gnomel10n, githubapp (with githubtest), tpsite, netsafe, glossary, ai, badgecard, spelling,
thesaurus to `pkg/`, testdata included; rewrite import paths; gofmt; build, vet, test. No behaviour change. Update
CLAUDE.md paths.

### 2. Platform (one commit)
Move config, i18n, blobstore, testdb into `internal/platform/`; split `service/errors.go` into `platform/apperr` and
`service/mailer.go` into `platform/mail`, keeping aliases in `service` for the moment so nothing else changes yet.

### 3. Delivery to `internal/web` (one commit)
`handler/web` → `internal/web`, `handler/middleware` → `internal/web/middleware`, `internal/templates` →
`internal/web/templates` (embed path updated). Handler files keep their names.

### 4. Domains, one per commit, bottom up
For each domain:
1. create `internal/<domain>/`; move the service file(s) as `service.go` …, its part of `repository.go` as `store.go`,
   its Postgres file(s) as `postgres.go` …, and their unit tests;
2. rename `XxxService` to `Service`, `NewXxxService` to `New` (callers read `review.Service`, `review.New`);
3. update `app` wiring and the `web` handlers' imports;
4. build, vet, full test suite; deploy when convenient.

Order: account → project → modulecontext → wiki → langtools → activity → reservation → source → catalog → discussion →
review → pretranslate. The first ones are small and settle the pattern; catalog and source are the largest.

### 5. Clean up
Remove the empty `internal/service` and `internal/repository`; drop the temporary aliases; rewrite the "Project
Structure" and "Architecture" sections of CLAUDE.md; remove the tag.

## Size and risk

- Steps 1–3 are renames: low risk, a few hours, verified by the build and the integration tests.
- Step 4 is twelve commits that touch most of the code but change no behaviour; the integration tests in `internal/app`
  run the real router against a real database and are the safety net. Larger domains (source, catalog) are the slowest.
- Risk: conflicts with feature work in flight. Mitigation: one step at a time, merged and deployed before the next.
- Not changing: database schema, URLs, templates' content, behaviour.

## Open choices

1. Library location: `pkg/` (signals reusable, importable by other projects) or keep them under `internal/lib/`
   (reusable inside this module only). The plan uses `pkg/`.
2. Domain names in the table (e.g. `source` for module groups and project files).
3. Whether `models` is split per domain later (not planned: it would bring back import cycles).
