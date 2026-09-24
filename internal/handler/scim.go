package handler

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/acme/agent-wrapper/internal/credential"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

// maxSCIMBody bounds a SCIM request. SCIM changes are small; a large group
// arrives as member PATCHes, not as one body.
const maxSCIMBody = 1 << 20

// Paging limits for SCIM lists.
const (
	defaultSCIMCount = 100
	maxSCIMCount     = 1000
)

func (h *Handler) scimRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /scim/v2/ServiceProviderConfig", h.requireSCIM(h.scimServiceProviderConfig))
	mux.HandleFunc("GET /scim/v2/ResourceTypes", h.requireSCIM(h.scimResourceTypes))
	mux.HandleFunc("GET /scim/v2/Schemas", h.requireSCIM(h.scimSchemas))
	mux.HandleFunc("GET /scim/v2/Users", h.requireSCIM(h.scimListUsers))
	mux.HandleFunc("POST /scim/v2/Users", h.requireSCIM(h.scimCreateUser))
	mux.HandleFunc("GET /scim/v2/Users/{id}", h.requireSCIM(h.scimGetUser))
	mux.HandleFunc("PUT /scim/v2/Users/{id}", h.requireSCIM(h.scimReplaceUser))
	mux.HandleFunc("PATCH /scim/v2/Users/{id}", h.requireSCIM(h.scimPatchUser))
	mux.HandleFunc("DELETE /scim/v2/Users/{id}", h.requireSCIM(h.scimDeleteUser))
	mux.HandleFunc("GET /scim/v2/Groups", h.requireSCIM(h.scimListGroups))
	mux.HandleFunc("POST /scim/v2/Groups", h.requireSCIM(h.scimCreateGroup))
	mux.HandleFunc("GET /scim/v2/Groups/{id}", h.requireSCIM(h.scimGetGroup))
	mux.HandleFunc("PUT /scim/v2/Groups/{id}", h.requireSCIM(h.scimReplaceGroup))
	mux.HandleFunc("PATCH /scim/v2/Groups/{id}", h.requireSCIM(h.scimPatchGroup))
	mux.HandleFunc("DELETE /scim/v2/Groups/{id}", h.requireSCIM(h.scimDeleteGroup))
	// A method-less catch-all: Go 1.22's ServeMux prefers a more specific
	// method+path pattern, so every route above still wins; this only
	// catches what none of them do, and answers with the SCIM error body
	// the RFC promises instead of ServeMux's default text/plain 405.
	mux.HandleFunc("/scim/v2/", h.requireSCIM(h.scimNoRoute))
}

// requireSCIM admits the identity provider's token. Unset answers 503 so a
// forgotten AWD_SCIM_TOKEN shows up in the IdP's provisioning log rather
// than leaving the endpoint open.
func (h *Handler) requireSCIM(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.SCIMToken == "" {
			writeSCIMError(w, &scim.Error{Status: http.StatusServiceUnavailable, Detail: "SCIM is not configured on this server"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(h.SCIMToken)) != 1 {
			writeSCIMError(w, &scim.Error{Status: http.StatusUnauthorized, Detail: "unauthorized"})
			return
		}
		next(w, r)
	}
}

func writeSCIM(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding a SCIM response", "err", err)
	}
}

func writeSCIMError(w http.ResponseWriter, e *scim.Error) {
	writeSCIM(w, e.Status, e.Body())
}

