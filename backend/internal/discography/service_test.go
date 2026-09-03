package discography

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type mockMetadataProvider struct {
	name          string
	searchArtists func(ctx context.Context, query string) ([]music.Artist, error)
	getArtist     func(ctx context.Context, id string) (*music.Artist, error)
	getDisco      func(ctx context.Context, artistID string) ([]music.Release, error)
	getRelease    func(ctx context.Context, releaseID string) (*music.Release, error)
	getTracks     func(ctx context.Context, releaseID string) ([]music.Track, error)
	getTrack      func(ctx context.Context, id string) (*music.Track, error)
}

func (m *mockMetadataProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockMetadataProvider) SearchArtists(ctx context.Context, query string) ([]music.Artist, error) {
	if m.searchArtists != nil {
		return m.searchArtists(ctx, query)
	}
	return nil, nil
}

func (m *mockMetadataProvider) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	if m.getArtist != nil {
		return m.getArtist(ctx, id)
	}
	return &music.Artist{ID: id, Name: "Artist " + id, Provider: m.Name()}, nil
}

func (m *mockMetadataProvider) GetDiscography(ctx context.Context, artistID string) ([]music.Release, error) {
	if m.getDisco != nil {
		return m.getDisco(ctx, artistID)
	}
	return nil, nil
}

func (m *mockMetadataProvider) GetRelease(ctx context.Context, releaseID string) (*music.Release, error) {
	if m.getRelease != nil {
		return m.getRelease(ctx, releaseID)
	}
	return &music.Release{ID: releaseID, Title: "Album " + releaseID, Provider: m.Name(), SourceID: releaseID}, nil
}

func (m *mockMetadataProvider) GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error) {
	if m.getTracks != nil {
		return m.getTracks(ctx, releaseID)
	}
	return nil, nil
}

func (m *mockMetadataProvider) GetTrack(ctx context.Context, id string) (*music.Track, error) {
	if m.getTrack != nil {
		return m.getTrack(ctx, id)
	}
	return &music.Track{ID: id, Title: "Track " + id, SourceProvider: m.Name(), SourceID: id}, nil
}

type mockBasicMetadataProvider struct {
	name          string
	searchArtists func(ctx context.Context, query string) ([]music.Artist, error)
	getArtist     func(ctx context.Context, id string) (*music.Artist, error)
	getDisco      func(ctx context.Context, artistID string) ([]music.Release, error)
	getRelease    func(ctx context.Context, releaseID string) (*music.Release, error)
	getTracks     func(ctx context.Context, releaseID string) ([]music.Track, error)
}

func (m *mockBasicMetadataProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockBasicMetadataProvider) SearchArtists(ctx context.Context, query string) ([]music.Artist, error) {
	if m.searchArtists != nil {
		return m.searchArtists(ctx, query)
	}
	return nil, nil
}

func (m *mockBasicMetadataProvider) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	if m.getArtist != nil {
		return m.getArtist(ctx, id)
	}
	return &music.Artist{ID: id, Name: "Artist " + id, Provider: m.Name()}, nil
}

func (m *mockBasicMetadataProvider) GetDiscography(ctx context.Context, artistID string) ([]music.Release, error) {
	if m.getDisco != nil {
		return m.getDisco(ctx, artistID)
	}
	return nil, nil
}

func (m *mockBasicMetadataProvider) GetRelease(ctx context.Context, releaseID string) (*music.Release, error) {
	if m.getRelease != nil {
		return m.getRelease(ctx, releaseID)
	}
	return &music.Release{ID: releaseID, Title: "Album " + releaseID, Provider: m.Name(), SourceID: releaseID}, nil
}

func (m *mockBasicMetadataProvider) GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error) {
	if m.getTracks != nil {
		return m.getTracks(ctx, releaseID)
	}
	return nil, nil
}

var _ provider.MetadataProvider = (*mockBasicMetadataProvider)(nil)

