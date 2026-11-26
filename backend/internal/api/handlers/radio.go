package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

// radioRequest reads the radio parameters shared by /library/radio and
// /library/radio/station: seed (a track id), artist, release, genre, nonce
// and exclude (track ids, comma separated or repeated).
func radioRequest(r *http.Request, user string) (repository.RadioRequest, error) {
	q := r.URL.Query()
	req := repository.RadioRequest{
		User:      user,
		TrackID:   q.Get("seed"),
		ArtistID:  q.Get("artist"),
		ReleaseID: q.Get("release"),
		Genre:     q.Get("genre"),
		Nonce:     q.Get("nonce"),
	}
	for _, raw := range q["exclude"] {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				req.Exclude = append(req.Exclude, id)
			}
		}
	}
	if len(req.Exclude) > repository.RadioMaxExclude {
		return req, apperr.New(apperr.CodeInvalidRequest, "Zu viele ausgeschlossene Titel (höchstens 500).")
	}
	return req, nil
}

// LocalRadio answers GET /library/radio with a list of tracks (unchanged
// shape). Besides seed, genre and nonce it accepts artist, release, limit
// (1–500, default 50) and exclude.
func (h *Handlers) LocalRadio(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context()).ID
	req, err := radioRequest(r, user)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	limit, _, err := pagination(r, repository.RadioDefaultLimit, repository.RadioMaxLimit)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	req.Limit = limit
	res, err := h.deps.Catalog.Radio(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	tracks, err := h.deps.Catalog.TracksByIDs(r.Context(), res.TrackIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, tracks)
}

// RadioStationPage is one batch of a radio station.
type RadioStationPage struct {
	Title string               `json:"title"`
	Seed  repository.RadioSeed `json:"seed"`
	// Tracks is this batch, in station order.
	Tracks []music.LibraryTrack `json:"tracks"`
	// NextCursor fetches the next batch; null once the station is used up.
	NextCursor *string `json:"next_cursor"`
	// Remaining is how many tracks the station still holds after this batch
	// (before exclusions made later).
	Remaining int `json:"remaining"`
}

const (
	radioStationTTL      = 6 * time.Hour
	radioStationMax      = 64
	radioStationDefault  = 25
	radioStationMaxBatch = 100
)

// radioStation is a computed station kept for its follow-up batches.
type radioStation struct {
	user    string
	title   string
	seed    repository.RadioSeed
	ids     []string
	expires time.Time
}

// radioStations keeps recent stations in memory, so the next batch of a
// station continues exactly where the last one ended, without repeats. After
// a restart or six hours the station is computed again from the request.
type radioStations struct {
	mu       sync.Mutex
	stations map[string]*radioStation
}

func (s *radioStations) get(token, user string) *radioStation {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stations[token]
	if st == nil || st.user != user || time.Now().After(st.expires) {
		return nil
	}
	return st
}

func (s *radioStations) put(st *radioStation) string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	token := hex.EncodeToString(buf[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stations == nil {
		s.stations = map[string]*radioStation{}
	}
	now := time.Now()
	for k, v := range s.stations {
		if now.After(v.expires) {
			delete(s.stations, k)
		}
	}
	for len(s.stations) >= radioStationMax {
		oldest := ""
		for k, v := range s.stations {
			if oldest == "" || v.expires.Before(s.stations[oldest].expires) {
				oldest = k
			}
		}
		delete(s.stations, oldest)
	}
	s.stations[token] = st
	return token
}

// parseRadioCursor splits "token.offset".
func parseRadioCursor(cursor string) (string, int, bool) {
	token, raw, ok := strings.Cut(cursor, ".")
	if !ok || token == "" || len(token) > 64 {
		return "", 0, false
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 || offset > repository.RadioMaxLimit {
		return "", 0, false
	}
	return token, offset, true
}

// RadioStation answers GET /library/radio/station: a titled station from a
// track (seed), artist, release or genre, served in batches of limit (1–100,
// default 25). next_cursor fetches the following batch without repeats.
func (h *Handlers) RadioStation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.UserFromContext(ctx).ID
	req, err := radioRequest(r, user)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	limit, _, err := pagination(r, radioStationDefault, radioStationMaxBatch)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var (
		st     *radioStation
		token  string
		offset int
	)
	if raw := strings.TrimSpace(queryString(r, "cursor")); raw != "" {
		var ok bool
		if token, offset, ok = parseRadioCursor(raw); !ok {
			response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Ungültiger Radio-Cursor."))
			return
		}
		st = h.radio.get(token, user)
	}
	if st == nil {
		full := req
		full.Exclude, full.Limit = nil, repository.RadioMaxLimit
		res, err := h.deps.Catalog.Radio(ctx, full)
		if err != nil {
			response.Error(w, r, err)
			return
		}
		st = &radioStation{user: user, title: res.Title, seed: res.Seed, ids: res.TrackIDs, expires: time.Now().Add(radioStationTTL)}
		token = h.radio.put(st)
	}

	excluded := map[string]bool{}
	for _, id := range req.Exclude {
		excluded[id] = true
	}
	offset = min(offset, len(st.ids))
	end := offset
	page := []string{}
	for end < len(st.ids) && len(page) < limit {
		if id := st.ids[end]; !excluded[id] {
			page = append(page, id)
		}
		end++
	}
	// Exclusions made since the station was computed apply to every batch.
	page, err = h.deps.Catalog.FilterExcludedTracks(ctx, user, page)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	tracks, err := h.deps.Catalog.TracksByIDs(ctx, page)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := RadioStationPage{Title: st.title, Seed: st.seed, Tracks: tracks, Remaining: len(st.ids) - end}
	if end < len(st.ids) {
		next := token + "." + strconv.Itoa(end)
		out.NextCursor = &next
	}
	response.OK(w, r, out)
}
