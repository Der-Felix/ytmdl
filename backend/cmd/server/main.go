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
	"sync"
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
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/playlist"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/deezer"
	"ytdm/backend/internal/provider/genius"
	"ytdm/backend/internal/provider/soundcloud"
	"ytdm/backend/internal/provider/spotify"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/provider/ytmusic"
	"ytdm/backend/internal/resolve"
	"ytdm/backend/internal/settings"
	"ytdm/backend/internal/storage"
	"ytdm/backend/internal/subscriptions"
	"ytdm/backend/internal/throughput"
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
	sessionPool    *mediasession.SessionPool

	// shutdownDeadline is the single absolute budget for one controlled
	// shutdown. It is stamped once - by serve when the signal arrives, or by
	// close when the server never got that far - and is then shared by the HTTP
	// drain, the worker stop and the final health flush. Both stampers run on
	// the same goroutine (serve, then the deferred close), so no lock is needed.
	shutdownDeadline time.Time

	// stopWorkersOnce guards the producer and worker stop sequence so that serve
	// and close start it at most once between them; workersStopped is closed
	// when that sequence has finished.
	stopWorkersOnce sync.Once
	workersStopped  chan struct{}
}

const (
	// fallbackShutdownTimeout bounds the controlled shutdown when the
	// configuration carries no positive value.
	fallbackShutdownTimeout = 10 * time.Second
	// maxShutdownPhaseReserve caps each of the two tail phases the shutdown
	// budget holds back: the wait for worker quiescence and the final media
	// session health flush behind it.
	maxShutdownPhaseReserve = 5 * time.Second
)

// shutdownTimeout is the total budget for one controlled shutdown.
func (a *application) shutdownTimeout() time.Duration {
	if a.cfg.Server.ShutdownTimeout > 0 {
		return a.cfg.Server.ShutdownTimeout
	}
	return fallbackShutdownTimeout
}

// phaseReserve is the slice of the shutdown budget kept back for one tail phase,
// so a slow HTTP drain can never consume all of it and leave either the workers'
// final database writes or the health snapshots behind them unfinished.
func (a *application) phaseReserve() time.Duration {
	reserve := a.shutdownTimeout() / 4
	if reserve > maxShutdownPhaseReserve {
		reserve = maxShutdownPhaseReserve
	}
	return reserve
}

// beginShutdownBudget stamps the shared absolute deadline on first use and
// returns it unchanged afterwards, so serve and close can never hand out two
// budgets for the same shutdown.
func (a *application) beginShutdownBudget() time.Time {
	if a.shutdownDeadline.IsZero() {
		a.shutdownDeadline = time.Now().Add(a.shutdownTimeout())
	}
	return a.shutdownDeadline
}

// drainDeadline is the point by which the HTTP server and the bulk of the worker
// drain must be done. The two reserves behind it belong to the wait for worker
// quiescence and to the final health flush, which is why the drain is cut short
// rather than allowed to run on.
func (a *application) drainDeadline() time.Time {
	return a.beginShutdownBudget().Add(-2 * a.phaseReserve())
}

// workersDeadline is the point by which the workers must be quiescent. Only
// past it can a health flush be the final one: jobs.Manager.Stop has returned,
// so every worker goroutine is gone, the interrupted-job requeue has written,
// and nothing is left that could record a session outcome and queue another
// health snapshot behind the flush.
func (a *application) workersDeadline() time.Time {
	return a.beginShutdownBudget().Add(-a.phaseReserve())
}

// stopWorkers runs the producer and worker stop sequence exactly once and
// returns the channel closed when it has finished. serve and close both wait on
// it under the shared budget, so the sequence never runs twice and is never
// waited on without a bound.
func (a *application) stopWorkers() <-chan struct{} {
	a.stopWorkersOnce.Do(func() {
		a.workersStopped = make(chan struct{})
		go func() {
			defer close(a.workersStopped)
			// The scheduler goes first so that nothing new is picked up while
			// the service drains what is already running.
			stopScheduler(a.scheduler)
			if a.subscriptions != nil {
				a.subscriptions.Stop()
			}
			if a.manager != nil {
				a.manager.Stop()
			}
			if a.libraryService != nil {
				a.libraryService.Stop()
			}
		}()
	})
	return a.workersStopped
}

