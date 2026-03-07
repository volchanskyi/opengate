package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/db"
)

const (
	testPathGroups  = "/api/v1/groups"
	testPathGroupsS = "/api/v1/groups/"
)

func TestGroupHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "grp@example.com", false)

	var createdGroupID uuid.UUID

	t.Run("create group", func(t *testing.T) {
		body := map[string]string{"name": "my-group"}
		w := doRequest(srv, http.MethodPost, testPathGroups, token, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var g db.Group
		json.NewDecoder(w.Body).Decode(&g)
		assert.Equal(t, "my-group", g.Name)
		createdGroupID = g.ID
	})

	t.Run("list groups", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroups, token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var groups []*db.Group
		json.NewDecoder(w.Body).Decode(&groups)
		assert.Len(t, groups, 1)
	})

	t.Run("get group", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+createdGroupID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get group not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("create group missing name", func(t *testing.T) {
		body := map[string]string{}
		w := doRequest(srv, http.MethodPost, testPathGroups, token, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create group invalid json", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, testPathGroups, token, "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get group invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete group invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete group", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+createdGroupID.String(), token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
