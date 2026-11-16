package api

import (
	"net/http"
	"regexp"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/mediasession"
)

// Both registered media-session route aliases accept cookie uploads.
var cookieUploadPath = regexp.MustCompile(`^/api/v1/(admin/)?media-sessions/[^/]+/cookies$`)

// Artwork uploads carry an image of up to 8 MiB plus multipart framing; the
// handler enforces the exact size itself.
var artworkUploadPath = regexp.MustCompile(`^/api/v1/library/(artists|releases)/[^/]+/artwork$`)

// maxArtworkUploadBodySize matches the limit the artwork handler reads with.
const maxArtworkUploadBodySize = 9 << 20

// Cookie and artwork uploads need room for the file and multipart framing
// before the handler runs. Other endpoints retain the configured request-body
// limit. Without the exception the general limit would refuse an upload the
// handler is prepared to accept.
func requestBodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		regular := middleware.BodyLimit(maxBytes)(next)
		upload := middleware.BodyLimit(mediasession.MaxCookieUploadBodySize)(next)
		artwork := middleware.BodyLimit(max(maxBytes, maxArtworkUploadBodySize))(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && cookieUploadPath.MatchString(r.URL.Path) {
				upload.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodPut && artworkUploadPath.MatchString(r.URL.Path) {
				artwork.ServeHTTP(w, r)
				return
			}
			regular.ServeHTTP(w, r)
		})
	}
}
