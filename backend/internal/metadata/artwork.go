package metadata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"

	// Registering the decoders lets image.DecodeConfig read the dimensions of
	// the covers the providers deliver.
	_ "image/jpeg"
	_ "image/png"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
)

// MaxArtworkBytes bounds how large a downloaded cover may be.
const MaxArtworkBytes = 8 << 20

// pictureTypeFrontCover is the FLAC picture type used for album covers.
const pictureTypeFrontCover = 3

// Artwork is a cover image together with everything needed to embed it.
type Artwork struct {
	Data   []byte
	MIME   string
	Width  int
	Height int
}

// ArtworkFetcher downloads cover images.
type ArtworkFetcher struct {
	client *http.Client
}

// NewArtworkFetcher builds a fetcher on top of an SSRF protected client.
func NewArtworkFetcher(client *http.Client) *ArtworkFetcher {
	if client == nil {
		client = httpx.New(httpx.DefaultTimeout)
	}
	return &ArtworkFetcher{client: client}
}

// Fetch downloads the cover at rawURL. An empty URL yields a nil result
// without an error, because a missing cover must not fail a download.
func (f *ArtworkFetcher) Fetch(ctx context.Context, rawURL string) (*Artwork, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, nil
	}
	parsed, err := httpx.ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The cover request could not be built.", err)
	}
	req.Header.Set("Accept", "image/jpeg,image/png;q=0.9,*/*;q=0.5")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The cover could not be downloaded.", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apperr.Newf(apperr.CodeProviderUnavailable,
			"The cover request answered with status %d.", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtworkBytes+1))
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The cover could not be read.", err)
	}
	if int64(len(data)) > MaxArtworkBytes {
		return nil, apperr.Newf(apperr.CodeUnsupportedMediaType,
			"The cover is larger than the allowed %d bytes.", MaxArtworkBytes)
	}
	return NewArtwork(data)
}

// NewArtwork inspects raw image data and returns the artwork description. Only
// JPEG and PNG are accepted, because those are the formats both the Vorbis
// comment picture block and MP4 covers are defined for.
func NewArtwork(data []byte) (*Artwork, error) {
	if len(data) == 0 {
		return nil, apperr.New(apperr.CodeUnsupportedMediaType, "The cover is empty.")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeUnsupportedMediaType, "The cover image could not be decoded.", err)
	}

	var mime string
	switch format {
	case "jpeg":
		mime = "image/jpeg"
	case "png":
		mime = "image/png"
	default:
		return nil, apperr.Newf(apperr.CodeUnsupportedMediaType,
			"Cover images of type %q are not supported.", format)
	}

	return &Artwork{Data: data, MIME: mime, Width: config.Width, Height: config.Height}, nil
}

// Extension returns the file extension matching the cover's type.
func (a *Artwork) Extension() string {
	if a.MIME == "image/png" {
		return ".png"
	}
	return ".jpg"
}

// MetadataBlockPicture renders the artwork as the base64 encoded
// METADATA_BLOCK_PICTURE value that Vorbis comments use for embedded covers.
func (a *Artwork) MetadataBlockPicture() string {
	description := ""

	buf := bytes.NewBuffer(make([]byte, 0, len(a.Data)+64))
	writeUint32BE(buf, pictureTypeFrontCover)
	writeUint32BE(buf, uint32(len(a.MIME)))
	buf.WriteString(a.MIME)
	writeUint32BE(buf, uint32(len(description)))
	buf.WriteString(description)
	writeUint32BE(buf, uint32(a.Width))
	writeUint32BE(buf, uint32(a.Height))
	writeUint32BE(buf, 24) // colour depth in bits per pixel
	writeUint32BE(buf, 0)  // number of colours in an indexed palette
	writeUint32BE(buf, uint32(len(a.Data)))
	buf.Write(a.Data)

	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// String renders a short description for logs.
func (a *Artwork) String() string {
	return fmt.Sprintf("%s %dx%d (%d bytes)", a.MIME, a.Width, a.Height, len(a.Data))
}

func writeUint32BE(buf *bytes.Buffer, value uint32) {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	buf.Write(raw[:])
}
