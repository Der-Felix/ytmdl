package webapp

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
)

const (
	indexHTML  = "<!doctype html><title>YTMDL</title><div id=root></div>"
	bundleName = "assets/index-Ab12Cd34.js"
)

// bundle is large enough to be worth compressing and recognisable in a body.
var bundle = strings.Repeat("console.log('ytmdl bundle');\n", 200)

// buildSite lays out a frontend build in a temporary directory: index.html,
// a hashed bundle with its gzip sibling, root files from public/, a dotfile
// and a subdirectory. A secret next to (not inside) the site checks that no
// request can leave it.
func buildSite(t *testing.T) (string, *Site) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "web")
	write := func(name string, data []byte) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", []byte(indexHTML))
	write(bundleName, []byte(bundle))
	write(bundleName+".gz", gzipBytes(t, []byte(bundle)))
	write("assets/index-Ef56.css", []byte("body{color:red}"))
	write("assets/font-1.woff2", []byte("wOF2"))
	write("favicon.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"))
	write("offline-sw.js", []byte("self.addEventListener('fetch',()=>{})"))
	write("offline-shell.json", []byte(`{"version":"1"}`))
	write(".env", []byte("SECRET=1"))
	write("assets/.hidden", []byte("hidden"))
	write("nested/readme.txt", []byte("nested"))
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte("outside-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	site, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { site.Close() })
	return dir, site
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serve(h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"X-Frame-Options":         "SAMEORIGIN",
		"Content-Security-Policy": "frame-ancestors 'self'",
		"X-Content-Type-Options":  "nosniff",
	}
	for key, value := range want {
		if got := rec.Header().Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestOpenRefusesWhatIsNotAFrontendBuild(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Error("an empty directory setting was accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing directory was accepted")
	}

	noIndex := t.TempDir()
	if err := os.Mkdir(filepath.Join(noIndex, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(noIndex); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Errorf("a directory without index.html: err = %v", err)
	}

	noAssets := t.TempDir()
	if err := os.WriteFile(filepath.Join(noAssets, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(noAssets); err == nil || !strings.Contains(err.Error(), "assets") {
		t.Errorf("a directory without assets: err = %v", err)
	}

	indexIsDir := t.TempDir()
	for _, name := range []string{"index.html", "assets"} {
		if err := os.Mkdir(filepath.Join(indexIsDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(indexIsDir); err == nil {
		t.Error("a directory named index.html was accepted as the application shell")
	}
}

func TestApplicationShellAndClientRoutes(t *testing.T) {
	_, site := buildSite(t)
	for _, target := range []string{"/", "/index.html", "/library/artists/42", "/settings?tab=system", "/nested", "/nested/", "/does-not-exist.js"} {
		t.Run(target, func(t *testing.T) {
			rec := serve(site, http.MethodGet, target, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if body := rec.Body.String(); body != indexHTML {
				t.Fatalf("body = %q, want index.html", body)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != DocumentCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, DocumentCacheControl)
			}
			if rec.Header().Get("ETag") == "" || rec.Header().Get("Last-Modified") == "" {
				t.Error("index.html lacks ETag or Last-Modified")
			}
			assertSecurityHeaders(t, rec)
		})
	}
}

func TestRootFilesAreServedAndRevalidated(t *testing.T) {
	_, site := buildSite(t)
	cases := map[string]string{
		"/favicon.svg":        "image/svg+xml",
		"/offline-sw.js":      "text/javascript; charset=utf-8",
		"/offline-shell.json": "application/json",
		"/nested/readme.txt":  "text/plain; charset=utf-8",
	}
	for target, ctype := range cases {
		t.Run(target, func(t *testing.T) {
			rec := serve(site, http.MethodGet, target, nil)
			if rec.Code != http.StatusOK || rec.Body.String() == indexHTML {
				t.Fatalf("status = %d, body = %q; want the file itself", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != ctype {
				t.Errorf("Content-Type = %q, want %q", got, ctype)
			}
			if got := rec.Header().Get("Cache-Control"); got != DocumentCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, DocumentCacheControl)
			}
			assertSecurityHeaders(t, rec)
		})
	}
}

func TestAssetsAreImmutableAndNeverFallBack(t *testing.T) {
	_, site := buildSite(t)

	rec := serve(site, http.MethodGet, "/"+bundleName, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != bundle {
		t.Fatalf("bundle: status = %d, body length %d", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Cache-Control"); got != AssetsCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, AssetsCacheControl)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	assertSecurityHeaders(t, rec)

	for target, ctype := range map[string]string{
		"/assets/index-Ef56.css": "text/css; charset=utf-8",
		"/assets/font-1.woff2":   "font/woff2",
	} {
		if rec := serve(site, http.MethodGet, target, nil); rec.Header().Get("Content-Type") != ctype {
			t.Errorf("%s: Content-Type = %q, want %q", target, rec.Header().Get("Content-Type"), ctype)
		}
	}

	for _, target := range []string{"/assets/missing-123.js", "/assets/", "/assets", "/assets/nested/x.js", "/" + bundleName + ".gz", "/assets/.hidden"} {
		t.Run(target, func(t *testing.T) {
			rec := serve(site, http.MethodGet, target, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "<title>YTMDL") {
				t.Fatal("a missing asset was answered with the application shell")
			}
			if rec.Header().Get("Cache-Control") == AssetsCacheControl {
				t.Error("a 404 must not be cached as immutable")
			}
			assertSecurityHeaders(t, rec)
		})
	}
}

func TestPrecompressedVariant(t *testing.T) {
	_, site := buildSite(t)
	target := "/" + bundleName

	plain := serve(site, http.MethodGet, target, nil)
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != bundle {
		t.Fatal("a client without Accept-Encoding did not get the plain file")
	}
	if !strings.Contains(plain.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("the plain variant lacks Vary: Accept-Encoding")
	}

	gz := serve(site, http.MethodGet, target, http.Header{"Accept-Encoding": {"br, gzip;q=0.8"}})
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", gz.Header().Get("Content-Encoding"))
	}
	if gz.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Errorf("gzip variant Content-Type = %q", gz.Header().Get("Content-Type"))
	}
	if !strings.Contains(gz.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("the gzip variant lacks Vary: Accept-Encoding")
	}
	if gz.Header().Get("Cache-Control") != AssetsCacheControl {
		t.Errorf("gzip variant Cache-Control = %q", gz.Header().Get("Cache-Control"))
	}
	zr, err := gzip.NewReader(gz.Body)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil || string(decoded) != bundle {
		t.Fatalf("decoded gzip body differs (err %v)", err)
	}
	if gz.Header().Get("ETag") == plain.Header().Get("ETag") {
		t.Error("the plain and the gzip variant share an ETag")
	}

	refused := serve(site, http.MethodGet, target, http.Header{"Accept-Encoding": {"gzip;q=0, *"}})
	if refused.Header().Get("Content-Encoding") != "" {
		t.Error("gzip;q=0 still got gzip")
	}

	// A file without a sibling is sent as is, without Vary.
	css := serve(site, http.MethodGet, "/assets/index-Ef56.css", http.Header{"Accept-Encoding": {"gzip"}})
	if css.Header().Get("Content-Encoding") != "" || css.Body.String() != "body{color:red}" {
		t.Error("a file without a gzip sibling was not sent as is")
	}
}

func TestConditionalRangeAndHead(t *testing.T) {
	_, site := buildSite(t)
	target := "/" + bundleName

	first := serve(site, http.MethodGet, target, nil)
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	notModified := serve(site, http.MethodGet, target, http.Header{"If-None-Match": {tag}})
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("If-None-Match: status = %d, body length %d", notModified.Code, notModified.Body.Len())
	}
	assertSecurityHeaders(t, notModified)

	since := serve(site, http.MethodGet, target, http.Header{"If-Modified-Since": {first.Header().Get("Last-Modified")}})
	if since.Code != http.StatusNotModified {
		t.Errorf("If-Modified-Since: status = %d, want 304", since.Code)
	}

	partial := serve(site, http.MethodGet, target, http.Header{"Range": {"bytes=0-6"}})
	if partial.Code != http.StatusPartialContent || partial.Body.String() != bundle[:7] {
		t.Fatalf("Range: status = %d, body %q", partial.Code, partial.Body.String())
	}

	head := serve(site, http.MethodHead, target, nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Cache-Control") != AssetsCacheControl {
		t.Fatalf("HEAD: status = %d, body length %d", head.Code, head.Body.Len())
	}
}

func TestOnlyReadMethods(t *testing.T) {
	_, site := buildSite(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := serve(site, method, "/", nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: Allow = %q", method, rec.Header().Get("Allow"))
		}
		assertSecurityHeaders(t, rec)
	}
}

func TestNoDotfilesNoTraversal(t *testing.T) {
	dir, site := buildSite(t)
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "outside.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Logf("symlink not created (%v); the escape check runs without it", err)
	}
	for _, target := range []string{"/.env", "/assets/.hidden", "/.git/config", "/nested/.secret"} {
		rec := serve(site, http.MethodGet, target, nil)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "SECRET") {
			t.Errorf("%s: status = %d, body %q; want a plain 404", target, rec.Code, rec.Body.String())
		}
	}
	for _, target := range []string{"/../outside.txt", "/assets/../../outside.txt", "/link.txt", `/..\outside.txt`, "/nested/../../outside.txt"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = target
		rec := httptest.NewRecorder()
		site.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "outside-secret") {
			t.Errorf("%s: a file outside the web directory was served", target)
		}
	}
}

// apiStub stands in for the API router: one route, the router's own JSON 404
// and method answers, an event stream and a byte-range audio response.
func apiStub() http.Handler {
	router := chi.NewRouter()
	router.NotFound(response.NotFound)
	router.MethodNotAllowed(response.MethodNotAllowed)
	router.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok", "padding": strings.Repeat("x", 4096)}})
	})
	router.Get("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		for i := 0; i < 2; i++ {
			io.WriteString(w, "event: ping\ndata: {}\n\n")
			if err := controller.Flush(); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	})
	router.Get("/api/v1/library/tracks/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		http.ServeContent(w, r, "track.opus", time.Unix(1700000000, 0), strings.NewReader(strings.Repeat("A", 8192)))
	})
	router.Get("/api/v1/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		response.JSON(w, http.StatusOK, map[string]string{"data": "late"})
	})
	return router
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	_, site := buildSite(t)
	return Handler(apiStub(), site, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAPIKeepsItsOwnAnswers(t *testing.T) {
	h := newHandler(t)

	health := serve(h, http.MethodGet, "/api/v1/health", nil)
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health: status = %d, body %q", health.Code, health.Body.String())
	}
	assertSecurityHeaders(t, health)
	if health.Header().Get("Cache-Control") == DocumentCacheControl {
		t.Error("the static cache policy leaked into the API")
	}

	// The API answers an unknown route itself, with its JSON error envelope
	// (status 400, code INVALID_REQUEST, see response.NotFound) - never with
	// the application shell.
	for _, target := range []string{"/api/x", "/api/v1/unknown", "/api/", "/api", "/assets/../api/v1/nope"} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.Path = target
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want the API's 400", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("Content-Type = %q, want the API's JSON", rec.Header().Get("Content-Type"))
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Error.Code != "INVALID_REQUEST" {
				t.Fatalf("body %q is not the API's JSON error", rec.Body.String())
			}
			assertSecurityHeaders(t, rec)
		})
	}

	// A POST to an API path is the API's business, not the static 405.
	post := serve(h, http.MethodPost, "/api/v1/health", nil)
	if post.Code != http.StatusMethodNotAllowed || !strings.HasPrefix(post.Header().Get("Content-Type"), "application/json") {
		t.Errorf("POST /api/v1/health: status = %d, Content-Type %q", post.Code, post.Header().Get("Content-Type"))
	}

	// Everything else is the web application.
	if rec := serve(h, http.MethodGet, "/apiary", nil); rec.Body.String() != indexHTML {
		t.Error("/apiary is a client route, not the API")
	}
	if rec := serve(h, http.MethodGet, "/"+bundleName, nil); rec.Header().Get("Cache-Control") != AssetsCacheControl {
		t.Error("assets are not served through the combined handler")
	}
}