// awaitWorkers starts the stop sequence if it is not running yet and waits for
// it until deadline, reporting whether the workers came down in time. Every wait
// is bounded by a point inside the shared budget, never by a timeout of its own.
func (a *application) awaitWorkers(deadline time.Time) bool {
	workersDone := a.stopWorkers()
	select {
	case <-workersDone:
		return true
	default:
	}

	waitCtx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	select {
	case <-workersDone:
		return true
	case <-waitCtx.Done():
		return false
	}
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
	// One recorder collects the counters of the hourly throughput summary:
	// provider process starts and reused answers, protection responses,
	// cooldown time and item outcomes.
	throughputRecorder := throughput.New()
	ytdlpClient := ytdlp.New(ytdlp.Options{
		Binary:         cfg.Tools.YTDLPPath,
		CookieFile:     cfg.Tools.CookieFile,
		PlayerClients:  cfg.Tools.PlayerClients,
		Timeout:        cfg.Tools.Timeout,
		FFmpegLocation: ffmpegLocation(cfg.Tools.FFmpegPath),
		Logger:         logger,
		Recorder:       throughputRecorder,
		Label:          string(provider.FamilyYouTube),
	})

	mediaSessionsRepo := repository.NewMediaSessions(db)
	legacyAdapter := mediasession.NewLegacyAdapter(cfg.Tools.CookieFile)
	cookieStorage, err := mediasession.NewCookieStorage(cfg.MediaSessions.CookieDir, legacyAdapter)
	if err != nil {
		logger.Warn("cookie storage initialization warning", "error", err.Error())
	}

	sessionPoolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   cfg.MediaSessions.MaxLeasesPerSession,
		SessionRequestsPerSec: cfg.MediaSessions.SessionRequestsPerSecond,
		SessionBurst:          cfg.MediaSessions.SessionBurst,
		GlobalRequestsPerSec:  cfg.MediaSessions.GlobalRequestsPerSecond,
		GlobalBurst:           cfg.MediaSessions.GlobalBurst,
		AllowUnknown:          true,
	}
	sessionPool := mediasession.NewSessionPool(sessionPoolCfg, cookieStorage, mediaSessionsRepo, legacyAdapter)
	if sessions, err := mediaSessionsRepo.ListSessions(ctx, mediasession.Filter{}); err == nil {
		sessionPool.ReloadSessions(sessions)
	} else {
		sessionPool.ReloadSessions(nil)
	}
	sessionPool.SetRecorder(throughputRecorder)
	// The base client can carry only the legacy cookie file. Managed-session
	// clones replace this gate when the orchestrator binds their cookie path.
	// It is installed before the media providers derive their paced copies of
	// the client, so every copy inherits it.
	ytdlpClient.SetExecutionGate(sessionPool.ExecutionGate(mediasession.LegacySessionID))

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

	sessionProber := mediasession.NewYTDLPProber(ytdlpClient, "")
	sessionProber.SetExecutionGateResolver(sessionPool.ExecutionGate)
	mediaSessionService := mediasession.NewService(mediasession.ServiceOptions{
		Repo:          mediaSessionsRepo,
		Storage:       cookieStorage,
		Pool:          sessionPool,
		LegacyAdapter: legacyAdapter,
		Prober:        sessionProber,
		Logger:        logger,
	})

	cooldownMgr := jobs.NewMediaCooldownManager()
	cooldownMgr.SetRecorder(throughputRecorder)

	providerOrchestrator := orchestrator.New(orchestrator.Options{
		Registry:    registry,
		SessionPool: sessionPool,
		Matcher:     engine,
		Cooldown:    cooldownMgr,
		Logger:      logger,
	})

	audioDownloader, err := downloader.New(downloader.Options{
		YTDLP:                 ytdlpClient,
		FFmpeg:                ffmpegRunner,
		Prober:                prober,
		AllowTranscode:        cfg.Downloads.AllowTranscode,
		CombinedAudioFallback: cfg.Downloads.CombinedAudioFallback,
		CombinedMaxBytes:      cfg.Downloads.CombinedFallbackMaxBytes,
		CombinedTimeout:       cfg.Downloads.CombinedFallbackTimeout,
		DurationToleranceMS:   durationVerifyTolerance(cfg.Matching.DurationToleranceMS),
		Retries:               cfg.Downloads.MaxRetries,
		CookieResolver:        providerOrchestrator.ResolveCookiePath,
		ExecutionGateResolver: sessionPool.ExecutionGate,
		Recorder:              throughputRecorder,
		Logger:                logger,
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
		if metadataProvider, err := registry.Metadata(ytmusic.ProviderName); err == nil {
			if ytMetadata, ok := metadataProvider.(*ytmusic.MetadataProvider); ok {
				lyricsProviders = append(lyricsProviders, ytmusic.NewLyricsProviderFromMetadata(ytMetadata))
			}
		}
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
		Cooldown:            cooldownMgr,
		Orchestrator:        providerOrchestrator,
		Broker:              broker,
		Logger:              logger,
		Throughput:          throughputRecorder,
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

	// Recovery notifications fired from pool callbacks run database work on the
	// goroutine that observed the recovery - a download worker, for instance.
	// Binding them to the application context lets shutdown cancel that work
	// instead of racing db.Close() or holding a worker against a stalled database.
	sessionPool.SetLifecycleContext(ctx)
	mediaSessionService.SetRecoveryHandler(func(ctx context.Context) {
		if _, err := manager.WakeSessionWaiters(ctx); err != nil {
			logger.Warn("failed to wake session waiters after session recovery", logging.KeyError, err.Error())
		}
	})

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
	// The update channel is a runtime setting: the UI changes it, ytmdlctl
	// reads the same row. A missing row keeps the stable default.
	if err := updateService.UseSettings(ctx, settingsRepo); err != nil {
		logger.Warn("stored update channel could not be loaded; using the stable channel", "error", err.Error())
	}

	playlistsRepo := repository.NewPlaylists(db)
	playlistService, err := playlist.New(playlist.Options{
		Store:  playlistsRepo,
		Logger: logger,
	})
	if err != nil {
		authLimiter.Close()
		stopScheduler(scheduler)
		subscriptionService.Stop()
		manager.Stop()
		db.Close()
		return nil, err
	}

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
		MediaSessions:  mediaSessionService,
		Playlists:      playlistService,
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
		sessionPool:    sessionPool,
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
			BaseURL:           cfg.Providers.YTMusic.BaseURL,
			HTTPClient:        httpClient,
			RequestsPerSecond: cfg.Providers.YTMusic.RequestsPerSecond,
			Burst:             cfg.Providers.YTMusic.Burst,
		})
		registry.RegisterMetadata(metadataProvider)
		logger.Info("provider registered", logging.KeyProvider, metadataProvider.Name(), "kind", "metadata")

		mediaProvider, err := ytmusic.NewMediaProvider(ytmusic.MediaConfig{
			Client:                client,
			Limit:                 cfg.Matching.CandidateLimit,
			RequestsPerSecond:     cfg.Providers.YTMusic.RequestsPerSecond,
			Burst:                 cfg.Providers.YTMusic.Burst,
			CombinedAudioFallback: cfg.Downloads.CombinedAudioFallback,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMedia(mediaProvider)
		logger.Info("provider registered", logging.KeyProvider, mediaProvider.Name(), "kind", "media")
	}

	if cfg.Providers.YouTube.Enabled {
		mediaProvider, err := youtube.New(youtube.Config{
			Name:                  youtube.ProviderName,
			Mode:                  youtube.SearchVideos,
			Client:                client,
			Limit:                 cfg.Matching.CandidateLimit,
			RequestsPerSecond:     cfg.Providers.YouTube.RequestsPerSecond,
			Burst:                 cfg.Providers.YouTube.Burst,
			CombinedAudioFallback: cfg.Downloads.CombinedAudioFallback,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMedia(mediaProvider)
		logger.Info("provider registered", logging.KeyProvider, mediaProvider.Name(), "kind", "media")
	}

	if cfg.Providers.SoundCloud.Enabled {
		// SoundCloud never uses YouTube credentials or a YouTube session's
		// execution gate; it is paced by its own limiter.
		soundCloudClient := client.WithCookieFile("").WithExecutionGate(nil).WithLabel(string(provider.FamilySoundCloud))
		soundCloudProvider, err := soundcloud.New(soundcloud.Config{
			Client:            soundCloudClient,
			Limit:             cfg.Matching.CandidateLimit,
			RequestsPerSecond: cfg.Providers.SoundCloud.RequestsPerSecond,
			Burst:             cfg.Providers.SoundCloud.Burst,
		})
		if err != nil {
			return nil, err
		}
		registry.RegisterMedia(soundCloudProvider)
		logger.Info("provider registered", logging.KeyProvider, soundCloudProvider.Name(), "kind", "media",
			"requests_per_second", cfg.Providers.SoundCloud.RequestsPerSecond,
			"burst", cfg.Providers.SoundCloud.Burst)
	}

	registry.SetDefaults(cfg.Providers.DefaultMetadata, cfg.Providers.DefaultMedia)

	if registry.DefaultMetadataName() == "" {
		return nil, fmt.Errorf("no metadata provider is configured; enable Deezer, Spotify or YouTube Music")
	}
	if registry.DefaultMediaName() == "" {
		return nil, fmt.Errorf("no media provider is configured; enable YouTube Music, YouTube or SoundCloud")
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
		// One budget from here on: HTTP drain, worker stop and the health flush
		// in close all share this deadline.
		a.beginShutdownBudget()
		a.manager.BeginShutdown()
		// No further synchronisation may begin; the one in flight is drained
		// below together with the download workers.
		a.subscriptions.BeginShutdown()
		// Event streams are intentionally long lived and http.Server.Shutdown
		// does not cancel them. Closing the broker releases them before draining
		// the remaining finite requests.
		a.broker.Close()
	}

	drainCtx, cancel := context.WithDeadline(context.Background(), a.drainDeadline())
	defer cancel()
	// Start the stop sequence while the HTTP server drains.
	a.stopWorkers()

	if err := server.Shutdown(drainCtx); err != nil {
		a.logger.Error("the HTTP server did not shut down cleanly", logging.KeyError, err.Error())
		if closeErr := server.Close(); closeErr != nil {
			a.logger.Error("the HTTP connections could not be forced closed", logging.KeyError, closeErr.Error())
		}
	}
	// Waiting for the workers must not eat the tail reserves: the drain deadline
	// cuts the wait short so close still gets to wait the workers out and to
	// persist the health snapshots behind them.
	if !a.awaitWorkers(a.drainDeadline()) {
		a.logger.Warn("the workers did not stop within the drain budget; continuing with the remaining budget")
	}
	return <-errs
}

// close releases everything the application holds. Running downloads are
// terminated through the job contexts. It walks the phases of the shared
// shutdown budget in the only order that makes the last flush a final one: wait
// the producers and workers out, seal and flush the health snapshots behind
// them, and only then let the database go away.
func (a *application) close() {
	a.logger.Info("stopping workers")
	// Reuses the deadline serve already stamped; only a shutdown that never
	// reached serve gets a budget stamped here.
	deadline := a.beginShutdownBudget()

	// Phase one: producers and workers down, on the first tail reserve.
	// jobs.Manager.Stop returns only once every worker goroutine is gone and the
	// interrupted-job requeue has written, so past this point nothing can record
	// a session outcome and queue a health snapshot behind the flush below.
	workersDown := a.awaitWorkers(a.workersDeadline())
	if !workersDown {
		a.logger.Warn("the workers did not stop within the shutdown budget; the health flush cannot be the final one and the database stays open")
	} else {
		// Nothing legitimate produces health snapshots any more. Sealing makes
		// that structural, so even a request handler that outlived the HTTP
		// drain cannot queue a write that would still run during the teardown.
		a.sessionPool.SealHealthPersist()
	}

	// Phase two: the final health flush, on the last reserve and behind the
	// producers rather than in front of them.
	quiescent := a.flushSessionHealth(deadline)

	if a.broker != nil {
		a.broker.Close()
	}
	if a.authLimiter != nil {
		a.authLimiter.Close()
	}

	// The pool may only go away once nothing can still be writing through it:
	// the workers are down, their final writes are done, and health persistence
	// has come to rest.
	safe := workersDown && quiescent
	if !safe {
		a.logger.Warn("database writes were still in flight at the shutdown deadline; leaving the pool to process exit rather than blocking in Close")
	}
	a.closeDatabase(safe)
	a.logger.Info("stopped")
}

// closeDatabase releases the pool, but only once nothing can still be writing
// through it. pgxpool.Close blocks until every connection is returned, so
// closing while a worker or the health drainer holds one would add exactly the
// unbounded tail behind the shutdown deadline that the shared budget exists to
// prevent. The process is exiting either way, so an unsafe pool is left to
// process teardown instead.
func (a *application) closeDatabase(safe bool) {
	if a.db == nil || !safe {
		return
	}
	if err := a.db.Close(); err != nil {
		a.logger.Error("the database could not be closed", logging.KeyError, err.Error())
	}
}

// flushSessionHealth drains any queued media session health writes to the
// database before the connection pool is closed. It runs on what is left of the
// shared shutdown budget up to deadline, never on a fresh timeout, and reports
// whether health persistence actually came to rest.
//
// Quiescence is what the pool reports, never what the context says. A flush over
// an already drained queue returns instantly even on an expired deadline, so an
// expired context is no evidence that a write is still in flight - reading it as
// such would leave the pool open on every shutdown that ran the budget close.
func (a *application) flushSessionHealth(deadline time.Time) bool {
	if a.sessionPool == nil {
		return true
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	if err := a.sessionPool.FlushHealthPersist(ctx); err != nil {
		a.logger.Error("failed to flush media session health persistence", logging.KeyError, err.Error())
	}
	return a.sessionPool.AwaitHealthPersistIdle(ctx)
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
