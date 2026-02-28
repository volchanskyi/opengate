package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

type createGroupRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	group := &db.Group{
		ID:      uuid.New(),
		Name:    req.Name,
		OwnerID: ContextUserID(r.Context()),
	}

	if err := s.store.CreateGroup(r.Context(), group); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create group")
		return
	}

	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	userID := ContextUserID(r.Context())
	groups, err := s.store.ListGroups(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}

	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}

	group, err := s.store.GetGroup(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get group")
		return
	}

	writeJSON(w, http.StatusOK, group)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}

	if err := s.store.DeleteGroup(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete group")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
