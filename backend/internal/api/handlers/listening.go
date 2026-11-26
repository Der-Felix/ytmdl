package handlers

import (
	"net/http"
	"os"
	"strings"
	"time"
	// The container image may carry no zoneinfo; the clients send IANA names.
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
)

// ListeningFeatures are the listening capabilities this server offers, as
// reported by GET /health. A client checks for a name before it uses the
// matching endpoints; an older server lists none of them.
var ListeningFeatures = []string{
	"mixes.groups",
	"mixes.artist",
	"mixes.decade",
	"mixes.favorites",
	"mixes.daypart",
	"radio.seeds",
	"radio.station",
	"history.stats",
	"search.lyrics",
	"listening.preferences",
}

// requestLocation resolves the tz query parameter (an IANA zone name such as
// Europe/Berlin). Without one it falls back to the server's TZ environment
// variable, then UTC. An unknown name in the parameter is an error.
func requestLocation(r *http.Request) (*time.Location, error) {
	name := strings.TrimSpace(queryString(r, "tz"))
	fromRequest := name != ""
	if !fromRequest {
		name = strings.TrimPrefix(strings.TrimSpace(os.Getenv("TZ")), ":")
	}
	if name == "" || name == "Local" || len(name) > 64 {
		if fromRequest {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Unbekannte Zeitzone.")
		}
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		if fromRequest {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Unbekannte Zeitzone.")
		}
		return time.UTC, nil
	}
	return loc, nil
}

// ListeningStats answers GET /history/stats?period=week|month|year|all.
func (h *Handlers) ListeningStats(w http.ResponseWriter, r *http.Request) {
	loc, err := requestLocation(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	limit, _, err := pagination(r, 10, repository.MaxStatsLimit)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	period := strings.TrimSpace(queryString(r, "period"))
	if period == "" {
		period = repository.StatsPeriodMonth
	}
	stats, err := h.deps.Catalog.ListeningStatsAt(r.Context(), middleware.UserFromContext(r.Context()).ID, period, limit, time.Now(), loc)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, stats)
}

// ListeningPreferences answers GET /listening-preferences.
func (h *Handlers) ListeningPreferences(w http.ResponseWriter, r *http.Request) {
	out, err := h.deps.Catalog.ListeningExclusions(r.Context(), middleware.UserFromContext(r.Context()).ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, out)
}

// AddListeningPreference answers PUT /listening-preferences with
// {"kind": "artist"|"genre"|"track", "target": "..."}; it is idempotent.
func (h *Handlers) AddListeningPreference(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	out, err := h.deps.Catalog.AddListeningExclusion(r.Context(), middleware.UserFromContext(r.Context()).ID, req.Kind, req.Target)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, out)
}

// RemoveListeningPreference answers DELETE /listening-preferences/{id}.
func (h *Handlers) RemoveListeningPreference(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Catalog.RemoveListeningExclusion(r.Context(), middleware.UserFromContext(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// RemoveListeningPreferenceByTarget answers DELETE
// /listening-preferences?kind=…&target=…; it succeeds when there is none.
func (h *Handlers) RemoveListeningPreferenceByTarget(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := h.deps.Catalog.RemoveListeningExclusionByTarget(r.Context(), middleware.UserFromContext(r.Context()).ID, q.Get("kind"), q.Get("target")); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
