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
	"syscall"
	"time"
	_ "time/tzdata" // SITE_TIMEZONE works without the system's zone files

	_ "github.com/jackc/pgx/v5/stdlib"

	"pelokalan/internal/activity"
	"pelokalan/internal/app"
	"pelokalan/internal/catalog"
	"pelokalan/internal/platform/config"
	"pelokalan/internal/platform/i18n"
	"pelokalan/internal/web"
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
	if err := app.Migrate(cfg.Database, "."); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations completed")

	a, err := app.New(cfg, db, app.Options{Root: "."})
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	repos := a.Repos
	blobs := a.Blobs
	authService := a.Auth
	catalogService := a.Catalogs
	glossaryService := a.Glossary
	activityService := a.Activity
	reservationService := a.Reservations
	if err := a.AI.RecoverJobs(context.Background()); err != nil {
		log.Printf("AI: recover jobs: %v", err)
	}
	if gh := a.Glossary.GitHubApp(); gh != nil { // say whether the GitHub App's id and key work
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if info, err := gh.App(ctx); err != nil {
				log.Printf("GitHub App: %v", err)
			} else {
				log.Printf("GitHub App: %s (%s), owned by %s", info.Name, info.Slug, info.Owner.Login)
			}
		}()
	} else {
		log.Printf("GitHub App: not configured (full integration with repositories is off)")
	}
	// Indonesian thesaurus and spelling word list: stored on first start, then served from memory
	for _, load := range []struct {
		name string
		fn   func(context.Context) error
	}{{"thesaurus", a.Thesaurus.Load}, {"spelling", a.Spelling.Load}} {
		go func() {
			for {
				err := load.fn(context.Background())
				if err == nil {
					return
				}
				log.Printf("%s: %v (retrying in an hour)", load.name, err)
				time.Sleep(time.Hour)
			}
		}()
	}

	// Download glossary sources that are due (checked every 15 minutes, each URL once per interval)
	// and record each module group's totals for the progress chart (at most hourly)
	go func() {
		// upstream states first (one release page per release and language, seconds), so a
		// deploy shows them right away; FetchDue may spend minutes downloading archives
		glossaryService.WarmUpstream(context.Background())
		glossaryService.FetchDue(context.Background(), cfg.Glossary.FetchInterval)
		glossaryService.RecordStats(context.Background())
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for range t.C {
			glossaryService.FetchDue(context.Background(), cfg.Glossary.FetchInterval)
			glossaryService.RecordStats(context.Background())
		}
	}()

	// Badges and one-time catch-ups, in order: (1) once, record members' work accepted upstream during the
	// month before detection existed (safe to delete after it ran); (2) once, record everyone's current
	// badges silently (BadgeAwardsBaselineJob); (3) every hour, record new badge tiers and email them.
	go func() {
		ctx := context.Background()
		if claimed, err := activityService.ClaimOneTimeJob(ctx, catalog.BackfillLandingsJob); err == nil && claimed {
			if projects, err := repos.Project.ListAll(ctx, 0); err != nil {
				log.Printf("upstream landings backfill: %v", err)
			} else {
				n, err := catalogService.BackfillLandings(ctx, projects, time.Now().AddDate(0, -1, 0))
				log.Printf("upstream landings backfill: %d strings accepted upstream recorded (err: %v)", n, err)
			}
		}
		if claimed, err := activityService.ClaimOneTimeJob(ctx, activity.BadgeAwardsBaselineJob); err == nil && claimed {
			_, err := activityService.AwardBadges(ctx, web.SiteLocation(), time.Now(), false)
			log.Printf("badges: baseline recorded silently (err: %v)", err)
		}
		for {
			if n, err := reservationService.Expire(ctx, time.Now()); err != nil {
				log.Printf("reservations: %v", err)
			} else if n > 0 {
				log.Printf("reservations: %d expired", n)
			}
			if n, err := activityService.AwardBadges(ctx, web.SiteLocation(), time.Now(), true); err != nil {
				log.Printf("badges: %v", err)
			} else if n > 0 {
				log.Printf("badges: emailed %d people about new badges", n)
			}
			time.Sleep(time.Hour)
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
		Handler:      a.Router,
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
