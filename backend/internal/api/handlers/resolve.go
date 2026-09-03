package handlers

import (
	"net/http"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

// Resolve answers GET /resolve?url=. It turns a pasted address into the
// provider identity the catalogue endpoints take, so that a client does not
// have to know the id formats of each provider.
//
// An address that cannot be read is an INVALID_REQUEST, which is how a caller
// learns to treat the input as a search query instead.
func (h *Handlers) Resolve(w http.ResponseWriter, r *http.Request) {
	target := queryString(r, "url")
	if target == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "The query parameter url is required.")
		return
	}
	if h.deps.Resolver == nil {
		response.Fail(w, r, apperr.CodeInternal, "Der Resolver ist nicht verfügbar.")
		return
	}

	ref, err := h.deps.Resolver.Resolve(r.Context(), target)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, ref)
}
