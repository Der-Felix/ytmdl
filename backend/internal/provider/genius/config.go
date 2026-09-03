package genius

import (
	"log/slog"
	"net/http"
	"time"
)

// Config configures the Genius lyrics fallback provider.
type Config struct {
	Enabled     bool
	AccessToken string
	BaseURL     string // defaults to "https://api.genius.com"
	HTTPClient  *http.Client
	Timeout     time.Duration
	Logger      *slog.Logger
}