// scimFail maps an error to a SCIM error. It is the SCIM routes' fail.
func (h *Handler) scimFail(w http.ResponseWriter, r *http.Request, err error) {
	var scimErr *scim.Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &scimErr):
		writeSCIMError(w, scimErr)
	case errors.As(err, &tooLarge):
		writeSCIMError(w, &scim.Error{Status: http.StatusRequestEntityTooLarge, Detail: fmt.Sprintf("the body exceeds %d bytes", maxSCIMBody)})
	case errors.Is(err, model.ErrNotFound):
		writeSCIMError(w, scim.NotFound("no such resource"))
	case errors.Is(err, model.ErrConflict):
		writeSCIMError(w, scim.Uniqueness(err.Error()))
	case errors.Is(err, model.ErrBadInput):
		writeSCIMError(w, scim.InvalidValue(err.Error()))
	default:
		h.log.ErrorContext(r.Context(), "unhandled SCIM error", "err", err, "path", r.URL.Path)
		writeSCIMError(w, &scim.Error{Status: http.StatusInternalServerError, Detail: "internal error"})
	}
}

// scimBody reads the whole request body, capped. It is read before any
// decoding so that an oversized body surfaces as the *http.MaxBytesError
// scimFail turns into a 413, not as a decoder's invalidSyntax.
func scimBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(w, r.Body, maxSCIMBody))
}

// scimBase is this server's SCIM root as the IdP reached it, for
// meta.location. X-Forwarded-Proto covers a TLS-terminating proxy.
func scimBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host + "/scim/v2"
}

// listParams reads startIndex and count with the RFC's defaults.
func listParams(r *http.Request) (startIndex, count int, err error) {
	startIndex, count = 1, defaultSCIMCount
	if v := r.URL.Query().Get("startIndex"); v != "" {
		if startIndex, err = strconv.Atoi(v); err != nil {
			return 0, 0, scim.InvalidValue("startIndex must be an integer")
		}
		if startIndex < 1 {
			startIndex = 1
		}
	}
	if v := r.URL.Query().Get("count"); v != "" {
		if count, err = strconv.Atoi(v); err != nil {
			return 0, 0, scim.InvalidValue("count must be an integer")
		}
		count = max(0, min(count, maxSCIMCount))
	}
	return startIndex, count, nil
}

func (h *Handler) scimServiceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	writeSCIM(w, http.StatusOK, scim.ServiceProviderConfig())
}

func (h *Handler) scimResourceTypes(w http.ResponseWriter, r *http.Request) {
	writeSCIM(w, http.StatusOK, scim.ResourceTypes(scimBase(r)))
}

func (h *Handler) scimSchemas(w http.ResponseWriter, _ *http.Request) {
	writeSCIM(w, http.StatusOK, scim.Schemas())
}

func (h *Handler) userLocation(r *http.Request, id string) string {
	return scimBase(r) + "/Users/" + id
}

func (h *Handler) scimListUsers(w http.ResponseWriter, r *http.Request) {
	filter, err := scim.ParseFilter(r.URL.Query().Get("filter"), model.SCIMAttrUserName, model.SCIMAttrExternalID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	startIndex, count, err := listParams(r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	users, total, err := h.store.ListSCIMUsers(r.Context(), filter, startIndex, count)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	out := make([]scim.User, 0, len(users))
	for _, u := range users {
		out = append(out, scim.EncodeUser(u, h.userLocation(r, u.ID)))
	}
	writeSCIM(w, http.StatusOK, scim.NewListResponse(out, total, startIndex, len(out)))
}

func (h *Handler) scimCreateUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := scim.DecodeUser(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if u.ID, err = credential.NewID(); err != nil {
		h.scimFail(w, r, err)
		return
	}
	u.Created, u.Modified = h.Now(), h.Now()
	if err := h.store.CreateSCIMUser(r.Context(), u); err != nil {
		h.scimFail(w, r, err)
		return
	}
	location := h.userLocation(r, u.ID)
	w.Header().Set("Location", location)
	writeSCIM(w, http.StatusCreated, scim.EncodeUser(u, location))
}

func (h *Handler) scimGetUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.store.SCIMUser(r.Context(), r.PathValue("id"))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeUser(u, h.userLocation(r, u.ID)))
}

