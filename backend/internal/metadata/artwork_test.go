package metadata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeImage(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})

	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	default:
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

func TestNewArtworkDetectsTheRealFormat(t *testing.T) {
	cases := []struct {
		format   string
		wantMIME string
		wantExt  string
	}{
		{"jpeg", "image/jpeg", ".jpg"},
		{"png", "image/png", ".png"},
	}
	for _, c := range cases {
		artwork, err := NewArtwork(makeImage(t, c.format))
		if err != nil {
			t.Fatalf("NewArtwork(%s): %v", c.format, err)
		}
		if artwork.MIME != c.wantMIME {
			t.Errorf("%s: MIME = %q, want %q", c.format, artwork.MIME, c.wantMIME)
		}
		if artwork.Extension() != c.wantExt {
			t.Errorf("%s: Extension = %q, want %q", c.format, artwork.Extension(), c.wantExt)
		}
		if artwork.Width != 4 || artwork.Height != 3 {
			t.Errorf("%s: dimensions = %dx%d, want 4x3", c.format, artwork.Width, artwork.Height)
		}
	}
}

func TestNewArtworkRejectsInvalidImages(t *testing.T) {
	if _, err := NewArtwork([]byte("not an image at all")); err == nil {
		t.Error("garbage must be rejected")
	}
	if _, err := NewArtwork(nil); err == nil {
		t.Error("an empty cover must be rejected")
	}
}

// TestFetchIgnoresAWrongContentType is the cover-extension bug in its original
// form: a server that labels a PNG as a JPEG must not decide the file name.
func TestFetchIgnoresAWrongContentType(t *testing.T) {
	pngBytes := makeImage(t, "png")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()

	artwork, err := NewArtworkFetcher(server.Client()).Fetch(context.Background(), loopbackURL(server.URL)+"/cover.jpg")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if artwork.MIME != "image/png" || artwork.Extension() != ".png" {
		t.Fatalf("MIME = %q, extension = %q — the bytes, not the header, decide",
			artwork.MIME, artwork.Extension())
	}
}

func TestFetchWithoutAContentTypeStillWorks(t *testing.T) {
	jpegBytes := makeImage(t, "jpeg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write(jpegBytes)
	}))
	defer server.Close()

	artwork, err := NewArtworkFetcher(server.Client()).Fetch(context.Background(), loopbackURL(server.URL)+"/cover")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if artwork.Extension() != ".jpg" {
		t.Fatalf("extension = %q, want .jpg", artwork.Extension())
	}
}

func TestFetchRejectsANonImageBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html>not a cover</html>"))
	}))
	defer server.Close()

	if _, err := NewArtworkFetcher(server.Client()).Fetch(context.Background(), loopbackURL(server.URL)+"/cover.jpg"); err == nil {
		t.Fatal("a body that is not an image must be refused")
	}
}

func TestFetchWithoutAURLIsNotAnError(t *testing.T) {
	artwork, err := NewArtworkFetcher(nil).Fetch(context.Background(), "  ")
	if err != nil || artwork != nil {
		t.Fatalf("Fetch = %v, %v; a missing cover must not fail a download", artwork, err)
	}
}

func TestMetadataBlockPictureCarriesTheRealMIME(t *testing.T) {
	artwork, err := NewArtwork(makeImage(t, "png"))
	if err != nil {
		t.Fatal(err)
	}
	block := artwork.MetadataBlockPicture()
	if block == "" {
		t.Fatal("no picture block was rendered")
	}
	decoded := decodePictureBlock(t, block)
	if decoded != "image/png" {
		t.Fatalf("embedded MIME = %q, want image/png", decoded)
	}
}

// decodePictureBlock reads the MIME type out of a METADATA_BLOCK_PICTURE body.
func decodePictureBlock(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode picture block: %v", err)
	}
	if len(raw) < 8 {
		t.Fatal("picture block too short")
	}
	length := binary.BigEndian.Uint32(raw[4:8])
	if int(length)+8 > len(raw) {
		t.Fatal("picture block MIME length out of range")
	}
	return string(raw[8 : 8+length])
}

// loopbackURL rewrites a httptest address to use the "localhost" host name.
// The fetcher refuses a URL whose host is a non-public IP literal, which is
// exactly the SSRF guard we want to keep; a name is allowed through and the
// test server's own client dials it without the production dialer.
func loopbackURL(raw string) string {
	return strings.Replace(raw, "127.0.0.1", "localhost", 1)
}
