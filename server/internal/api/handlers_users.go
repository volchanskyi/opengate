package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	userID := ContextUserID(r.Context())

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get user")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	claims := ContextClaims(r.Context())
	if claims == nil || !claims.IsAdmin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	claims := ContextClaims(r.Context())
	if claims == nil || !claims.IsAdmin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