func TestAPICompression(t *testing.T) {
	h := newHandler(t)

	gz := serve(h, http.MethodGet, "/api/v1/health", http.Header{"Accept-Encoding": {"gzip"}})
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON was not compressed: Content-Encoding = %q", gz.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(gz.Body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, _ := io.ReadAll(zr); !strings.Contains(string(decoded), `"status":"ok"`) {
		t.Fatal("compressed JSON does not decode to the response")
	}

	plain := serve(h, http.MethodGet, "/api/v1/health", nil)
	if plain.Header().Get("Content-Encoding") != "" {
		t.Error("a client without Accept-Encoding got a compressed response")
	}

	// Audio is never compressed, and a range request is passed through
	// untouched so that Content-Range keeps describing the bytes sent.
	audio := serve(h, http.MethodGet, "/api/v1/library/tracks/1/stream", http.Header{"Accept-Encoding": {"gzip"}, "Range": {"bytes=100-199"}})
	if audio.Code != http.StatusPartialContent || audio.Body.Len() != 100 || audio.Header().Get("Content-Encoding") != "" {
		t.Fatalf("audio range: status = %d, length %d, encoding %q", audio.Code, audio.Body.Len(), audio.Header().Get("Content-Encoding"))
	}
	full := serve(h, http.MethodGet, "/api/v1/library/tracks/1/stream", http.Header{"Accept-Encoding": {"gzip"}})
	if full.Header().Get("Content-Encoding") != "" || full.Body.Len() != 8192 {
		t.Fatalf("audio: encoding %q, length %d", full.Header().Get("Content-Encoding"), full.Body.Len())
	}
}

