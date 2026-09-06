// Command server runs the ytdm music downloader backend.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/config"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/httpx"
	"ytdm/backend/internal/jobs"
	libsvc "ytdm/backend/internal/library"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/deezer"
	"ytdm/backend/internal/provider/genius"
	"ytdm/backend/internal/provider/spotify"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/provider/ytmusic"
	"ytdm/backend/internal/resolve"
	"ytdm/backend/internal/settings"
	"ytdm/backend/internal/storage"
	"ytdm/backend/internal/subscriptions"
	"ytdm/backend/internal/update"
	"ytdm/backend/internal/ytdlp"
)

// version is set at build time with
// -ldflags "-X main.version=$(cat .release-version)".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ytdm: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to the configuration file (default: ./config.yaml when present)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg.Logging.Level, cfg.Logging.Format).
		With("service", "ytdm", "version", version)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := build(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.close()

	return app.serve(ctx)
}

// application holds everything the running server needs.
type application struct {
	cfg         config.Config
	logger      *slog.Logger
	db          *database.DB
	manager     *jobs.Manager
	broker      *jobs.Broker
	router      http.Handler
	authLimiter *auth.Limiter

	// subscriptions and scheduler are nil only if the wiring failed; the
	// scheduler is additionally nil when the periodic sync is switched off.
	subscriptions  *subscriptions.Service
	scheduler      *subscriptions.Scheduler
	libraryService *libsvc.Service
}

