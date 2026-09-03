// Package spotify implements the Spotify metadata provider. Spotify is used
// for the catalogue only — artists, discography, releases and tracks. Audio is
// never fetched from here.
package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
)

// maxResponseBytes bounds how much of a provider answer is read.
const maxResponseBytes = 8 << 20

// tokenSkew is subtracted from the token lifetime so that a request never
// travels with an almost expired token.
const tokenSkew = 30 * time.Second

// client speaks the Spotify Web API using the client credentials flow.
type client struct {
	httpClient *http.Client
	apiBaseURL string
	authURL    string
	clientID   string
	secret     string
	market     string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// tokenResponse is the answer of the token endpoint.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// accessToken returns a valid bearer token, fetching a new one when needed.
func (c *client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	form := url.Values{"grant_type": []string{"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The Spotify token request could not be built.", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeProviderUnavailable, "Spotify could not be reached.", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify token response could not be read.", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", apperr.New(apperr.CodeProviderUnavailable,
			"The Spotify credentials were rejected. Check the client id and secret.")
	case http.StatusTooManyRequests:
		return "", rateLimitError(resp)
	default:
		return "", apperr.Newf(apperr.CodeProviderUnavailable,
			"The Spotify token endpoint answered with status %d.", resp.StatusCode)
	}

	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify token response could not be decoded.", err)
	}
	if token.AccessToken == "" {
		return "", apperr.New(apperr.CodeProviderUnavailable, "Spotify returned an empty access token.")
	}

	lifetime := time.Duration(token.ExpiresIn) * time.Second
	if lifetime <= tokenSkew {
		lifetime = 2 * tokenSkew
	}
	c.token = token.AccessToken
	c.tokenExpiry = time.Now().Add(lifetime - tokenSkew)
	return c.token, nil
}

// get performs an authenticated GET request and decodes the JSON answer into
// out. A rejected token is refreshed once and the request repeated.
func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	for attempt := range 2 {
		token, err := c.accessToken(ctx)
		if err != nil {
			return err
		}

		endpoint := c.apiBaseURL + path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "The Spotify request could not be built.", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return apperr.Wrap(apperr.CodeProviderUnavailable, "Spotify could not be reached.", err)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify response could not be read.", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify response could not be decoded.", err)
			}
			return nil
		case http.StatusUnauthorized:
			if attempt == 0 {
				c.invalidateToken()
				continue
			}
			return apperr.New(apperr.CodeProviderUnavailable, "Spotify rejected the access token.")
		case http.StatusNotFound:
			return apperr.New(apperr.CodeProviderNotFound, "Spotify does not know this item.")
		case http.StatusTooManyRequests:
			return rateLimitError(resp)
		default:
			return apperr.Newf(apperr.CodeProviderUnavailable,
				"Spotify answered with status %d: %s", resp.StatusCode, spotifyMessage(body))
		}
	}
	return apperr.New(apperr.CodeProviderUnavailable, "The Spotify request could not be authorised.")
}

func (c *client) invalidateToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.tokenExpiry = time.Time{}
}

// rateLimitError builds the rate limit error, including the wait time Spotify
// asked for.
func rateLimitError(resp *http.Response) error {
	retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		return apperr.Newf(apperr.CodeProviderRateLimited,
			"Spotify is rate limiting the backend. Retry in %d seconds.", seconds)
	}
	return apperr.New(apperr.CodeProviderRateLimited, "Spotify is rate limiting the backend.")
}

// spotifyMessage extracts the error message from an API error body.
func spotifyMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	const maxLen = 200
	text := strings.TrimSpace(string(body))
	if len(text) > maxLen {
		text = text[:maxLen] + "…"
	}
	if text == "" {
		return "no message"
	}
	return text
}

// paginate walks a paged Spotify collection and calls visit for every page.
// It stops when a page is not full or when the hard limit is reached.
func paginate(ctx context.Context, c *client, path string, query url.Values, pageSize, hardLimit int,
	visit func(raw json.RawMessage) (int, error)) error {

	if query == nil {
		query = url.Values{}
	}
	offset := 0
	for offset < hardLimit {
		page := url.Values{}
		for key, values := range query {
			page[key] = values
		}
		page.Set("limit", strconv.Itoa(pageSize))
		page.Set("offset", strconv.Itoa(offset))

		var envelope struct {
			Items json.RawMessage `json:"items"`
			Next  string          `json:"next"`
			Total int             `json:"total"`
		}
		if err := c.get(ctx, path, page, &envelope); err != nil {
			return err
		}

		count, err := visit(envelope.Items)
		if err != nil {
			return err
		}
		if count < pageSize || envelope.Next == "" {
			return nil
		}
		offset += count
	}
	return nil
}

// pathFor builds an API path from an id, refusing ids that would change the
// request target.
func pathFor(format, id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "The Spotify id must not be empty.")
	}
	for _, r := range trimmed {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAllowed {
			return "", apperr.Newf(apperr.CodeInvalidRequest, "%q is not a valid Spotify id.", id)
		}
	}
	return fmt.Sprintf(format, trimmed), nil
}