// TestEventStreamThroughRealServer checks over a real connection that an
// event reaches the client while the handler is still running, with a client
// that accepts gzip, as browsers do.
func TestEventStreamThroughRealServer(t *testing.T) {
	server := httptest.NewServer(newHandler(t))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	transport := &http.Transport{DisableCompression: true}
	defer transport.CloseIdleConnections()
	started := time.Now()
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("the event stream was compressed: %q", resp.Header.Get("Content-Encoding"))
	}
	if resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Error("the event stream lacks the security headers")
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "event: ping\n" {
		t.Fatalf("first line %q, err %v", line, err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("the first event arrived after %s; it was buffered", elapsed)
	}
}

// TestStaticWriteDeadlineDoesNotOutliveTheResponse checks that the deadline
// set for a static response is cleared again: the next request on the same
// keep-alive connection, which may be a long stream, must not inherit it.
func TestStaticWriteDeadlineDoesNotOutliveTheResponse(t *testing.T) {
	previous := staticWriteTimeout
	staticWriteTimeout = 150 * time.Millisecond
	t.Cleanup(func() { staticWriteTimeout = previous })

	server := httptest.NewServer(newHandler(t))
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	get := func(target string) *http.Response {
		t.Helper()
		if _, err := io.WriteString(conn, "GET "+target+" HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			t.Fatalf("%s: body: %v", target, err)
		}
		resp.Body.Close()
		return resp
	}

	if resp := get("/"); resp.StatusCode != http.StatusOK {
		t.Fatalf("static: status %d", resp.StatusCode)
	}
	// Answers after 400ms, well past the static deadline.
	if resp := get("/api/v1/slow"); resp.StatusCode != http.StatusOK {
		t.Fatalf("slow API on the same connection: status %d", resp.StatusCode)
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                      false,
		"gzip":                  true,
		"GZIP":                  true,
		"deflate, gzip;q=0.5":   true,
		"gzip;q=0":              false,
		"gzip; q=0.0":           false,
		"*":                     true,
		"*;q=0":                 false,
		"gzip;q=0, *":           false,
		"br":                    false,
		"identity":              false,
		"x-gzip":                true,
		"gzip;q=bogus":          false,
		"br;q=1.0, gzip;q=0.01": true,
	}
	for header, want := range cases {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}
