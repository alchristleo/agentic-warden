package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/acme/agent-wrapper/internal/credential"
	"github.com/acme/agent-wrapper/internal/model"
)

// defaultTokenTTL is how long an enrollment token lives unless the request
// says otherwise. A day covers "mint it now, run the installer this
// afternoon" without leaving tokens lying around for weeks.
const defaultTokenTTL = 24 * time.Hour

// postEnrollmentToken mints a single-use token that enrolls one machine for
// one user. The plaintext is returned once and never stored.
func (h *Handler) postEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		TTL  string `json:"ttl"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.User == "" {
		writeError(w, http.StatusUnprocessableEntity, "user is required")
		return
	}
	ttl := defaultTokenTTL
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "ttl must be a positive duration such as 24h")
			return
		}
		ttl = parsed
	}
	plain, hash, err := credential.New()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	expires := h.Now().Add(ttl)
	if err := h.store.PutEnrollmentToken(r.Context(), model.EnrollmentToken{Hash: hash, User: req.User, ExpiresAt: expires}); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "user": req.User, "expiresAt": expires})
}

// postEnroll exchanges an enrollment token for a machine credential. It is
// the one unauthenticated write, because the token is the credential.
func (h *Handler) postEnroll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Name  string `json:"name"`
		OS    string `json:"os"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusUnprocessableEntity, "token is required")
		return
	}
	now := h.Now()
	user, err := h.store.ConsumeEnrollmentToken(r.Context(), credential.Hash(req.Token), now)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := credential.NewID()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	plain, hash, err := credential.New()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	machine := model.Machine{ID: id, User: user, Name: req.Name, OS: req.OS, CredentialHash: hash, EnrolledAt: now}
	if err := h.store.PutMachine(r.Context(), machine); err != nil {
		h.fail(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "machine enrolled", "machine", id, "user", user, "name", req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"machineId": id, "credential": plain, "user": user})
}

func (h *Handler) getMachines(w http.ResponseWriter, r *http.Request) {
	machines, err := h.store.ListMachines(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, machines)
}

func (h *Handler) deleteMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteMachine(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "machine revoked", "machine", id)
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON reads a small JSON body, rejecting unknown fields so a typo in
// a request is an error rather than a silently ignored option.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return errors.New("the request body is not valid: " + err.Error())
	}
	return nil
}
