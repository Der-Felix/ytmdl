// Package ytmusic implements the YouTube Music providers: a metadata provider
// built on the InnerTube API that the web player itself uses, and a media
// provider that reuses the yt-dlp based implementation with a YouTube Music
// search.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ytdm/backend/internal/apperr"
)

// ProviderName is the stable identifier of the YouTube Music providers.
const ProviderName = "ytmusic"

// InnerTube client identification. These values are what the YouTube Music web
// player sends; the API rejects requests that do not identify a known client.
const (
	clientName       = "WEB_REMIX"
	clientVersion    = "1.20240103.01.00"
	userAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	apiFormatVersion = "1"
)

// Search filter parameters. They are the opaque values the web client sends
// for its own filter tabs.
const (
	filterArtists = "Eg-KAQwIABAAGAAgASgAMABqChAEEAUQAxAKEAk%3D"
	filterAlbums  = "Eg-KAQwIABAAGAEgACgAMABqChAEEAUQAxAKEAk%3D"
)

// maxResponseBytes bounds how much of an InnerTube answer is read.
const maxResponseBytes = 32 << 20

// innerTube performs InnerTube requests.
type innerTube struct {
	httpClient *http.Client
	baseURL    string
	language   string
	region     string
}

// request is the InnerTube request envelope.
type request struct {
	Context  requestContext `json:"context"`
	BrowseID string         `json:"browseId,omitempty"`
	VideoID  string         `json:"videoId,omitempty"`
	Query    string         `json:"query,omitempty"`
	Params   string         `json:"params,omitempty"`
}

type requestContext struct {
	Client clientContext `json:"client"`
}

type clientContext struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
	HL            string `json:"hl"`
	GL            string `json:"gl"`
}

func (t *innerTube) newRequest() request {
	return request{Context: requestContext{Client: clientContext{
		ClientName:    clientName,
		ClientVersion: clientVersion,
		HL:            t.language,
		GL:            t.region,
	}}}
}

// call posts a request to an InnerTube endpoint and returns the decoded body.
func (t *innerTube) call(ctx context.Context, endpoint string, payload request) (node, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return node{}, apperr.Wrap(apperr.CodeInternal, "The YouTube Music request could not be encoded.", err)
	}

	target := strings.TrimRight(t.baseURL, "/") + "/youtubei/v1/" + endpoint + "?prettyPrint=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return node{}, apperr.Wrap(apperr.CodeInternal, "The YouTube Music request could not be built.", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Goog-Api-Format-Version", apiFormatVersion)
	req.Header.Set("X-YouTube-Client-Name", "67")
	req.Header.Set("X-YouTube-Client-Version", clientVersion)
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("Referer", "https://music.youtube.com/")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return node{}, apperr.Wrap(apperr.CodeProviderUnavailable, "YouTube Music could not be reached.", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return node{}, apperr.Wrap(apperr.CodeProviderUnavailable, "The YouTube Music response could not be read.", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return node{}, apperr.New(apperr.CodeProviderRateLimited, "YouTube Music is rate limiting the backend.")
	case http.StatusNotFound, http.StatusBadRequest:
		return node{}, apperr.New(apperr.CodeProviderNotFound, "YouTube Music does not know this item.")
	default:
		return node{}, apperr.Newf(apperr.CodeProviderUnavailable,
			"YouTube Music answered with status %d.", resp.StatusCode)
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return node{}, apperr.Wrap(apperr.CodeProviderUnavailable, "The YouTube Music response could not be decoded.", err)
	}
	return wrap(decoded), nil
}

// search runs a search query with an optional filter parameter.
func (t *innerTube) search(ctx context.Context, query, params string) (node, error) {
	payload := t.newRequest()
	payload.Query = query
	payload.Params = params
	return t.call(ctx, "search", payload)
}

// browse loads a browse id, optionally with continuation parameters.
func (t *innerTube) browse(ctx context.Context, browseID, params string) (node, error) {
	payload := t.newRequest()
	payload.BrowseID = browseID
	payload.Params = params
	return t.call(ctx, "browse", payload)
}

// next loads the watch context of a video, which is where the tab that leads
// to the lyrics page lives.
func (t *innerTube) next(ctx context.Context, videoID string) (node, error) {
	payload := t.newRequest()
	payload.VideoID = videoID
	return t.call(ctx, "next", payload)
}
