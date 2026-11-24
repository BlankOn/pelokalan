// Package app wires repositories, services, handlers and routes together. cmd/server runs it
// with the real configuration; the integration tests run it against a throwaway database.
package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"pelokalan/internal/ai"
	"pelokalan/internal/blobstore"
	"pelokalan/internal/config"
	"pelokalan/internal/glossary"
	"pelokalan/internal/gnomel10n"
	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/handler/web"
	"pelokalan/internal/repository"
	"pelokalan/internal/repository/postgres"
	"pelokalan/internal/service"
)

// Options changes how the application is put together
type Options struct {
	// Root is the project directory holding internal/templates, static and migrations ("." on the server)
	Root string
	// Mailer replaces the configured one (tests capture emails with it)
	Mailer service.Mailer
	// Offline leaves out the GNOME translation site client, so nothing asks the site (tests)
	Offline bool
}

// App is the wired application
type App struct {
	Router http.Handler
	DB     *sql.DB
	Repos  *repository.Repositories
	Blobs  *blobstore.Postgres
	Mailer service.Mailer

	Auth         *service.AuthService
	Projects     *service.ProjectService
	Catalogs     *service.CatalogService
	Glossary     *service.GlossaryService
	Discussions  *service.DiscussionService
	Activity     *service.ActivityService
	Reservations *service.ReservationService
	Wiki         *service.WikiService
	Contexts     *service.ContextService
	AI           *service.AIService
	Thesaurus    *service.ThesaurusService
	Spelling     *service.SpellingService
	Handler      *web.Handler
}

// Migrate brings the database schema up to date with the migrations under root
func Migrate(db config.DatabaseConfig, root string) error {
	abs, err := filepath.Abs(filepath.Join(root, "migrations"))
	if err != nil {
		return err
	}
	m, err := migrate.New("file://"+filepath.ToSlash(abs), db.MigrateURL())
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		m.Close()
		return fmt.Errorf("migrate up: %w", err)
	}
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		return fmt.Errorf("migrate close: %v %v", srcErr, dbErr)
	}
	return nil
}

