# Architecture

A layout for a server-rendered web application (or an API) in Go, organised **by domain**, with storage behind
interfaces that each domain defines for itself. It is written so that it can be applied to another codebase: the
first part describes the target, the second part how to move an existing codebase there without changing behaviour.

Example names below (`accounts`, `orders`, `billing`, `catalog` …) are placeholders; use your own domain words.

---

## 1. Goals

- **Find code by what it is about, not by its technical role.** Everything about orders (rules, storage, tests)
  lives in one package; you do not open five folders to follow one feature.
- **Small, explicit dependencies.** A package states what it needs as narrow interfaces; the composition root wires
  the concrete types. No globals, no service locator.
- **No import cycles by construction.** Domains are ordered; calls only go down that order. The rare call that
  must go up goes through an interface owned by the lower package.
- **Storage is replaceable and invisible to delivery.** Handlers never write SQL; domain logic never knows about
  HTTP.
- **Reusable code is really reusable.** Generic libraries do not import application code and could be published as
  they are.
- **One command proves it works.** Formatting, vetting, unit tests and integration tests against a real database run
  together before every commit.

---

## 2. Layout

```
cmd/server/main.go        bootstrap only: config, DB, migrations, background jobs, HTTP server
pkg/                      reusable libraries; import only the standard library and other pkg/*
  <format>/               parsers/writers of file formats the product handles
  <client>/               clients of external services (+ a <client>test/ fake for tests)
  netsafe/ …              small generic helpers (SSRF-safe HTTP transport, image cards, text tools)
internal/
  app/                    composition root: app.New wires everything; app.Migrate; HTTP integration tests
  platform/               cross-cutting infrastructure, no business rules
    config/               typed configuration from the environment
    i18n/                 UI strings per language (+ a test that every key exists in every language)
    apperr/               errors meant for people (an i18n key + arguments)
    store/                sentinel storage errors (ErrNotFound, ErrConflict) and small SQL helpers
    mailer/ blobstore/    outgoing mail, file/object storage (interfaces + implementations)
    testdb/               creates and drops a throwaway database for integration tests
  models/                 shared domain types (plain structs, small methods, no I/O)
  <domain>/               one package per domain (see §3)
  web/                    delivery: handlers grouped by domain in files, middleware/, templates/
migrations/               numbered SQL migrations (up/down), run on start
static/                   CSS, JS, images
docs/                     design notes and plans
```

### Why one package per domain (and not `orders/usecase` + `orders/repo`)