// build wires the whole backend together.
func build(ctx context.Context, cfg config.Config, logger *slog.Logger) (*application, error) {
	db, err := database.Open(ctx, database.Options{
		URL:             cfg.Database.URL,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
		StartupTimeout:  cfg.Database.StartupTimeout,
		StartupBackoff:  cfg.Database.StartupBackoff,
		Logger:          logger,
	})
	if err != nil {
		return nil, err
	}
	// Only the redacted URL is ever logged; the password never reaches a log
	// line, a health response or an error message.
	logger.Info("database ready",
		"target", db.Target(),
		"url", cfg.Database.Redacted(),
		"max_conns", cfg.Database.MaxConns)

	catalogRepo := repository.NewCatalog(db)
	jobsRepo := repository.NewJobs(db)
	filesRepo := repository.NewFiles(db)
	settingsRepo := repository.NewSettings(db)
	subscriptionsRepo := repository.NewSubscriptions(db)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	auditRepo := repository.NewAudit(db)

	if recovered, err := auditRepo.RecoverRunningRuns(ctx); err == nil && recovered > 0 {
		logger.Info("recovered stale running audit runs", "count", recovered)
	}

	middleware.SetTrustedProxies(cfg.Server.TrustedProxies)
	authLimiter := auth.NewLimiter(5, 5*time.Minute)
	authService := auth.NewService(usersRepo, sessionsRepo, authLimiter, logger)
	authService.StartCleanupLoop(ctx, time.Hour)

	library, err := storage.NewLibrary(cfg.Library.Path)
	if err != nil {
		db.Close()
		return nil, err
	}
	storageGuard := storage.NewStorageGuard(cfg.Library.Path, cfg.Library.StorageGuardID, cfg.Library.MinFreeBytes)
	library.SetGuard(storageGuard)
	logger.Info("library ready", "path", library.Root(), "guard_configured", cfg.Library.StorageGuardID != "")

	stagingManager, err := storage.NewStagingManager(cfg.Downloads.StagingDir, cfg.Downloads.StagingMinFreeBytes, cfg.Downloads.StagingMaxBytes)
	if err != nil {
		db.Close()
		return nil, err
	}
	logger.Info("staging ready", "path", stagingManager.Root())

	ffmpegRunner := ffmpeg.New(cfg.Tools.FFmpegPath, cfg.Tools.Timeout)
	prober := downloader.NewProber(downloader.ProberOptions{
		Binary:  cfg.Tools.FFprobePath,
		Timeout: cfg.Tools.Timeout,
		Logger:  logger,
	})
	ytdlpClient := ytdlp.New(ytdlp.Options{
		Binary:         cfg.Tools.YTDLPPath,
		CookieFile:     cfg.Tools.CookieFile,
		PlayerClients:  cfg.Tools.PlayerClients,
		Timeout:        cfg.Tools.Timeout,
		FFmpegLocation: ffmpegLocation(cfg.Tools.FFmpegPath),
		Logger:         logger,
	})

	registry, err := buildProviders(cfg, ytdlpClient, logger)
	if err != nil {
		db.Close()
		return nil, err
	}

	engine := matcher.New(matcher.Options{
		MinScore:            cfg.Matching.MinScore,
		DurationToleranceMS: cfg.Matching.DurationToleranceMS,
	})

	discographyService, err := discography.NewService(discography.Options{
		Registry:            registry,
		DurationToleranceMS: cfg.Matching.DurationToleranceMS,
		Logger:              logger,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	audioDownloader, err := downloader.New(downloader.Options{
		YTDLP:               ytdlpClient,
		FFmpeg:              ffmpegRunner,
		Prober:              prober,
		AllowTranscode:      cfg.Downloads.AllowTranscode,
		DurationToleranceMS: durationVerifyTolerance(cfg.Matching.DurationToleranceMS),
		Retries:             cfg.Downloads.MaxRetries,
		Logger:              logger,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	tagger := metadata.NewTagger(ffmpegRunner)
	broker := jobs.NewBroker(logger)

	lyricsProviders := []provider.LyricsProvider{
		lyrics.NewLRCLib(lyrics.LRCLibConfig{
			Client: httpx.New(cfg.Providers.HTTPTimeout),
		}),
	}
	if cfg.Providers.YTMusic.Enabled {
		lyricsProviders = append(lyricsProviders, ytmusic.NewLyricsProvider(ytmusic.Config{
			BaseURL:    cfg.Providers.YTMusic.BaseURL,
			HTTPClient: httpx.New(cfg.Providers.HTTPTimeout),
		}))
	}
	geniusLyricsProvider := genius.NewLyricsProvider(genius.Config{
		Enabled:     cfg.Providers.Genius.Enabled,
		AccessToken: cfg.Providers.Genius.AccessToken,
		HTTPClient:  httpx.New(cfg.Providers.HTTPTimeout),
		Logger:      logger,
	})
	lyricsProviders = append(lyricsProviders, geniusLyricsProvider)

	lyricsResolver := lyrics.NewResolver(lyrics.ResolverOptions{
		Providers: lyricsProviders,
		Logger:    logger,
	})

	manager, err := jobs.NewManager(jobs.ManagerOptions{
		Store:               jobsRepo,
		Catalog:             catalogRepo,
		Files:               filesRepo,
		Library:             library,
		Staging:             stagingManager,
		Registry:            registry,
		Discography:         discographyService,
		Matcher:             engine,
		Downloader:          audioDownloader,
		Tagger:              tagger,
		Artwork:             metadata.NewArtworkFetcher(httpx.New(cfg.Providers.HTTPTimeout)),
		Lyrics:              lyricsResolver,
		Cooldown:            jobs.NewMediaCooldownManager(),
		Broker:              broker,
		Logger:              logger,
		Concurrency:         cfg.Downloads.Concurrent,
		MaxRetries:          cfg.Downloads.MaxRetries,
		RetryBackoff:        cfg.Downloads.RetryBackoff,
		TrackTimeout:        cfg.Downloads.TrackTimeout,
		DurationToleranceMS: cfg.Matching.DurationToleranceMS,
		TempDir:             cfg.Downloads.TempDir,
		EmbedCover:          cfg.Library.EmbedCover,
		WriteCoverFile:      cfg.Library.WriteCoverFile,
		SkipExisting:        cfg.Downloads.SkipExisting,
		LyricsEnabled:       cfg.Library.LyricsEnabled,
		LyricsWriteSidecar:  cfg.Library.LyricsWriteSidecar,
		AllowOfflineStaging: cfg.Downloads.AllowOfflineStaging,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	settingsService, err := settings.New(settingsRepo, manager, engine, cfg)
	if err != nil {
		db.Close()
		return nil, err
	}
	settingsService.SetGeniusController(geniusLyricsProvider)
	if err := settingsService.Load(ctx); err != nil {
		db.Close()
		return nil, err
	}

	if err := manager.Start(ctx); err != nil {
		db.Close()
		return nil, err
	}

	subscriptionService, err := subscriptions.New(subscriptions.Options{
		Store:               subscriptionsRepo,
		Catalog:             catalogRepo,
		Files:               filesRepo,
		Discography:         discographyService,
		Registry:            registry,
		Downloader:          subscriptions.NewJobQueue(manager),
		Broker:              broker,
		Logger:              logger,
		SyncInterval:        cfg.Subscriptions.SyncInterval,
		RetryInterval:       cfg.Subscriptions.RetryInterval,
		SyncTimeout:         cfg.Subscriptions.SyncTimeout,
		DurationToleranceMS: cfg.Matching.DurationToleranceMS,
	})
	if err != nil {
		manager.Stop()
		db.Close()
		return nil, err
	}
	if err := subscriptionService.Start(ctx); err != nil {
		manager.Stop()
		db.Close()
		return nil, err
	}

	// The scheduler is started only when the periodic sync is switched on. The
	// endpoints and the manual check stay available either way, which is what
	// makes switching it off a decision about background work rather than
	// about the feature.
	var scheduler *subscriptions.Scheduler
	if cfg.Subscriptions.Enabled {
		scheduler, err = subscriptions.NewScheduler(subscriptions.SchedulerOptions{
			Service:   subscriptionService,
			Interval:  cfg.Subscriptions.CheckInterval,
			BatchSize: cfg.Subscriptions.BatchSize,
			Logger:    logger,
		})
		if err != nil {
			subscriptionService.Stop()
			manager.Stop()
			db.Close()
			return nil, err
		}
		if err := scheduler.Start(ctx); err != nil {
			subscriptionService.Stop()
			manager.Stop()
			db.Close()
			return nil, err
		}
	} else {
		logger.Info("the subscription scheduler is disabled; subscriptions are only synced on request")
	}

	libraryService, err := libsvc.NewService(libsvc.ServiceOptions{
		Lifecycle:   ctx,
		Library:     library,
		Catalog:     catalogRepo,
		Files:       filesRepo,
		Jobs:        manager,
		Lyrics:      lyricsResolver,
		Prober:      prober,
		Tagger:      tagger,
		Broker:      broker,
		Audit:       auditRepo,
		Providers:   registry,
		Logger:      logger,
		Concurrency: 4,
	})
	if err != nil {
		stopScheduler(scheduler)
		subscriptionService.Stop()
		manager.Stop()
		db.Close()
		return nil, err
	}

	updateService := update.NewService(update.Config{
		Enabled:       cfg.Update.Enabled,
		Repository:    cfg.Update.Repository,
		CheckInterval: cfg.Update.CheckInterval,
	}, version, nil, logger)

	handlerSet, err := handlers.New(handlers.Deps{
		Discography:    discographyService,
		Registry:       registry,
		Jobs:           manager,
		Subscriptions:  subscriptionService,
		Catalog:        catalogRepo,
		Files:          filesRepo,
		Settings:       settingsService,
		Library:        library,
		LibraryService: libraryService,
		Resolver:       resolve.NewService(ytdlpClient),
		Auth:           authService,
		Database:       db,
		Updates:        updateService,
		Tools: map[string]handlers.Checker{
			"yt-dlp":  ytdlpClient,
			"ffmpeg":  ffmpegRunner,
			"ffprobe": prober,
		},
		Version:      version,
		StartedAt:    time.Now(),
		CookieSecure: cfg.Server.CookieSecure,
		Logger:       logger,
	})

	if err != nil {
		authLimiter.Close()
		stopScheduler(scheduler)
		subscriptionService.Stop()
		manager.Stop()
		db.Close()
		return nil, err
	}

	router, err := api.NewRouter(api.RouterOptions{
		Handlers:        handlerSet,
		Auth:            authService,
		Logger:          logger,
		MaxRequestBytes: cfg.Server.MaxRequestBytes,
		RequestTimeout:  cfg.Server.ReadTimeout,
		CookieSecure:    cfg.Server.CookieSecure,
	})
	if err != nil {
		authLimiter.Close()
		stopScheduler(scheduler)
		subscriptionService.Stop()
		manager.Stop()
		db.Close()
		return nil, err
	}

	return &application{
		cfg:            cfg,
		logger:         logger,
		db:             db,
		manager:        manager,
		broker:         broker,
		router:         router,
		authLimiter:    authLimiter,
		subscriptions:  subscriptionService,
		scheduler:      scheduler,
		libraryService: libraryService,
	}, nil
}

// stopScheduler stops a scheduler that may not have been created.
func stopScheduler(scheduler *subscriptions.Scheduler) {
	if scheduler != nil {
		scheduler.Stop()
	}
}

// buildProviders registers the configured metadata and media providers.
func buildProviders(cfg config.Config, client *ytdlp.Client, logger *slog.Logger) (*provider.Registry, error) {
	registry := provider.NewRegistry()
	httpClient := httpx.New(cfg.Providers.HTTPTimeout)

	if cfg.Providers.Deezer.Enabled {
		deezerProvider := deezer.New(deezer.Config{
			APIBaseURL:        cfg.Providers.Deezer.APIBaseURL,
			HTTPClient:        httpClient,
			RequestsPerSecond: cfg.Providers.Deezer.RequestsPerSecond,
			Burst:             cfg.Providers.Deezer.Burst,
			MaxRetries:        cfg.Providers.Deezer.MaxRetries,
			RetryBackoff:      cfg.Providers.Deezer.RetryBackoff,
			MaxRetryBackoff:   cfg.Providers.Deezer.MaxRetryBackoff,
		})
		registry.RegisterMetadata(deezerProvider)
		logger.Info("provider registered",
			logging.KeyProvider, deezerProvider.Name(), "kind", "metadata",
			"requests_per_second", cfg.Providers.Deezer.RequestsPerSecond,
			"burst", cfg.Providers.Deezer.Burst,
			"max_retries", cfg.Providers.Deezer.MaxRetries)
	}

	if cfg.Providers.Spotify.Enabled {
		spotifyProvider, err := spotify.New(spotify.Config{
			ClientID:     cfg.Providers.Spotify.ClientID,
			ClientSecret: cfg.Providers.Spotify.ClientSecret,
			Market:       cfg.Providers.Spotify.Market,
			APIBaseURL:   cfg.Providers.Spotify.APIBaseURL,
			AuthURL:      cfg.Providers.Spotify.AuthURL,
			HTTPClient:   httpClient,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMetadata(spotifyProvider)
		logger.Info("provider registered", logging.KeyProvider, spotifyProvider.Name(), "kind", "metadata")
	} else {
		logger.Warn("Spotify is disabled: no client credentials are configured",
			logging.KeyProvider, "spotify")
	}

	if cfg.Providers.YTMusic.Enabled {
		metadataProvider := ytmusic.NewMetadataProvider(ytmusic.Config{
			BaseURL:    cfg.Providers.YTMusic.BaseURL,
			HTTPClient: httpClient,
		})
		registry.RegisterMetadata(metadataProvider)
		logger.Info("provider registered", logging.KeyProvider, metadataProvider.Name(), "kind", "metadata")

		mediaProvider, err := ytmusic.NewMediaProvider(ytmusic.MediaConfig{
			Client:            client,
			Limit:             cfg.Matching.CandidateLimit,
			RequestsPerSecond: cfg.Providers.YTMusic.RequestsPerSecond,
			Burst:             cfg.Providers.YTMusic.Burst,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMedia(mediaProvider)
		logger.Info("provider registered", logging.KeyProvider, mediaProvider.Name(), "kind", "media")
	}

	if cfg.Providers.YouTube.Enabled {
		mediaProvider, err := youtube.New(youtube.Config{
			Name:              youtube.ProviderName,
			Mode:              youtube.SearchVideos,
			Client:            client,
			Limit:             cfg.Matching.CandidateLimit,
			RequestsPerSecond: cfg.Providers.YouTube.RequestsPerSecond,
			Burst:             cfg.Providers.YouTube.Burst,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMedia(mediaProvider)
		logger.Info("provider registered", logging.KeyProvider, mediaProvider.Name(), "kind", "media")
	}

	registry.SetDefaults(cfg.Providers.DefaultMetadata, cfg.Providers.DefaultMedia)

	if registry.DefaultMetadataName() == "" {
		return nil, fmt.Errorf("no metadata provider is configured; enable Deezer, Spotify or YouTube Music")
	}
	if registry.DefaultMediaName() == "" {
		return nil, fmt.Errorf("no media provider is configured; enable YouTube Music or YouTube")
	}
	return registry, nil
}

// serve runs the HTTP server until the context is cancelled.
func (a *application) serve(ctx context.Context) error {
	server := &http.Server{
		Addr:         a.cfg.Server.Address,
		Handler:      a.router,
		ReadTimeout:  a.cfg.Server.ReadTimeout,
		WriteTimeout: a.cfg.Server.WriteTimeout,
		IdleTimeout:  a.cfg.Server.IdleTimeout,
		// The header timeout stays short even when the body may take longer.
		ReadHeaderTimeout: 15 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		a.logger.Info("server listening",
			"address", a.cfg.Server.Address,
			"concurrent_downloads", a.manager.Concurrency())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		a.logger.Info("shutdown requested")
		a.manager.BeginShutdown()
		// No further synchronisation may begin; the one in flight is drained
		// below together with the download workers.
		a.subscriptions.BeginShutdown()
		// Event streams are intentionally long lived and http.Server.Shutdown
		// does not cancel them. Closing the broker releases them before draining
		// the remaining finite requests.
		a.broker.Close()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()
	workersDone := make(chan struct{})
	go func() {
		// The scheduler goes first so that nothing new is picked up while the
		// service drains what is already running.
		stopScheduler(a.scheduler)
		a.subscriptions.Stop()
		a.manager.Stop()
		if a.libraryService != nil {
			a.libraryService.Stop()
		}
		close(workersDone)
	}()

	if err := server.Shutdown(shutdownCtx); err != nil {
		a.logger.Error("the HTTP server did not shut down cleanly", logging.KeyError, err.Error())
		if closeErr := server.Close(); closeErr != nil {
			a.logger.Error("the HTTP connections could not be forced closed", logging.KeyError, closeErr.Error())
		}
	}
	<-workersDone
	return <-errs
}

// close releases everything the application holds. Running downloads are
// terminated through the job contexts.
func (a *application) close() {
	a.logger.Info("stopping workers")
	stopScheduler(a.scheduler)
	if a.subscriptions != nil {
		a.subscriptions.Stop()
	}
	a.manager.Stop()
	if a.libraryService != nil {
		a.libraryService.Stop()
	}
	a.broker.Close()
	if a.authLimiter != nil {
		a.authLimiter.Close()
	}
	if err := a.db.Close(); err != nil {
		a.logger.Error("the database could not be closed", logging.KeyError, err.Error())
	}
	a.logger.Info("stopped")
}

// ffmpegLocation returns the directory yt-dlp should look for ffmpeg in, but
// only when an explicit path was configured.
func ffmpegLocation(path string) string {
	if path == "" || path == "ffmpeg" || !filepath.IsAbs(path) {
		return ""
	}
	return filepath.Dir(path)
}

// durationVerifyTolerance widens the matching tolerance for the check that
// runs after a download. Platforms often include a second or two of silence,
// which must not invalidate an otherwise correct file.
func durationVerifyTolerance(matchToleranceMS int) int {
	const minimum = 15000
	if matchToleranceMS*3 > minimum {
		return matchToleranceMS * 3
	}
	return minimum
}
