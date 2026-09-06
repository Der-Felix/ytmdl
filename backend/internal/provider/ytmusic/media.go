package ytmusic

import (
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/ytdlp"
)

// MediaConfig configures the YouTube Music media provider.
type MediaConfig struct {
	Client            *ytdlp.Client
	Limit             int
	RequestsPerSecond float64
	Burst             int
}

// NewMediaProvider builds the YouTube Music media provider.
//
// YouTube Music and YouTube are the same platform behind one extractor, so the
// yt-dlp based implementation in the youtube package is reused; only the search
// entry point differs. Results are marked as coming from a music catalogue,
// which the matcher uses to prefer them over general video uploads.
func NewMediaProvider(cfg MediaConfig) (*youtube.MediaProvider, error) {
	return youtube.New(youtube.Config{
		Name:              ProviderName,
		Mode:              youtube.SearchMusic,
		Client:            cfg.Client,
		Limit:             cfg.Limit,
		MusicService:      true,
		RequestsPerSecond: cfg.RequestsPerSecond,
		Burst:             cfg.Burst,
	})
}