func TestServiceResolveArtist(t *testing.T) {
	mockP := &mockMetadataProvider{
		name: "spotify",
		getArtist: func(ctx context.Context, id string) (*music.Artist, error) {
			return &music.Artist{ID: id, Name: "Test Artist", Provider: "spotify"}, nil
		},
		getDisco: func(ctx context.Context, artistID string) ([]music.Release, error) {
			return []music.Release{
				{ID: "r1", SourceID: "r1", Title: "Album 1", ReleaseType: music.ReleaseAlbum, Year: 2021, Provider: "spotify"},
				{ID: "r2", SourceID: "r2", Title: "Single 1", ReleaseType: music.ReleaseSingle, Year: 2022, Provider: "spotify"},
			}, nil
		},
		getTracks: func(ctx context.Context, releaseID string) ([]music.Track, error) {
			if releaseID == "r1" {
				return []music.Track{
					{ID: "t1", Title: "Song 1", DurationMS: 200000, Artists: []string{"Test Artist"}},
				}, nil
			}
			return []music.Track{
				{ID: "t2", Title: "Song 2", DurationMS: 180000, Artists: []string{"Test Artist"}},
			}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)

	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	var stages []Stage
	res, err := service.ResolveArtist(context.Background(), ArtistRequest{
		Provider: "spotify",
		ArtistID: "a1",
		Filter:   music.DefaultReleaseFilter(),
	}, func(stage Stage, current, total int) {
		stages = append(stages, stage)
	})

	if err != nil {
		t.Fatalf("ResolveArtist: %v", err)
	}

	if res.Artist.Name != "Test Artist" {
		t.Errorf("artist = %+v", res.Artist)
	}
	if len(res.Releases) != 2 {
		t.Errorf("releases = %d, want 2", len(res.Releases))
	}
	if res.TotalTracks != 2 || len(res.Groups) != 2 {
		t.Errorf("tracks = %d/%d, want 2/2", res.TotalTracks, len(res.Groups))
	}
	if len(stages) == 0 {
		t.Errorf("progress stages empty")
	}
}

func TestServiceResolveArtist_WarningsOnFailedRelease(t *testing.T) {
	mockP := &mockMetadataProvider{
		name: "spotify",
		getArtist: func(ctx context.Context, id string) (*music.Artist, error) {
			return &music.Artist{ID: id, Name: "Test Artist", Provider: "spotify"}, nil
		},
		getDisco: func(ctx context.Context, artistID string) ([]music.Release, error) {
			return []music.Release{
				{ID: "r1", SourceID: "r1", Title: "Good Album", ReleaseType: music.ReleaseAlbum, Provider: "spotify"},
				{ID: "r2", SourceID: "r2", Title: "Bad Album", ReleaseType: music.ReleaseAlbum, Provider: "spotify"},
			}, nil
		},
		getTracks: func(ctx context.Context, releaseID string) ([]music.Track, error) {
			if releaseID == "r2" {
				return nil, errors.New("network error")
			}
			return []music.Track{
				{ID: "t1", Title: "Song 1", DurationMS: 200000, Artists: []string{"Test Artist"}},
			}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)

	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	res, err := service.ResolveArtist(context.Background(), ArtistRequest{
		Provider: "spotify",
		ArtistID: "a1",
	}, nil)

	if err != nil {
		t.Fatalf("ResolveArtist: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Errorf("warnings = %d, want 1", len(res.Warnings))
	}
	if len(res.Groups) != 1 {
		t.Errorf("groups = %d, want 1", len(res.Groups))
	}
}

func TestServiceResolveTrack_DirectResolver(t *testing.T) {
	mockP := &mockMetadataProvider{
		name: "spotify",
		getTrack: func(ctx context.Context, id string) (*music.Track, error) {
			return &music.Track{ID: id, Title: "Direct Track", SourceProvider: "spotify"}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)

	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	tr, err := service.ResolveTrack(context.Background(), "spotify", "t1", "")
	if err != nil {
		t.Fatalf("ResolveTrack: %v", err)
	}
	if tr.Title != "Direct Track" {
		t.Errorf("track = %+v", tr)
	}
}

func TestServiceResolveTrack_FallbackToRelease(t *testing.T) {
	mockP := &mockBasicMetadataProvider{
		name: "ytmusic",
		getTracks: func(ctx context.Context, releaseID string) ([]music.Track, error) {
			return []music.Track{
				{ID: "t1", SourceID: "t1", Title: "Found Track"},
			}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)

	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	tr, err := service.ResolveTrack(context.Background(), "ytmusic", "t1", "r1")
	if err != nil {
		t.Fatalf("ResolveTrack: %v", err)
	}
	if tr.Title != "Found Track" {
		t.Errorf("track = %+v", tr)
	}

	if _, err := service.ResolveTrack(context.Background(), "ytmusic", "t1", ""); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Errorf("err code = %v, want INVALID_REQUEST", apperr.CodeOf(err))
	}
}

func TestServiceSearchArtists_Fallback(t *testing.T) {
	mockDeezer := &mockMetadataProvider{
		name: "deezer",
		searchArtists: func(ctx context.Context, query string) ([]music.Artist, error) {
			return nil, apperr.New(apperr.CodeProviderUnavailable, "Deezer network error")
		},
	}
	mockYTMusic := &mockMetadataProvider{
		name: "ytmusic",
		searchArtists: func(ctx context.Context, query string) ([]music.Artist, error) {
			return []music.Artist{
				{ID: "yt1", Name: "YT Artist", Provider: "ytmusic"},
			}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockDeezer)
	reg.RegisterMetadata(mockYTMusic)
	reg.SetDefaults("deezer", "ytmusic")

	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	artists, err := service.SearchArtists(context.Background(), "deezer", "Artist")
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if len(artists) != 1 || artists[0].Name != "YT Artist" {
		t.Errorf("expected fallback artists from ytmusic, got: %+v", artists)
	}
}

// A release that could not be read for a transient reason has to stay
// distinguishable from one that is permanently gone: the subscription sync
// schedules its next run differently for the two.
func TestResolveArtistCountsTransientWarnings(t *testing.T) {
	registry := provider.NewRegistry()
	registry.RegisterMetadata(&mockMetadataProvider{
		getDisco: func(context.Context, string) ([]music.Release, error) {
			return []music.Release{
				{Title: "Rate limited", SourceID: "limited", ReleaseType: music.ReleaseAlbum},
				{Title: "Unavailable", SourceID: "down", ReleaseType: music.ReleaseAlbum},
				{Title: "Gone for good", SourceID: "gone", ReleaseType: music.ReleaseAlbum},
				{Title: "Fine", SourceID: "fine", ReleaseType: music.ReleaseAlbum},
			}, nil
		},
		getTracks: func(_ context.Context, releaseID string) ([]music.Track, error) {
			switch releaseID {
			case "limited":
				return nil, apperr.New(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded")
			case "down":
				return nil, apperr.New(apperr.CodeProviderUnavailable, "Deezer is unreachable")
			case "gone":
				return nil, apperr.New(apperr.CodeReleaseNotFound, "Deezer does not know this item.")
			default:
				return []music.Track{{Title: "Track", Artists: []string{"Artist"}, DurationMS: 1000}}, nil
			}
		},
	})

	service, err := NewService(Options{Registry: registry, Logger: quietLogger()})
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	result, err := service.ResolveArtist(context.Background(),
		ArtistRequest{Provider: "mock", ArtistID: "1"}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(result.Warnings) != 3 {
		t.Fatalf("expected three warnings, got %v", result.Warnings)
	}
	// The rate limit and the outage are worth another attempt soon; the
	// missing release is not.
	if result.TransientWarnings != 2 {
		t.Fatalf("expected two transient warnings, got %d", result.TransientWarnings)
	}
}

func TestResolveArtistReportsNoTransientWarningsOnACleanRun(t *testing.T) {
	registry := provider.NewRegistry()
	registry.RegisterMetadata(&mockMetadataProvider{
		getDisco: func(context.Context, string) ([]music.Release, error) {
			return []music.Release{{Title: "Fine", SourceID: "fine", ReleaseType: music.ReleaseAlbum}}, nil
		},
		getTracks: func(context.Context, string) ([]music.Track, error) {
			return []music.Track{{Title: "Track", Artists: []string{"Artist"}, DurationMS: 1000}}, nil
		},
	})

	service, err := NewService(Options{Registry: registry, Logger: quietLogger()})
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	result, err := service.ResolveArtist(context.Background(),
		ArtistRequest{Provider: "mock", ArtistID: "1"}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(result.Warnings) != 0 || result.TransientWarnings != 0 {
		t.Fatalf("a clean run reported warnings: %+v", result)
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestResolveArtistFilesAFeatureUnderThePrimaryArtist is the end to end guard
// for the artist fragmentation: a provider that credits a collaboration must
// not produce an album artist of its own.
func TestResolveArtistFilesAFeatureUnderThePrimaryArtist(t *testing.T) {
	mockP := &mockMetadataProvider{
		name: "ytmusic",
		getArtist: func(context.Context, string) (*music.Artist, error) {
			return &music.Artist{ID: "a1", Name: "LACAZETTE", Provider: "ytmusic"}, nil
		},
		getDisco: func(context.Context, string) ([]music.Release, error) {
			return []music.Release{{
				ID: "r1", SourceID: "r1", Title: "CCN", ReleaseType: music.ReleaseSingle, Year: 2025,
				Provider: "ytmusic", Artists: []string{"LACAZETTE", "Bushido"}, AlbumArtist: "LACAZETTE",
			}}, nil
		},
		getTracks: func(context.Context, string) ([]music.Track, error) {
			return []music.Track{{
				ID: "t1", SourceID: "t1", Title: "CCN", DurationMS: 150000,
				Artists: []string{"LACAZETTE", "Bushido"},
			}}, nil
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)
	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	res, err := service.ResolveArtist(context.Background(), ArtistRequest{
		Provider: "ytmusic", ArtistID: "a1", Filter: music.DefaultReleaseFilter(),
	}, nil)
	if err != nil {
		t.Fatalf("ResolveArtist: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(res.Groups))
	}
	track := res.Groups[0].Track
	if track.AlbumArtist != "LACAZETTE" {
		t.Errorf("album artist = %q, want LACAZETTE", track.AlbumArtist)
	}
	if len(track.Artists) != 2 || track.Artists[1] != "Bushido" {
		t.Errorf("artists = %v, want both credits", track.Artists)
	}
	if track.TrackTotal != 1 || track.DiscTotal != 1 || track.DiscNumber != 1 {
		t.Errorf("totals = %d/%d disc %d, want 1/1 disc 1",
			track.TrackTotal, track.DiscTotal, track.DiscNumber)
	}
	if track.Album != "CCN" {
		t.Errorf("album = %q, want CCN", track.Album)
	}
}
