// Package api wires the HTTP routes to the handlers.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"log/slog"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

// RouterOptions configures the router.
type RouterOptions struct {
	Handlers *handlers.Handlers
	Auth     *auth.Service
	Logger   *slog.Logger

	// MaxRequestBytes bounds the size of a request body.
	MaxRequestBytes int64
	// RequestTimeout bounds how long a normal request may run. The event
	// stream is exempt from it.
	RequestTimeout time.Duration
	// CookieSecure forces the Secure flag on cookies.
	CookieSecure bool
}

// eventsPath is excluded from the request timeout because the stream is long
// lived by design.
const eventsPath = "/events"

// NewRouter builds the HTTP router.
func NewRouter(opts RouterOptions) (http.Handler, error) {
	if opts.Handlers == nil {
		return nil, apperr.New(apperr.CodeInternal, "The router needs a handler set.")
	}
	if opts.Auth == nil {
		return nil, apperr.New(apperr.CodeInternal, "The router needs an auth service.")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxBytes := opts.MaxRequestBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer(logger))
	router.Use(middleware.Logger(logger))
	router.Use(middleware.BodyLimit(maxBytes))
	router.Use(middleware.Timeout(timeout, eventsPath))

	router.NotFound(response.NotFound)
	router.MethodNotAllowed(response.MethodNotAllowed)

	h := opts.Handlers

	router.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(middleware.Authenticate(opts.Auth))
		v1.Use(middleware.EnsureCSRF(opts.CookieSecure))

		// Public endpoints
		v1.Get("/health", h.Health)

		v1.Route("/auth", func(authRouter chi.Router) {
			authRouter.Get("/status", h.Status)

			// Pre-auth mutating routes (must supply valid CSRF token)
			authRouter.Group(func(preAuth chi.Router) {
				preAuth.Use(middleware.CSRF)
				preAuth.Post("/setup", h.Setup)
				preAuth.Post("/login", h.Login)
			})

			// Authenticated auth routes
			authRouter.Group(func(authed chi.Router) {
				authed.Use(middleware.RequireAuth)
				authed.Get("/me", h.Me)

				authed.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Post("/logout", h.Logout)
				})
			})
		})

		// Authenticated Application Endpoints
		v1.Group(func(authed chi.Router) {
			authed.Use(middleware.RequireAuth)

			authed.Route("/profile", func(profile chi.Router) {
				profile.Get("/", h.GetProfile)
				profile.Get("/sessions", h.ListSessions)

				profile.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Patch("/", h.UpdateProfile)
					mutating.Post("/password", h.ChangePassword)
					mutating.Delete("/sessions/{id}", h.RevokeSession)
					mutating.Post("/sessions/revoke-others", h.RevokeOtherSessions)
				})
			})

			authed.Get("/providers", h.Providers)
			authed.Get("/search/artists", h.SearchArtists)
			authed.Get("/resolve", h.Resolve)

			authed.Route("/artists", func(artists chi.Router) {
				artists.Get("/{id}", h.GetArtist)
				artists.Get("/{id}/discography", h.GetDiscography)
			})

			authed.Get("/releases/{id}", h.GetRelease)

			authed.Route("/downloads", func(downloads chi.Router) {
				downloads.Use(middleware.CSRF)
				downloads.Post("/artist", h.DownloadArtist)
				downloads.Post("/release", h.DownloadRelease)
				downloads.Post("/track", h.DownloadTrack)
			})

			authed.Route("/subscriptions", func(subs chi.Router) {
				subs.Get("/", h.ListSubscriptions)
				subs.Get("/export", h.ExportSubscriptions)
				subs.Get("/{id}", h.GetSubscription)

				subs.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Post("/", h.Subscribe)
					mutating.Post("/import/preview", h.PreviewImportSubscriptions)
					mutating.Post("/import/apply", h.ApplyImportSubscriptions)
					mutating.Patch("/{id}", h.UpdateSubscription)
					mutating.Delete("/{id}", h.DeleteSubscription)
					mutating.Post("/{id}/sync", h.SyncSubscription)
				})
			})

			authed.Route("/jobs", func(jobs chi.Router) {
				jobs.Get("/", h.ListJobs)
				jobs.Get("/{id}", h.GetJob)

				jobs.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Patch("/{id}", h.UpdateJob)
					mutating.Post("/{id}/pause", h.PauseJob)
					mutating.Post("/{id}/resume", h.ResumeJob)
					mutating.Post("/{id}/retry-failed", h.RetryFailedJob)
					mutating.Post("/{job_id}/items/{item_id}/retry", h.RetryJobItem)
					mutating.Delete("/{id}", h.CancelJob)

					mutating.Group(func(admin chi.Router) {
						admin.Use(middleware.RequireAdmin)
						admin.Delete("/history", h.DeleteJobHistory)
					})
				})
			})

			authed.Route("/library", func(library chi.Router) {
				library.Get("/stats", h.LibraryStats)
				library.Get("/search", h.LibrarySearch)
				library.Get("/artists", h.LibraryArtists)
				library.Get("/artists/{id}", h.LibraryArtistDetail)
				library.Get("/releases", h.LibraryReleases)
				library.Get("/releases/{id}", h.LibraryReleaseDetail)
				library.Get("/tracks", h.LibraryTracks)
				library.Get("/tracks/{id}", h.LibraryTrackDetail)
				library.Get("/tracks/{id}/stream", h.StreamTrack)
				library.Head("/tracks/{id}/stream", h.StreamTrack)
				library.Get("/files/{id}/stream", h.StreamFile)
				library.Head("/files/{id}/stream", h.StreamFile)
				library.Get("/tracks/{id}/lyrics", h.TrackLyrics)
				library.Get("/lyrics/backfill", h.BackfillLyricsStatus)
				library.Get("/lyrics/backfill/preview", h.PreviewBackfillLyrics)
				library.Get("/compatibility", h.CompatibilityReport)
				library.Get("/scan", h.GetLibraryScan)
				library.Get("/audits", h.ListLibraryAudits)
				library.Get("/audits/current", h.GetCurrentLibraryAudit)
				library.Get("/audits/{id}", h.GetLibraryAudit)
				library.Get("/audits/{id}/findings", h.ListLibraryAuditFindings)

				library.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Post("/tracks/{id}/redownload", h.RedownloadLibraryTrack)
					mutating.Post("/tracks/{id}/retag", h.RetagLibraryTrack)
					mutating.Post("/tracks/{id}/lyrics/refresh", h.RefreshTrackLyrics)
					mutating.Delete("/tracks/{id}/lyrics", h.DeleteTrackLyrics)
					mutating.Post("/lyrics/backfill", h.BackfillLyrics)
					mutating.Post("/reorganize", h.Reorganize)
					mutating.Post("/scan", h.StartLibraryScan)

					// Destructive library mutations & repairs require Administrator privileges
					mutating.Group(func(admin chi.Router) {
						admin.Use(middleware.RequireAdmin)
						admin.Post("/audits", h.StartLibraryAudit)
						admin.Post("/audits/{id}/cancel", h.CancelLibraryAudit)
						admin.Post("/repairs/preview", h.PreviewLibraryRepairs)
						admin.Post("/repairs/apply", h.ApplyLibraryRepairs)
						admin.Post("/releases/{id}/artwork/preview", h.PreviewReleaseArtwork)
						admin.Post("/releases/{id}/artwork/refresh", h.RefreshReleaseArtwork)
						admin.Post("/artwork/preview", h.PreviewBulkArtwork)
						admin.Post("/artwork/refresh", h.RefreshBulkArtwork)
						admin.Delete("/releases/{id}", h.DeleteLibraryRelease)
						admin.Delete("/tracks/{id}", h.DeleteLibraryTrack)
						admin.Delete("/scan/issues/{id}", h.DeleteLibraryOrphanIssue)
					})
				})
			})

			authed.Get("/events", h.Events)
			authed.Get("/settings", h.GetSettings)

			// Administrator-Only Endpoints
			authed.Group(func(admin chi.Router) {
				admin.Use(middleware.RequireAdmin)

				admin.Route("/users", func(users chi.Router) {
					users.Get("/", h.ListUsers)
					users.Get("/{id}", h.GetUser)

					users.Group(func(mutating chi.Router) {
						mutating.Use(middleware.CSRF)
						mutating.Post("/", h.CreateUser)
						mutating.Patch("/{id}", h.UpdateUser)
						mutating.Post("/{id}/reset-password", h.ResetPassword)
						mutating.Delete("/{id}", h.DeleteUser)
					})
				})

				admin.Route("/storage", func(storageRouter chi.Router) {
					storageRouter.Get("/status", h.StorageStatus)
					storageRouter.Group(func(mutating chi.Router) {
						mutating.Use(middleware.CSRF)
						mutating.Post("/probe", h.StorageProbe)
						mutating.Post("/queue/pause", h.StorageQueuePause)
						mutating.Post("/queue/resume", h.StorageQueueResume)
					})
				})

				admin.Group(func(mutating chi.Router) {
					mutating.Use(middleware.CSRF)
					mutating.Put("/settings", h.UpdateSettings)
				})
			})
		})
	})

	return router, nil
}