func (h *Handler) scimReplaceUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := scim.DecodeUser(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u.ID, u.Modified = r.PathValue("id"), h.Now()
	if err := h.store.ReplaceSCIMUser(r.Context(), u); err != nil {
		h.scimFail(w, r, err)
		return
	}
	h.scimGetUser(w, r)
}

func (h *Handler) scimPatchUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	change, err := scim.UserPatch(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := h.store.PatchSCIMUser(r.Context(), r.PathValue("id"), change, h.Now())
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeUser(u, h.userLocation(r, u.ID)))
}

func (h *Handler) scimDeleteUser(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteSCIMUser(r.Context(), r.PathValue("id")); err != nil {
		h.scimFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// scimNoRoute answers a request under /scim/v2 that no registered route
// matched, whether the path is unknown or the method is wrong for it.
func (h *Handler) scimNoRoute(w http.ResponseWriter, r *http.Request) {
	writeSCIMError(w, scim.NotFound("no such SCIM endpoint or method: "+r.Method+" "+r.URL.Path))
}

func (h *Handler) groupLocation(r *http.Request, id string) string {
	return scimBase(r) + "/Groups/" + id
}

// withMembers reports whether the IdP wants members in the answer. Both
// target IdPs ask to exclude them when reading large groups.
func withMembers(r *http.Request) bool {
	for _, attr := range strings.Split(r.URL.Query().Get("excludedAttributes"), ",") {
		if strings.EqualFold(strings.TrimSpace(attr), "members") {
			return false
		}
	}
	return true
}

func (h *Handler) scimListGroups(w http.ResponseWriter, r *http.Request) {
	filter, err := scim.ParseFilter(r.URL.Query().Get("filter"), model.SCIMAttrDisplayName, model.SCIMAttrExternalID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	startIndex, count, err := listParams(r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	groups, total, err := h.store.ListSCIMGroups(r.Context(), filter, startIndex, count)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	members := withMembers(r)
	out := make([]scim.Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, scim.EncodeGroup(g, h.groupLocation(r, g.ID), members))
	}
	writeSCIM(w, http.StatusOK, scim.NewListResponse(out, total, startIndex, len(out)))
}

func (h *Handler) scimCreateGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := scim.DecodeGroup(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if g.ID, err = credential.NewID(); err != nil {
		h.scimFail(w, r, err)
		return
	}
	g.Created, g.Modified = h.Now(), h.Now()
	if err := h.store.CreateSCIMGroup(r.Context(), g); err != nil {
		h.scimFail(w, r, err)
		return
	}
	stored, err := h.store.SCIMGroup(r.Context(), g.ID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	location := h.groupLocation(r, g.ID)
	w.Header().Set("Location", location)
	writeSCIM(w, http.StatusCreated, scim.EncodeGroup(stored, location, true))
}

func (h *Handler) scimGetGroup(w http.ResponseWriter, r *http.Request) {
	g, err := h.store.SCIMGroup(r.Context(), r.PathValue("id"))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeGroup(g, h.groupLocation(r, g.ID), withMembers(r)))
}

func (h *Handler) scimReplaceGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := scim.DecodeGroup(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g.ID, g.Modified = r.PathValue("id"), h.Now()
	if err := h.store.ReplaceSCIMGroup(r.Context(), g); err != nil {
		h.scimFail(w, r, err)
		return
	}
	h.scimGetGroup(w, r)
}

// scimPatchGroup answers 204 unless the IdP asked for attributes back;
// Entra accepts either, and 204 spares serializing a large member list.
func (h *Handler) scimPatchGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	change, err := scim.GroupPatch(bytes.NewReader(body))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := h.store.PatchSCIMGroup(r.Context(), r.PathValue("id"), change, h.Now())
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if r.URL.Query().Get("attributes") == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeGroup(g, h.groupLocation(r, g.ID), withMembers(r)))
}

func (h *Handler) scimDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteSCIMGroup(r.Context(), r.PathValue("id")); err != nil {
		h.scimFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