// New wires the application on an open, migrated database
func New(cfg *config.Config, db *sql.DB, opts Options) (*App, error) {
	if opts.Root == "" {
		opts.Root = "."
	}
	a := &App{DB: db}
	a.Repos = &repository.Repositories{
		User:     postgres.NewUserRepository(db),
		OTP:      postgres.NewOTPRepository(db),
		Project:  postgres.NewProjectRepository(db),
		Catalog:  postgres.NewCatalogRepository(db),
		Glossary: postgres.NewGlossaryRepository(db),
		AI:       postgres.NewAIRepository(db),
		Commit:   postgres.NewCommitRepository(db),
		Discuss:  postgres.NewDiscussionRepository(db),
		Activity: postgres.NewActivityRepository(db),
		Reserve:  postgres.NewReservationRepository(db),
	}
	repos := a.Repos

	// Files (comment attachments) live in Postgres behind an S3-like interface
	a.Blobs = blobstore.NewPostgres(db)

	// Services
	a.Mailer = opts.Mailer
	if a.Mailer == nil {
		a.Mailer = service.NewMailer(cfg.Mail)
	}
	mailer := a.Mailer
	a.Auth = service.NewAuthService(repos.User, repos.OTP, mailer, cfg.Auth, cfg.Site, cfg.JWT)
	a.Projects = service.NewProjectService(repos.Project, repos.User, mailer, a.Auth, cfg.Auth, cfg.Site)
	a.Catalogs = service.NewCatalogService(repos.Catalog, repos.Commit, a.Projects)
	fetcher := glossary.NewFetcher(cfg.Glossary.MaxDownloadMB<<20, cfg.Glossary.AllowPrivateHosts,
		cfg.Site.Name+" glossary fetcher (+"+cfg.Site.BaseURL+")")
	a.Glossary = service.NewGlossaryService(repos.Glossary, fetcher)
	a.Catalogs.SetProjectFiles(repos.Glossary) // git patches against repository files
	a.Discussions = service.NewDiscussionService(repos.Discuss, repos.Project, repos.Glossary, repos.Catalog, a.Blobs, mailer, cfg.Site)
	// activity log for the Insight tab
	a.Activity = service.NewActivityService(repos.Activity)
	a.Activity.SetBadgeMail(repos.User, mailer, cfg.Site, a.Auth.IsAllowed) // emails about new badges
	// module reservations (Settings → Umum): claimed on edits, released by people, merges and expiry
	a.Reservations = service.NewReservationService(repos.Reserve, repos.Project, repos.User, mailer, cfg.Site)
	a.Catalogs.SetReservations(a.Reservations)
	a.Auth.SetActivity(a.Activity)
	a.Glossary.SetProjects(repos.Project)
	a.Glossary.SetLandings(a.Catalogs) // members' work accepted upstream, after each import
	if !opts.Offline {
		// state of project files on the GNOME translation site (read-only API, cached, rate limited)
		a.Glossary.SetUpstream(gnomel10n.New(gnomel10n.DefaultBase, false,
			cfg.Site.Name+" upstream status (+"+cfg.Site.BaseURL+")"))
	}
	a.Catalogs.SetActivity(a.Activity)
	a.Discussions.SetActivity(a.Activity)
	a.Discussions.SetNotifications(postgres.NewNotificationRepository(db))
	a.Wiki = service.NewWikiService(postgres.NewWikiRepository(db), a.Blobs)
	a.Contexts = service.NewContextService(postgres.NewContextRepository(db))
	a.AI = service.NewAIService(repos.AI, repos.Catalog, repos.Glossary, ai.NewBox(cfg.AI.SecretKey), cfg.AI.AllowPrivateHosts)
	a.AI.SetHistory(a.Catalogs)
	a.AI.SetContexts(a.Contexts)

	// Handlers
	h, err := web.NewHandler(
		filepath.Join(opts.Root, "internal", "templates"),
		cfg.Site,
		cfg.Server,
		a.Auth,
		a.Projects,
		a.Catalogs,
		a.Glossary,
		a.AI,
		a.Discussions,
		a.Wiki,
		a.Contexts,
	)
	if err != nil {
		return nil, fmt.Errorf("web handler: %w", err)
	}
	a.Handler = h
	// Indonesian thesaurus and spelling check: loaded by the caller (cmd/server downloads them on first start)
	a.Thesaurus = service.NewThesaurusService(postgres.NewThesaurusRepository(db), fetcher, cfg.Glossary.ThesaurusURL)
	h.SetThesaurus(a.Thesaurus)
	a.Spelling = service.NewSpellingService(postgres.NewSpellingRepository(db), fetcher, cfg.Glossary.SpellingURL)
	h.SetSpelling(a.Spelling)
	h.SetSyncInterval(cfg.Glossary.FetchInterval)
	h.SetActivity(a.Activity)
	h.SetReservations(a.Reservations)
	// PO files can also be created from a URL: same SSRF-safe transport, upload size limit
	h.SetPOFetcher(glossary.NewFetcher(cfg.Server.MaxUploadMB<<20, cfg.Glossary.AllowPrivateHosts,
		cfg.Site.Name+" PO fetcher (+"+cfg.Site.BaseURL+")"))
	authMW := middleware.NewAuth(a.Auth)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.GetHead) // HEAD is answered like GET (link preview crawlers check links with it)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.SameOrigin)
	r.Use(middleware.Lang)

	fileServer := http.FileServer(http.Dir(filepath.Join(opts.Root, "static")))
	r.Handle("/static/*", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// revalidate on every load so a deploy never serves stale scripts
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Public pages: projects, their glossary and members are readable without signing in
	r.Group(func(r chi.Router) {
		r.Use(authMW.Optional)

		r.Get("/", h.Home)
		r.Get("/lang", h.SetLang)
		r.Get("/login", h.LoginPage)
		r.Post("/login", h.Login)
		r.Get("/register", h.RegisterPage)
		r.Post("/register", h.Register)
		r.Post("/verify", h.Verify)
		r.Post("/verify/resend", h.ResendCode)
		r.Post("/logout", h.Logout)

		r.Get("/projects", h.Projects)
		r.Get("/projects/{id}", h.ProjectHome)
		r.Get("/projects/{id}/glossary", h.GlossaryPage)
		r.Get("/projects/{id}/glossary/search", h.GlossarySearch)
		r.Get("/projects/{id}/members", h.MembersPage)
		r.Get("/api/projects/{id}/members", h.APIMembers)                // public JSON, no key
		r.Get("/users/{username}", h.UserPage)                           // public profile: stats and badges
		r.Get("/users/{username}/badges/{key}/{tier}", h.BadgeSharePage) // share page, or its preview (.png)
		r.Get("/git/{token}", h.GitPatchByLink)                          // signed patch link for curl | git am
		r.Get("/projects/{id}/insight", h.InsightPage)
		r.Get("/join/{token}", h.JoinPage)
		r.Get("/projects/{id}/wiki", h.WikiIndex)
		r.Get("/licenses", h.LicensesPage)
		r.Get("/about", h.AboutPage)
		r.Get("/projects/{id}/contexts", h.ContextsPage)
		r.Get("/projects/{id}/project-files", h.ProjectFilesMore)
		r.Get("/projects/{id}/my-files", h.MyFiles)
		r.Get("/projects/{id}/group-chart", h.GroupChart)
		r.Get("/projects/{id}/member-files", h.MemberFiles)
		r.Get("/projects/{id}/project-files/{fid}/download", h.ProjectFileDownload)
		r.Get("/projects/{id}/topics/{key}", h.TopicPage)
		r.Get("/projects/{id}/threads/{type}/{key}", h.ThreadPage)
		r.Get("/projects/{id}/wiki/files/{fid}/{filename}", h.WikiFile)
		r.Get("/projects/{id}/wiki/{slug}", h.WikiPage)
		r.Get("/projects/{id}/glossary/{hash}", h.GlossaryTermPage)
		r.Get("/projects/{id}/discussions", h.DiscussionsPage)
		r.Get("/projects/{id}/discussions/{type}/{key}", h.ThreadPartial)
		r.Get("/projects/{id}/attachments/{aid}/{filename}", h.Attachment)

		// PO files belong to the project and can be read by anyone who can read it
		r.Get("/projects/{id}/files", h.ProjectPage)
		r.Get("/projects/{id}/{username}/po/{filename}", h.Editor)
		r.Get("/projects/{id}/{username}/po/{filename}/history", h.History)
		r.Get("/projects/{id}/{username}/po/{filename}/raw", h.RawEditor)
		r.Get("/catalogs/{cid}", h.CatalogRedirect)
		r.Get("/catalogs/{cid}/list", h.EntryList)
		r.Get("/catalogs/{cid}/entries/{eid}", h.EntryEditor)
		r.Get("/catalogs/{cid}/download", h.Download)
		r.Get("/catalogs/{cid}/download.mo", h.DownloadMO)
		r.Get("/catalogs/{cid}/raw", h.CatalogRedirect)
		r.Get("/catalogs/{cid}/history", h.CatalogRedirect)
		r.Get("/catalogs/{cid}/history/working", h.HistoryDiff)
		r.Get("/catalogs/{cid}/history/{hid}", h.HistoryDiff)
		r.Get("/catalogs/{cid}/history/{hid}/download", h.DownloadCommit)
		r.Get("/catalogs/{cid}/ai/jobs/{jid}", h.AIJobStatus)
	})

	// Signed-in pages
	r.Group(func(r chi.Router) {
		r.Use(authMW.Required)

		r.Get("/profile", h.ProfilePage)
		r.Post("/profile", h.UpdateProfile)
		r.Post("/profile/identity", h.UpdateIdentity)
		r.Post("/profile/badge-emails", h.UpdateBadgeEmails)
		r.Post("/projects/{id}/reservations", h.ReserveModule)
		r.Post("/projects/{id}/reservations/release", h.ReleaseModule)
		r.Post("/editor-settings", h.UpdateEditorSettings)

		r.Get("/invitations", h.InvitationsPage)
		r.Post("/invitations/{iid}/accept", h.AcceptInvitation)
		r.Post("/invitations/{iid}/decline", h.DeclineInvitation)

		r.Post("/projects", h.CreateProject)

		// Changes to PO files (the owner only; checked per file)
		r.Post("/catalogs/{cid}/entries/{eid}", h.SaveEntry)
		r.Post("/catalogs/{cid}/context", h.SaveCatalogContext)
		r.Post("/catalogs/{cid}/entries/{eid}/ai", h.AITranslateEntry)
		r.Post("/catalogs/{cid}/update", h.UpdateFromTemplate)
		r.Post("/catalogs/{cid}/pretranslate", h.PreTranslate)
		r.Post("/catalogs/{cid}/replace", h.ReplaceCatalog)
		r.Post("/catalogs/{cid}/delete", h.DeleteCatalog)
		r.Post("/catalogs/{cid}/raw", h.SaveRaw)
		r.Post("/catalogs/{cid}/properties", h.UpdateProperties)
		r.Post("/catalogs/{cid}/history/{hid}/restore", h.RestoreCommit)
		r.Post("/catalogs/{cid}/history/{hid}/message", h.EditCommitMessage)
		r.Get("/catalogs/{cid}/commit/preview", h.CommitPreview)
		r.Post("/catalogs/{cid}/commit", h.CommitCatalog)
		r.Post("/catalogs/{cid}/history/discard", h.DiscardChanges)
		r.Get("/catalogs/{cid}/ai/estimate", h.AIEstimate)
		r.Post("/catalogs/{cid}/ai", h.AIStart)
		r.Post("/catalogs/{cid}/ai/jobs/{jid}/cancel", h.AICancel)
		r.Post("/projects/{id}/upload", h.UploadCatalog)
		r.Post("/projects/{id}/translations", h.CreateTranslation)
		r.Post("/projects/{id}/update-all", h.UpdateAllFromTemplate)

		// Glossary management
		// Module groups (Settings → Kelompok modul)
		r.Post("/projects/{id}/module-groups/sources", h.AddModuleGroupSource)
		r.Post("/projects/{id}/module-groups/import", h.ImportModuleGroup)
		r.Post("/projects/{id}/module-groups/sync", h.SetModuleGroupSync)
		r.Post("/projects/{id}/module-groups/imports/{iid}/delete", h.DeleteModuleGroupImport)
		r.Post("/projects/{id}/module-groups/{gid}/delete", h.DeleteModuleGroup)
		r.Post("/projects/{id}/module-groups/{gid}/fetch", h.FetchModuleGroup)
		r.Post("/projects/{id}/module-groups/{gid}/unsync", h.RemoveModuleGroupSync)

		// Members and settings
		r.Post("/projects/{id}/discussions/{type}/{key}/comments", h.PostComment)
		r.Post("/projects/{id}/discussions/{type}/{key}/delete", h.DeleteDiscussion)
		r.Post("/projects/{id}/comments/{cid}/edit", h.EditComment)
		r.Post("/projects/{id}/comments/{cid}/delete", h.DeleteComment)
		r.Post("/projects/{id}/members/invite", h.Invite)
		r.Post("/projects/{id}/members/{uid}/role", h.ChangeMemberRole)
		r.Post("/projects/{id}/members/{uid}/remove", h.RemoveMember)
		r.Post("/projects/{id}/invitations/{iid}/cancel", h.CancelInvitation)
		r.Post("/projects/{id}/invite-links", h.CreateInviteLink)
		r.Post("/projects/{id}/invite-links/{lid}/delete", h.DeleteInviteLink)
		r.Post("/join/{token}", h.Join)
		r.Post("/projects/{id}/join", h.JoinProject)
		r.Get("/projects/{id}/wiki/new", h.WikiNew)
		r.Get("/projects/{id}/thesaurus", h.ThesaurusLookup)
		r.Post("/catalogs/{cid}/spell", h.SpellCheck)
		r.Get("/projects/{id}/spell/suggest", h.SpellSuggest)
		r.Post("/projects/{id}/dictionary", h.AddDictionaryWord)
		r.Post("/projects/{id}/dictionary/delete", h.DeleteDictionaryWord)
		r.Get("/catalogs/{cid}/patch", h.CatalogGitPatch)
		r.Post("/projects/{id}/project-files/{fid}/take", h.ProjectFileTake)
		r.Post("/projects/{id}/project-files/sync", h.ProjectFilesSync)
		r.Get("/projects/{id}/project-files/sync", h.ProjectFilesSyncStatus)
		r.Get("/projects/{id}/topics/new", h.TopicNew)
		r.Post("/projects/{id}/topics/new", h.TopicCreate)
		r.Post("/projects/{id}/wiki/new", h.WikiCreate)
		r.Post("/projects/{id}/wiki/preview", h.WikiPreview)
		r.Post("/projects/{id}/wiki/files", h.WikiUpload)
		r.Get("/projects/{id}/wiki/{slug}/edit", h.WikiEdit)
		r.Post("/projects/{id}/wiki/{slug}/edit", h.WikiSave)
		r.Post("/projects/{id}/wiki/{slug}/delete", h.WikiDelete)
		r.Post("/projects/{id}/wiki/{slug}/move", h.WikiMove)
		r.Get("/projects/{id}/settings", h.SettingsPage)
		r.Post("/projects/{id}/settings", h.UpdateSettings)
		r.Post("/projects/{id}/notifications", h.SaveNotifications)
		r.Post("/projects/{id}/delete", h.DeleteProject)
		r.Post("/projects/{id}/ai", h.SaveAISettings)
		r.Post("/projects/{id}/ai/test", h.TestAISettings)
		r.Post("/projects/{id}/ai/delete", h.DeleteAISettings)

	})

	a.Router = r
	return a, nil
}
