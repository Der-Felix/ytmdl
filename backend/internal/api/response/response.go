// Package response renders the API's uniform JSON envelopes. Every successful
// answer carries its payload under "data"; every failure carries a machine
// readable code under "error".
package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"ytdm/backend/internal/apperr"
)

// contextKey is the private key type of this package.
type contextKey struct{ name string }

// RequestIDKey carries the request id through the context.
var RequestIDKey = &contextKey{name: "request-id"}

// RequestID reads the request id from a request context.
func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id, ok := r.Context().Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// Meta describes a list result.
type Meta struct {
	Count  int `json:"count"`
	Total  int `json:"total"`
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// envelope is the successful answer shape.
type envelope struct {
	Data any   `json:"data"`
	Meta *Meta `json:"meta,omitempty"`
}

// errorEnvelope is the failure answer shape.
type errorEnvelope struct {
	Error Detail `json:"error"`
}

// Detail is the machine readable error description.
type Detail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// JSON writes a raw payload.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if payload == nil {
		return
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		// The status line is already on the wire, so the only thing left is to
		// record why the body is incomplete.
		slog.Default().Error("the response body could not be written", "error", err.Error())
	}
}

// Data writes a successful answer.
func Data(w http.ResponseWriter, status int, data any) {
	JSON(w, status, envelope{Data: data})
}

// OK writes a 200 OK data envelope.
func OK(w http.ResponseWriter, r *http.Request, data any) {
	Data(w, http.StatusOK, data)
}

// Created writes a 201 Created data envelope.
func Created(w http.ResponseWriter, r *http.Request, data any) {
	Data(w, http.StatusCreated, data)
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// List writes a successful list answer including its meta block.
func List(w http.ResponseWriter, data any, meta Meta) {
	JSON(w, http.StatusOK, envelope{Data: data, Meta: &meta})
}

// Error writes an application error with the status its code maps to.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	code := apperr.CodeOf(err)
	Fail(w, r, code, apperr.MessageOf(err))
}

// Fail writes an error built from a code and a message.
func Fail(w http.ResponseWriter, r *http.Request, code apperr.Code, message string) {
	if code == "" {
		code = apperr.CodeInternal
	}
	JSON(w, apperr.HTTPStatus(code), errorEnvelope{Error: Detail{
		Code:      string(code),
		Message:   message,
		RequestID: RequestID(r),
	}})
}

// NotFound answers an unknown route.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Fail(w, r, apperr.CodeInvalidRequest, "The requested endpoint does not exist.")
}

// MethodNotAllowed answers a known route with the wrong method.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusMethodNotAllowed, errorEnvelope{Error: Detail{
		Code:      string(apperr.CodeInvalidRequest),
		Message:   "The HTTP method is not allowed for this endpoint.",
		RequestID: RequestID(r),
	}})
}
