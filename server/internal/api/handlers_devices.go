package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.URL.Query().Get("group_id")
	if groupIDStr == "" {
		writeError(w, http.StatusBadRequest, "group_id query parameter is required")
		return
	}

	groupID, err := uuid.Parse(groupIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}

	devices, err := s.store.ListDevices(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}

	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	device, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get device")
		return
	}

	writeJSON(w, http.StatusOK, device)
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete device")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