Splitting each domain into `usecase`/`repo`/`delivery` sub-packages gives every package the same name, so every
import needs an alias (`ordersuc`, `ordersrepo` …) and the storage interface ends up far from its only consumer.
Separate **files** inside one package give the same separation with none of the friction, and the interface sits next
to the code that uses it (Go's "the consumer defines the interface").

### Why one `web` package (and not delivery per domain)

Pages mix domains: one screen may show an order, its invoice, its comments and its reservations. Templates are parsed
as one set with shared layouts and partials. So delivery stays one package; inside it, handler files are grouped by
domain (`orders.go`, `billing.go`, …).

### Why `models` stays one package

Domain types reference each other (an order has a customer, an invoice has an order). Splitting them per domain
brings import cycles back. Keep `models` as plain data with small, pure methods (formatting, derived flags, URL
builders), no database or network access.

---

## 3. Anatomy of a domain package

```
internal/orders/
  service.go        type Service struct{…}; func New(…) *Service; the domain's operations and rules
  <topic>.go        more logic files when service.go grows (pricing.go, export.go …)
  store.go          the storage interfaces THIS domain needs, and their small record types
  postgres.go       their Postgres implementation: NewRepository(db *sql.DB) Repository
  postgres_<x>.go   more storage files for large domains
  *_test.go         unit tests of the logic (fakes of the store interfaces, or none)
```

Conventions:

- The type is `Service` and the constructor `New`; callers read `orders.Service`, `orders.New(…)`. Storage is
  `orders.Repository` / `orders.NewRepository(db)` (more than one is fine: `NewCommitRepository`).
- **Constructor injection.** `New` takes everything the service always needs (its repository, other domains'
  services or repositories, mailer, config sections). Optional collaborators or ones that would create a wiring cycle
  are set afterwards with explicit setters (`SetActivity(a)`, `SetReviews(fn)`), called only in `app.New`. A nil
  optional collaborator means "feature off", and the code checks for it.
- **The interface is as small as the consumer needs**, not a mirror of the implementation. A domain may depend on
  another domain's *repository interface* when it only needs data, and on its *service* when it needs the rules.
- **Methods take `context.Context` first** and the acting user (`*models.User`) when permissions matter; permission
  checks live in the service, not in handlers.
- Name domains so they do not clash with `pkg/` or the standard library (`assistant`, not `ai`;
  `ordercontext`, not `context`; `mailer`, not `mail`).

---

## 4. Dependency rules

1. `pkg/*` imports only the standard library and other `pkg/*`. Never `internal/`.
2. `platform/*` imports the standard library, `pkg/*` and other `platform/*`. No business rules.
3. A domain imports `models`, `platform/*`, `pkg/*` and **domains below it** in a fixed order, written down in the
   project's docs. Example: `activity → accounts → projects → catalog, billing → orders → reviews`
   (left = bottom).
4. A call that would point **up** that order goes through a small interface (or a function value) defined in the
   lower domain and injected by `app`:
   ```go
   // in internal/catalog (lower): what it needs from orders (higher), nothing more
   type OrderCounter interface {
       OpenOrders(ctx context.Context, productID int64) (int, error)
   }
   func (s *Service) SetOrderCounter(c OrderCounter) { s.orders = c }
   ```
   ```go
   // in internal/app
   a.Catalog.SetOrderCounter(a.Orders)
   ```
5. `web` imports domains; domains never import `web`.
6. `web` and `app` never touch SQL. Only `postgres*.go` files (plus `platform/blobstore`, `platform/testdb` and
   `app.Migrate`) talk to the database.
7. Only `app` knows every concrete type.

Keep the order short and linear. When you find yourself adding the third upward interface between the same two
domains, the boundary is probably wrong.

---

## 5. Errors

Two kinds, never mixed:

- **Errors for people**: `apperr.New("err.order_closed", orderID)` returns an `*apperr.UserError{Key, Args}`.
  `Key` is an i18n key; handlers render it in the reader's language. Common ones are predeclared
  (`apperr.ErrForbidden`, `apperr.ErrNotFound` …). Services return these when the person can do something about it.
- **Storage sentinels**: repositories return `store.ErrNotFound` / `store.ErrConflict` (optimistic locking,
  unique violations) and wrapped driver errors otherwise. Services translate them into user errors where it matters;
  handlers map what is left (`errors.Is(err, store.ErrNotFound)` → 404, anything else → 500 + log).

Messages that are stored and shown later (job errors, version messages) are stored as `"key|arg|arg"` and rendered
through the same i18n lookup, so they follow the reader's language.

---

## 6. Composition root (`internal/app`)

```go
type Repositories struct { Accounts accounts.Repository; Orders orders.Repository; … }

type App struct {
    Repos   *Repositories
    Orders  *orders.Service
    …
    Handler http.Handler
}

func New(cfg *config.Config, db *sql.DB, opts Options) (*App, error) {
    a := &App{}
    a.Repos = &Repositories{Accounts: accounts.NewRepository(db), Orders: orders.NewRepository(db), …}
    a.Accounts = accounts.New(a.Repos.Accounts, mailer, cfg.Auth)
    a.Orders = orders.New(a.Repos.Orders, a.Accounts, mailer, cfg.Site)
    a.Catalog.SetOrderCounter(a.Orders) // upward link, see §4
    h, err := web.NewHandler(…services…)
    a.Handler = routes(h)                // every route in one place
    return a, err
}
```

- `Options` switches off what tests must not reach (external sites, real mail): `Options{Offline: true}` plus a
  capturing mailer.
- `main.go` only loads config, opens the DB, runs `app.Migrate`, calls `app.New`, starts background jobs (tickers
  calling service methods) and serves HTTP with graceful shutdown.
- All routes live in `app` (or one `routes.go`), grouped into public / signed-in / role-restricted groups with
  middleware, so access rules are visible at a glance.

---

## 7. Delivery (`internal/web`)

- `Handler` holds the services it uses; handler methods are thin: parse input → call one service method → render or
  redirect. Loading and permission helpers (`loadOrder`, `loadOwnOrder`) are shared and return early with the
  right status.
- Templates are embedded, parsed once as a set; layouts + partials; every string through `{{t .Lang "key"}}`.
- Middleware (auth, language, CSRF/origin checks, logging) lives in `web/middleware` and only puts values into the
  request context.
- JSON endpoints and HTML pages use the same services; a request that asks for JSON (`X-Requested-With: fetch`,
  `Accept`) gets JSON from the same handler.
- Static assets are served with a content hash in the URL (`?v=<hash>`) so deploys never serve stale JS/CSS.

---

## 8. Persistence

- PostgreSQL through `database/sql` with the pgx stdlib driver; plain SQL in `postgres*.go`, no ORM.
- Migrations are numbered files (`NNN_name.up.sql` / `.down.sql`), applied automatically on start. Backfills go in
  the same migration as the schema change.
- Concurrency: rows people edit carry a `version`; updates check it and return `store.ErrConflict` when stale. The UI
  keeps the person's text and offers to reload instead of overwriting.
- Files/blobs go through a `blobstore.Store` interface (Put/Get/Delete/GC), so moving them to object storage is a
  swap in `app`.
- Long or bulk inserts are batched; IDs needed by child rows are taken from the sequence first rather than relying on
  `RETURNING` order.

---

## 9. Background work

- Periodic jobs are service methods (`FetchDue`, `Expire`, `GC`) called by tickers in `main.go`; each guards itself
  against overlapping runs (a mutex or a "running" flag in the service).
- Work started by a request but too slow for it runs in a goroutine owned by the service, with its own
  `context.Background()`-derived context, progress stored in the database (or in memory when losing it is
  acceptable) and polled by the page. Jobs left "running" after a restart are marked failed on start.
- One-time start-up jobs are recorded in a table (`one_time_jobs`) so they run exactly once across deploys.

---

## 10. Testing

- **Unit tests** next to the code, in every package where logic can be tested without I/O (`pkg/*`, pure functions
  in domains, template helpers in `web`).
- **Integration tests** in `internal/app`: `app.New` with the real router against a throwaway database
  (`testdb.Create` on `TEST_DATABASE_URL`, migrated, dropped in `TestMain`; only databases with the test prefix are
  ever dropped). Without the env variable they skip, so `go test ./...` still works anywhere.
- A small **harness**: sign a user up through the real forms, `user.get/post/upload`, assertions such as
  `status`, `redirectsTo`, `contains`, `lacks`, `scalar[T](sql)` to read the database, a capturing mailer with
  `waitFor`. Tests share one database, so every name they create is unique.
- **Fakes for external services** (`pkg/<client>test`, `httptest` servers for AI/APIs). Tests never call real sites.
- An i18n test fails when a key is missing in a language or used but undefined; a template test parses every
  template.
- `make check` = gofmt check + `go vet` + all tests including integration. It must pass before every commit.

---

## 11. Conventions that keep it clean

- Every package has a doc comment saying what it owns; every exported identifier has a one-line comment.
- Domain docs (a `CLAUDE.md`/`CONTRIBUTING.md`) list: the dependency order, each domain's responsibility, and the
  rules that are not obvious from the code. Update them in the same commit as the code.
- Add a route → add an integration test. Add a rule that touches the database → add an integration test.
- Do not add a dependency from a lower domain to a higher one "just this once"; add the interface.
- Prefer a few explicit setters in `app.New` over clever wiring frameworks.

---

## 12. Refactoring an existing codebase to this layout

The typical starting point: one big `service` package with every domain, one `repository` package with one huge
interface file plus a `postgres` implementation, one `handler` package, libraries mixed in with app code. The size of
those packages, not their layering, is the real problem.

Principles: **no behaviour change**, **one step per commit**, **every step passes the full check and can be
deployed**, **pause feature work while a step is open**.

### Step 0: Safety net
- Make one command run gofmt, vet, unit and integration tests (`make check`). If there are no HTTP integration tests
  yet, write a harness and cover the main flows first. This is what makes the moves safe.
- Tag the starting point (`git tag pre-structure`).
- Write the plan (`docs/structure-plan.md`): current packages and sizes, target layout, the domain table (which
  service/storage/handler files go where), the dependency order, the known upward links and their interfaces.

### Step 1: Libraries to `pkg/` (mechanical)
`git mv` every self-contained library (parsers, external clients, generic helpers) with its testdata to `pkg/`,
rewrite import paths, build, vet, test. If a library imports app code, cut that dependency first (pass values in,
or move the shared type into the library).

### Step 2: Platform
Move config, i18n, blob storage and test-database code under `internal/platform/`. Split the old service package's
shared helpers out: user-facing errors → `platform/apperr`, mail → `platform/mailer`, storage sentinels and SQL
helpers → `platform/store`. Keep temporary type aliases in the old package so nothing else changes in this commit.

### Step 3: Delivery to `internal/web`
Move handlers, middleware and templates (fix the `embed` path). File names stay.

### Step 4: Domains, one per commit, bottom up
For each domain in the dependency order (smallest and lowest first; they settle the pattern):
1. create `internal/<domain>/`; move its service file(s) as `service.go` …, its slice of the big repository
   interface as `store.go`, its Postgres file(s) as `postgres.go` …, and its unit tests;
2. rename `XxxService` → `Service`, `NewXxxService` → `New`, `XxxRepository` → `Repository`;
3. where it calls a domain not moved yet, keep importing the old package for now; where a call points up the
   order, introduce the small interface and wire it in `app`;
4. update `app` wiring and the handlers' imports;
5. build, vet, full test suite; commit; deploy when convenient.

### Step 5: Clean up
Delete the empty old packages and the temporary aliases, rename the old repositories aggregate to
`app.Repositories`, rewrite the structure/architecture sections of the docs, remove the tag.

### What not to change during the refactor
Database schema, URLs, template content, behaviour, public APIs. If a bug is found, note it and fix it in a separate
commit after the step.

### Checklist after each step
- [ ] `make check` passes (formatting, vet, unit, integration).
- [ ] No package in `pkg/` imports `internal/`.
- [ ] No domain imports a domain above it; no domain imports `web`.
- [ ] No SQL outside `postgres*.go`, `platform/blobstore`, `platform/testdb`, `app.Migrate`.
- [ ] Docs updated (layout, dependency order, new interfaces).
