package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
)

// keepAliveInterval is how often a comment line is sent on an idle connection.
// It keeps the connection and any proxy in front of it from timing out.
const keepAliveInterval = 20 * time.Second

// Events answers GET /events with a server sent event stream of job progress.
//
// The handler owns exactly one subscription and releases it when the client
// disconnects, so a closed browser tab cannot leak a subscriber.
func (h *Handlers) Events(w http.ResponseWriter, r *http.Request) {
	jobFilter := queryString(r, "job_id")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Ask a reverse proxy not to buffer the stream.
	w.Header().Set("X-Accel-Buffering", "no")

	// The streaming probe has to run after the headers are set, because the
	// flush commits the response: anything set afterwards never reaches the
	// client, and a browser's EventSource rejects a stream that does not
	// arrive as text/event-stream. A writer without flush support fails here
	// while nothing has been written yet, so the error answer still works.
	controller := http.NewResponseController(w)
	if err := controller.Flush(); err != nil {
		response.Fail(w, r, apperr.CodeInternal, "The connection does not support streaming.")
		return
	}

	events, unsubscribe := h.deps.Jobs.Broker().Subscribe()
	defer unsubscribe()

	// The reconnect hint tells the browser's EventSource how long to wait.
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}

	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}

		case event, open := <-events:
			if !open {
				return
			}
			if jobFilter != "" && event.JobID != jobFilter {
				continue
			}
			if err := writeEvent(w, event); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

// writeEvent renders one event in the server sent event format.
func writeEvent(w http.ResponseWriter, event jobs.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload)
	return err
}
