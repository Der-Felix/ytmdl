package playlist

import (
	"context"
	"log/slog"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

// Options configures the playlist and favorites service.
type Options struct {
	Store  *repository.Playlists
	Logger *slog.Logger
}

// Service provides high-level business operations for user-owned playlists and track favorites.
type Service struct {
	store  *repository.Playlists
	logger *slog.Logger
}

// New creates a new playlist and favorites service.
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, apperr.New(apperr.CodeInternal, "The playlist service needs a repository store.")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:  opts.Store,
		logger: logger,
	}, nil
}

// CreatePlaylist creates a new user playlist with validation.
func (s *Service) CreatePlaylist(ctx context.Context, userID, name, description string) (repository.Playlist, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.Playlist{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.CreatePlaylist(ctx, userID, name, description)
}

// ListPlaylists lists all playlists owned by the authenticated user.
func (s *Service) ListPlaylists(ctx context.Context, userID string) ([]repository.Playlist, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.ListPlaylistsForUser(ctx, userID)
}

// GetPlaylist retrieves a user's playlist and its ordered tracks.
func (s *Service) GetPlaylist(ctx context.Context, userID, playlistID string) (repository.PlaylistDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.PlaylistDetail{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.GetPlaylistForUser(ctx, userID, playlistID)
}

// UpdatePlaylist updates metadata for a user's playlist.
func (s *Service) UpdatePlaylist(ctx context.Context, userID, playlistID, name, description string) (repository.Playlist, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.Playlist{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.UpdatePlaylist(ctx, userID, playlistID, name, description)
}

// DeletePlaylist deletes a user's playlist.
func (s *Service) DeletePlaylist(ctx context.Context, userID, playlistID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.DeletePlaylist(ctx, userID, playlistID)
}

// AddTrack adds a library track to the playlist.
func (s *Service) AddTrack(ctx context.Context, userID, playlistID, trackID string) (repository.PlaylistDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.PlaylistDetail{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.AddTrack(ctx, userID, playlistID, trackID)
}

// RemoveTrack removes a track from the playlist.
func (s *Service) RemoveTrack(ctx context.Context, userID, playlistID, trackID string) (repository.PlaylistDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.PlaylistDetail{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.RemoveTrack(ctx, userID, playlistID, trackID)
}

// ReorderTracks updates the track order atomically.
func (s *Service) ReorderTracks(ctx context.Context, userID, playlistID string, trackIDs []string) (repository.PlaylistDetail, error) {
	if strings.TrimSpace(userID) == "" {
		return repository.PlaylistDetail{}, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.ReorderTracks(ctx, userID, playlistID, trackIDs)
}

// FavoriteTrack favorites a track for the user.
func (s *Service) FavoriteTrack(ctx context.Context, userID, trackID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.FavoriteTrack(ctx, userID, trackID)
}

// UnfavoriteTrack unfavorites a track for the user.
func (s *Service) UnfavoriteTrack(ctx context.Context, userID, trackID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.UnfavoriteTrack(ctx, userID, trackID)
}

// IsFavorite checks whether a track is favorited by the user.
func (s *Service) IsFavorite(ctx context.Context, userID, trackID string) (bool, error) {
	if strings.TrimSpace(userID) == "" {
		return false, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.IsFavorite(ctx, userID, trackID)
}

// ListFavoriteTracks returns all favorite tracks for the user.
func (s *Service) ListFavoriteTracks(ctx context.Context, userID string) ([]music.LibraryTrack, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.ListFavoriteTracks(ctx, userID)
}

// ListFavoriteTrackIDs returns the IDs of all favorited tracks for the user.
func (s *Service) ListFavoriteTrackIDs(ctx context.Context, userID string) ([]string, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperr.New(apperr.CodeUnauthenticated, "Anmeldung erforderlich.")
	}
	return s.store.ListFavoriteTrackIDs(ctx, userID)
}
