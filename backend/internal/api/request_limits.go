package api

import (
	"net/http"
	"regexp"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/mediasession"
)

// Both registered media-session route aliases accept cookie uploads.
var cookieUploadPath = regexp.MustCompile(`^/api/v1/(admin/)?media-sessions/[^/]+/cookies$`)

// Cookie uploads need room for the file and multipart framing before the
// handler runs. Other endpoints retain the configured request-body limit.
func requestBodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		regular := middleware.BodyLimit(maxBytes)(next)
		upload := middleware.BodyLimit(mediasession.MaxCookieUploadBodySize)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && cookieUploadPath.MatchString(r.URL.Path) {
				upload.ServeHTTP(w, r)
				return
			}
			regular.ServeHTTP(w, r)
		})
	}
}
