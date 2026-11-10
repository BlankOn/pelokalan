package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"

	"pelokalan/internal/ai"
	"pelokalan/internal/blobstore"
	"pelokalan/internal/config"
	"pelokalan/internal/glossary"
	"pelokalan/internal/handler/middleware"
	"pelokalan/internal/handler/web"
	"pelokalan/internal/i18n"
	"pelokalan/internal/repository"
	"pelokalan/internal/repository/postgres"
	"pelokalan/internal/service"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := config.Load()
	i18n.Default = i18n.Normalize(cfg.Site.DefaultLang)

	if cfg.Auth.Whitelist.Len() == 0 {
		log.Println("EMAIL_WHITELIST is empty: registration and sign-in are open to every email address")
	} else {
		log.Printf("Email whitelist: %d entries", cfg.Auth.Whitelist.Len())
	}

	// Connect to database
	db, err := sql.Open("pgx", cfg.Database.DSN())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Run database migrations
	m, err := migrate.New("file://migrations", cfg.Database.MigrateURL())
	if err != nil {
		log.Fatalf("Failed to create migrate instance: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		log.Fatalf("Failed to close migrate: %v %v", srcErr, dbErr)
	}
	log.Println("Database migrations completed")

	// Repositories
	repos := &repository.Repositories{
		User:     postgres.NewUserRepository(db),
		OTP:      postgres.NewOTPRepository(db),
		Project:  postgres.NewProjectRepository(db),
		Catalog:  postgres.NewCatalogRepository(db),
		Glossary: postgres.NewGlossaryRepository(db),
		AI:       postgres.NewAIRepository(db),
		Commit:   postgres.NewCommitRepository(db),
		Discuss:  postgres.NewDiscussionRepository(db),
	}

	// Files (comment attachments) live in Postgres behind an S3-like interface
	blobs := blobstore.NewPostgres(db)

	// Services
	mailer := service.NewMailer(cfg.Mail)
	authService := service.NewAuthService(repos.User, repos.OTP, mailer, cfg.Auth, cfg.Site, cfg.JWT)
	projectService := service.NewProjectService(repos.Project, repos.User, mailer, authService, cfg.Auth, cfg.Site)
	catalogService := service.NewCatalogService(repos.Catalog, repos.Commit, projectService)
	fetcher := glossary.NewFetcher(cfg.Glossary.MaxDownloadMB<<20, cfg.Glossary.AllowPrivateHosts,
		cfg.Site.Name+" glossary fetcher (+"+cfg.Site.BaseURL+")")
	glossaryService := service.NewGlossaryService(repos.Glossary, fetcher)
	discussionService := service.NewDiscussionService(repos.Discuss, repos.Project, repos.Glossary, repos.Catalog, blobs, mailer, cfg.Site)
	discussionService.SetNotifications(postgres.NewNotificationRepository(db))
	wikiService := service.NewWikiService(postgres.NewWikiRepository(db), blobs)
	contextService := service.NewContextService(postgres.NewContextRepository(db))
	aiService := service.NewAIService(repos.AI, repos.Catalog, repos.Glossary, ai.NewBox(cfg.AI.SecretKey), cfg.AI.AllowPrivateHosts)
	aiService.SetHistory(catalogService)
	aiService.SetContexts(contextService)
	if err := aiService.RecoverJobs(context.Background()); err != nil {
		log.Printf("AI: recover jobs: %v", err)
	}

	// Handlers
	webHandler, err := web.NewHandler(
		filepath.Join("internal", "templates"),
		cfg.Site,
		cfg.Server,
		authService,
		projectService,
		catalogService,
		glossaryService,
		aiService,
		discussionService,
		wikiService,
		contextService,
	)
	if err != nil {
		log.Fatalf("Failed to initialize web handler: %v", err)
	}
	// Indonesian thesaurus for the editor: stored on first start, then served from memory
	thesaurusService := service.NewThesaurusService(postgres.NewThesaurusRepository(db), fetcher, cfg.Glossary.ThesaurusURL)
	webHandler.SetThesaurus(thesaurusService)
	go func() {
		for {
			err := thesaurusService.Load(context.Background())
			if err == nil {
				return
			}
			log.Printf("thesaurus: %v (retrying in an hour)", err)
			time.Sleep(time.Hour)
		}
	}()
	// PO files can also be created from a URL: same SSRF-safe transport, upload size limit
	webHandler.SetSyncInterval(cfg.Glossary.FetchInterval)
	webHandler.SetPOFetcher(glossary.NewFetcher(cfg.Server.MaxUploadMB<<20, cfg.Glossary.AllowPrivateHosts,
		cfg.Site.Name+" PO fetcher (+"+cfg.Site.BaseURL+")"))
	authMiddleware := middleware.NewAuth(authService)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.SameOrigin)
	r.Use(middleware.Lang)

	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// revalidate on every load so a deploy never serves stale scripts
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Public pages: projects, their glossary and members are readable without signing in
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.Optional)

		r.Get("/", webHandler.Home)
		r.Get("/lang", webHandler.SetLang)
		r.Get("/login", webHandler.LoginPage)
		r.Post("/login", webHandler.Login)
		r.Get("/register", webHandler.RegisterPage)
		r.Post("/register", webHandler.Register)
		r.Post("/verify", webHandler.Verify)
		r.Post("/verify/resend", webHandler.ResendCode)
		r.Post("/logout", webHandler.Logout)

		r.Get("/projects", webHandler.Projects)
		r.Get("/projects/{id}", webHandler.ProjectHome)
		r.Get("/projects/{id}/glossary", webHandler.GlossaryPage)
		r.Get("/projects/{id}/glossary/search", webHandler.GlossarySearch)
		r.Get("/projects/{id}/members", webHandler.MembersPage)
		r.Get("/join/{token}", webHandler.JoinPage)
		r.Get("/projects/{id}/wiki", webHandler.WikiIndex)
		r.Get("/licenses", webHandler.LicensesPage)
		r.Get("/projects/{id}/contexts", webHandler.ContextsPage)
		r.Get("/projects/{id}/project-files", webHandler.ProjectFilesMore)
		r.Get("/projects/{id}/my-files", webHandler.MyFiles)
		r.Get("/projects/{id}/group-chart", webHandler.GroupChart)
		r.Get("/projects/{id}/member-files", webHandler.MemberFiles)
		r.Get("/projects/{id}/project-files/{fid}/download", webHandler.ProjectFileDownload)
		r.Get("/projects/{id}/topics/{key}", webHandler.TopicPage)
		r.Get("/projects/{id}/threads/{type}/{key}", webHandler.ThreadPage)
		r.Get("/projects/{id}/wiki/files/{fid}/{filename}", webHandler.WikiFile)
		r.Get("/projects/{id}/wiki/{slug}", webHandler.WikiPage)
		r.Get("/projects/{id}/glossary/{hash}", webHandler.GlossaryTermPage)
		r.Get("/projects/{id}/discussions", webHandler.DiscussionsPage)
		r.Get("/projects/{id}/discussions/{type}/{key}", webHandler.ThreadPartial)
		r.Get("/projects/{id}/attachments/{aid}/{filename}", webHandler.Attachment)

		// PO files belong to the project and can be read by anyone who can read it
		r.Get("/projects/{id}/files", webHandler.ProjectPage)
		r.Get("/projects/{id}/{username}/po/{filename}", webHandler.Editor)
		r.Get("/projects/{id}/{username}/po/{filename}/history", webHandler.History)
		r.Get("/projects/{id}/{username}/po/{filename}/raw", webHandler.RawEditor)
		r.Get("/catalogs/{cid}", webHandler.CatalogRedirect)
		r.Get("/catalogs/{cid}/list", webHandler.EntryList)
		r.Get("/catalogs/{cid}/entries/{eid}", webHandler.EntryEditor)
		r.Get("/catalogs/{cid}/download", webHandler.Download)
		r.Get("/catalogs/{cid}/download.mo", webHandler.DownloadMO)
		r.Get("/catalogs/{cid}/raw", webHandler.CatalogRedirect)
		r.Get("/catalogs/{cid}/history", webHandler.CatalogRedirect)
		r.Get("/catalogs/{cid}/history/working", webHandler.HistoryDiff)
		r.Get("/catalogs/{cid}/history/{hid}", webHandler.HistoryDiff)
		r.Get("/catalogs/{cid}/history/{hid}/download", webHandler.DownloadCommit)
		r.Get("/catalogs/{cid}/ai/jobs/{jid}", webHandler.AIJobStatus)
	})

	// Signed-in pages
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.Required)

		r.Get("/profile", webHandler.ProfilePage)
		r.Post("/profile", webHandler.UpdateProfile)
		r.Post("/profile/identity", webHandler.UpdateIdentity)

		r.Get("/invitations", webHandler.InvitationsPage)
		r.Post("/invitations/{iid}/accept", webHandler.AcceptInvitation)
		r.Post("/invitations/{iid}/decline", webHandler.DeclineInvitation)

		r.Post("/projects", webHandler.CreateProject)

		// Changes to PO files (the owner only; checked per file)
		r.Post("/catalogs/{cid}/entries/{eid}", webHandler.SaveEntry)
		r.Post("/catalogs/{cid}/context", webHandler.SaveCatalogContext)
		r.Post("/catalogs/{cid}/entries/{eid}/ai", webHandler.AITranslateEntry)
		r.Post("/catalogs/{cid}/update", webHandler.UpdateFromTemplate)
		r.Post("/catalogs/{cid}/pretranslate", webHandler.PreTranslate)
		r.Post("/catalogs/{cid}/replace", webHandler.ReplaceCatalog)
		r.Post("/catalogs/{cid}/delete", webHandler.DeleteCatalog)
		r.Post("/catalogs/{cid}/raw", webHandler.SaveRaw)
		r.Post("/catalogs/{cid}/properties", webHandler.UpdateProperties)
		r.Post("/catalogs/{cid}/history/{hid}/restore", webHandler.RestoreCommit)
		r.Get("/catalogs/{cid}/commit/preview", webHandler.CommitPreview)
		r.Post("/catalogs/{cid}/commit", webHandler.CommitCatalog)
		r.Post("/catalogs/{cid}/history/discard", webHandler.DiscardChanges)
		r.Get("/catalogs/{cid}/ai/estimate", webHandler.AIEstimate)
		r.Post("/catalogs/{cid}/ai", webHandler.AIStart)
		r.Post("/catalogs/{cid}/ai/jobs/{jid}/cancel", webHandler.AICancel)
		r.Post("/projects/{id}/upload", webHandler.UploadCatalog)
		r.Post("/projects/{id}/translations", webHandler.CreateTranslation)
		r.Post("/projects/{id}/update-all", webHandler.UpdateAllFromTemplate)

		// Glossary management
		r.Post("/projects/{id}/glossary/sources", webHandler.GlossaryAddSource)
		r.Post("/projects/{id}/glossary/versions/{vid}/delete", webHandler.GlossaryDeleteVersion)
		r.Post("/projects/{id}/glossary/import", webHandler.GlossaryImport)
		r.Post("/projects/{id}/glossary/imports/{gid}/delete", webHandler.GlossaryDeleteImport)
		r.Post("/projects/{id}/glossary/feeds", webHandler.GlossarySetFeed)
		r.Post("/projects/{id}/glossary/versions/{vid}/fetch", webHandler.GlossaryFetchNow)
		r.Post("/projects/{id}/glossary/versions/{vid}/unfeed", webHandler.GlossaryRemoveFeed)

		// Members and settings
		r.Post("/projects/{id}/discussions/{type}/{key}/comments", webHandler.PostComment)
		r.Post("/projects/{id}/discussions/{type}/{key}/delete", webHandler.DeleteDiscussion)
		r.Post("/projects/{id}/comments/{cid}/edit", webHandler.EditComment)
		r.Post("/projects/{id}/comments/{cid}/delete", webHandler.DeleteComment)
		r.Post("/projects/{id}/members/invite", webHandler.Invite)
		r.Post("/projects/{id}/members/{uid}/role", webHandler.ChangeMemberRole)
		r.Post("/projects/{id}/members/{uid}/remove", webHandler.RemoveMember)
		r.Post("/projects/{id}/invitations/{iid}/cancel", webHandler.CancelInvitation)
		r.Post("/projects/{id}/invite-links", webHandler.CreateInviteLink)
		r.Post("/projects/{id}/invite-links/{lid}/delete", webHandler.DeleteInviteLink)
		r.Post("/join/{token}", webHandler.Join)
		r.Get("/projects/{id}/wiki/new", webHandler.WikiNew)
		r.Get("/projects/{id}/thesaurus", webHandler.ThesaurusLookup)
		r.Post("/projects/{id}/project-files/{fid}/take", webHandler.ProjectFileTake)
		r.Post("/projects/{id}/project-files/sync", webHandler.ProjectFilesSync)
		r.Get("/projects/{id}/project-files/sync", webHandler.ProjectFilesSyncStatus)
		r.Get("/projects/{id}/topics/new", webHandler.TopicNew)
		r.Post("/projects/{id}/topics/new", webHandler.TopicCreate)
		r.Post("/projects/{id}/wiki/new", webHandler.WikiCreate)
		r.Post("/projects/{id}/wiki/preview", webHandler.WikiPreview)
		r.Post("/projects/{id}/wiki/files", webHandler.WikiUpload)
		r.Get("/projects/{id}/wiki/{slug}/edit", webHandler.WikiEdit)
		r.Post("/projects/{id}/wiki/{slug}/edit", webHandler.WikiSave)
		r.Post("/projects/{id}/wiki/{slug}/delete", webHandler.WikiDelete)
		r.Post("/projects/{id}/wiki/{slug}/move", webHandler.WikiMove)
		r.Get("/projects/{id}/settings", webHandler.SettingsPage)
		r.Post("/projects/{id}/settings", webHandler.UpdateSettings)
		r.Post("/projects/{id}/notifications", webHandler.SaveNotifications)
		r.Post("/projects/{id}/delete", webHandler.DeleteProject)
		r.Post("/projects/{id}/ai", webHandler.SaveAISettings)
		r.Post("/projects/{id}/ai/test", webHandler.TestAISettings)
		r.Post("/projects/{id}/ai/delete", webHandler.DeleteAISettings)

	})

	// Download glossary sources that are due (checked every 15 minutes, each URL once per interval)
	// and record each module group's totals for the progress chart (at most hourly)
	go func() {
		glossaryService.FetchDue(context.Background(), cfg.Glossary.FetchInterval)
		glossaryService.RecordStats(context.Background())
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for range t.C {
			glossaryService.FetchDue(context.Background(), cfg.Glossary.FetchInterval)
			glossaryService.RecordStats(context.Background())
		}
	}()

	// Remove stored files no attachment uses any more (like a bucket lifecycle rule)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			if n, err := blobs.GC(context.Background(), 24*time.Hour); err != nil {
				log.Printf("blob gc: %v", err)
			} else if n > 0 {
				log.Printf("blob gc: removed %d unused files", n)
			}
		}
	}()

	// Periodically remove stale one-time codes
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			if err := authService.Cleanup(context.Background()); err != nil {
				log.Printf("cleanup: %v", err)
			}
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		log.Printf("Server starting on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	log.Println("Server stopped")
}
