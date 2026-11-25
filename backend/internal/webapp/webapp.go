// Package webapp serves the built web application from the backend's own
// listener. It exists for the single-image deployment, which has no nginx in
// front of the backend, and reproduces what frontend/nginx.conf does for the
// two-image deployment:
//
//   - /assets/* are content-hashed bundles: cached for a year as immutable,
//     and a missing one is a plain 404, never the application shell;
//   - every other path that is not /api is the file it names, or index.html
//     when there is none (client-side routing), and none of these may be
//     cached without revalidation;
//   - every response, the API's included, carries the same three security
//     headers nginx adds.
//
// Files are served through http.ServeContent, which provides Last-Modified,
// conditional requests and byte ranges. Text files may have a gzip sibling
// (name.gz) prepared when the image is built; it is sent to clients that
// accept gzip. The directory is opened as an os.Root, so no request can reach
// a file outside it, and dotfiles and directory listings are never served.
package webapp

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"ytdm/backend/internal/api/middleware"
)

const (
	assetsPrefix = "/assets/"
	indexFile    = "index.html"

	// AssetsCacheControl is sent with every file below /assets/. Their names
	// carry a content hash, so a name never refers to different content.
	AssetsCacheControl = "public, max-age=31536000, immutable"
	// DocumentCacheControl is sent with every other file. index.html is the
	// only file whose name stays the same across releases and it names the
	// hashed bundles; a cached copy would keep an old release running.
	DocumentCacheControl = "no-cache, must-revalidate"

	// apiCompressionLevel is the gzip level for compressible API responses.
	apiCompressionLevel = 5
)

// staticWriteTimeout bounds how long one static response may take to be
// written. The server itself has no write timeout, because the event stream
// and audio streams are long lived; a static file is not, and a client that
// stops reading must not hold the connection forever. A variable only so that
// tests can shorten it.
var staticWriteTimeout = 2 * time.Minute

// compressibleTypes are the media types nginx compresses (gzip_types plus
// text/html, which nginx always compresses). The event stream, audio and
// images other than SVG are deliberately absent.
var compressibleTypes = []string{
	"text/html",
	"text/plain",
	"text/css",
	"text/xml",
	"text/javascript",
	"application/javascript",
	"application/x-javascript",
	"application/xml+rss",
	"application/json",
	"image/svg+xml",
}

// contentTypes covers the extensions of the built application that Go's
// built-in table does not know or that must not depend on the host's
// /etc/mime.types. Anything else falls back to mime.TypeByExtension and then
// to application/octet-stream, as nginx's default_type does; the type is never
// sniffed.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "text/xml; charset=utf-8",
	".wasm":        "application/wasm",
}

// SetSecurityHeaders adds the headers nginx sends with every response: no
// framing by other sites, and no content-type guessing.
func SetSecurityHeaders(h http.Header) {
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Security-Policy", "frame-ancestors 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
}

// Site serves one directory holding the built web application.
type Site struct {
	root *os.Root
}

// Open validates dir and opens it for serving. dir must contain index.html
// and an assets directory, which is what a frontend build produces; anything
// else is almost certainly a wrong path and is refused at startup rather than
// answered with 404s later.
func Open(dir string) (*Site, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("web app directory is empty")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open web app directory %s: %w", dir, err)
	}
	site := &Site{root: root}
	if f, info, ok := site.open(indexFile); ok {
		f.Close()
		if info.Size() == 0 {
			root.Close()
			return nil, fmt.Errorf("web app directory %s: %s is empty", dir, indexFile)
		}
	} else {
		root.Close()
		return nil, fmt.Errorf("web app directory %s has no %s; point MUSICDL_WEB_DIR at a frontend build", dir, indexFile)
	}
	info, err := root.Stat(strings.Trim(assetsPrefix, "/"))
	if err != nil || !info.IsDir() {
		root.Close()
		return nil, fmt.Errorf("web app directory %s has no assets directory; point MUSICDL_WEB_DIR at a frontend build", dir)
	}
	return site, nil
}

// Close releases the directory.
func (s *Site) Close() error { return s.root.Close() }

// open returns name if it is a regular file inside the root. Directories,
// symlinks that leave the root and anything that cannot be read count as
// missing.
func (s *Site) open(name string) (*os.File, fs.FileInfo, bool) {
	f, err := s.root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, false
	}
	return f, info, true
}

// ServeHTTP serves the web application. It never answers for /api; Handler
// routes those requests to the API before they get here.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	SetSecurityHeaders(w.Header())
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name, ok := cleanPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// A response that a client stops reading must not hold the connection.
	// The deadline is cleared again afterwards: without a server write
	// timeout, net/http would otherwise carry it over to the next request on
	// the same connection, which may be the event stream.
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(time.Now().Add(staticWriteTimeout)) == nil {
		defer controller.SetWriteDeadline(time.Time{})
	}

	// The assets directory itself is part of the asset namespace too: it
	// answers 404, not with a listing and not with the application shell.
	if name == strings.Trim(assetsPrefix, "/") || strings.HasPrefix("/"+name, assetsPrefix) {
		if !s.serveFile(w, r, name, AssetsCacheControl) {
			http.NotFound(w, r)
		}
		return
	}
	if name != "" && s.serveFile(w, r, name, DocumentCacheControl) {
		return
	}
	// Client-side route, the root, a directory or a file that does not
	// exist: the application shell decides.
	if !s.serveFile(w, r, indexFile, DocumentCacheControl) {
		http.NotFound(w, r)
	}
}

// cleanPath turns a request path into a name relative to the root. It
// refuses anything that is not plainly a public file name: dotfiles and dot
// directories (which also covers every ".." that survives cleaning),
// backslashes, NUL bytes and the precompressed .gz siblings, which are an
// encoding of a file rather than a file of their own.
func cleanPath(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	cleaned := path.Clean(raw)
	if strings.ContainsAny(cleaned, "\\\x00") {
		return "", false
	}
	name := strings.TrimPrefix(cleaned, "/")
	if name == "" {
		return "", true
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	if strings.HasSuffix(name, ".gz") {
		return "", false
	}
	return name, true
}

// serveFile writes name with the given cache policy and reports whether the
// file existed. A gzip sibling is preferred when the client accepts it.
func (s *Site) serveFile(w http.ResponseWriter, r *http.Request, name, cacheControl string) bool {
	f, info, ok := s.open(name)
	if !ok {
		return false
	}
	defer f.Close()

	h := w.Header()
	ctype := contentType(name)
	body, size, encoding := f, info.Size(), ""
	if isCompressible(ctype) {
		if gz, gzInfo, ok := s.open(name + ".gz"); ok {
			defer gz.Close()
			// The response depends on Accept-Encoding whichever variant is
			// sent, so caches must keep them apart.
			h.Add("Vary", "Accept-Encoding")
			if acceptsGzip(r.Header.Get("Accept-Encoding")) {
				body, size, encoding = gz, gzInfo.Size(), "gzip"
			}
		}
	}

	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", cacheControl)
	if encoding != "" {
		h.Set("Content-Encoding", encoding)
	}
	h.Set("ETag", etag(info.ModTime(), size, encoding))
	http.ServeContent(w, r, name, info.ModTime(), body)
	return true
}

// etag names one representation of a file: its modification time and size,
// like nginx, plus the content coding, so that the plain and the gzip variant
// never share a validator.
func etag(modified time.Time, size int64, encoding string) string {
	tag := strconv.FormatInt(modified.Unix(), 16) + "-" + strconv.FormatInt(size, 16)
	if encoding != "" {
		tag += "-" + encoding
	}
	return `"` + tag + `"`
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ctype, ok := contentTypes[ext]; ok {
		return ctype
	}
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		return ctype
	}
	return "application/octet-stream"
}

func isCompressible(ctype string) bool {
	base, _, _ := strings.Cut(ctype, ";")
	base = strings.TrimSpace(base)
	for _, candidate := range compressibleTypes {
		if base == candidate {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether an Accept-Encoding header admits gzip, honouring
// an explicit q=0 for gzip and the * wildcard.
func acceptsGzip(header string) bool {
	gzipQ, wildcardQ := -1.0, -1.0
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		token = strings.ToLower(strings.TrimSpace(token))
		q := 1.0
		for _, param := range strings.Split(params, ";") {
			key, value, found := strings.Cut(strings.TrimSpace(param), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					q = parsed
				} else {
					q = 0
				}
			}
		}
		switch token {
		case "gzip", "x-gzip":
			gzipQ = max(gzipQ, q)
		case "*":
			wildcardQ = max(wildcardQ, q)
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	return wildcardQ > 0
}

// Handler puts the API and the web application on one listener. Requests for
// /api and below go to the API, everything else to the site. The API keeps
// answering unknown paths with its own JSON 404, never with index.html.
//
// Both sides get the security headers nginx adds. API responses of the types
// nginx compresses are gzip encoded for clients that accept it; range
// requests (audio) are passed through untouched, as is the event stream,
// whose type is not compressible.
func Handler(api http.Handler, site http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	compressed := chimiddleware.Compress(apiCompressionLevel, compressibleTypes...)(api)
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetSecurityHeaders(w.Header())
		if r.Header.Get("Range") != "" {
			api.ServeHTTP(w, r)
			return
		}
		compressed.ServeHTTP(w, r)
	})
	// The API router logs and recovers on its own; static requests get the
	// same request id, access log line and panic recovery.
	static := middleware.RequestID(middleware.Recoverer(logger)(middleware.Logger(logger)(site)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || isAPIPath(path.Clean("/"+r.URL.Path)) {
			apiHandler.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}

func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}
