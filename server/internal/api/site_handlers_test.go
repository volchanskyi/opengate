package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/device"
)

const (
	testPathSites  = "/api/v1/sites"
	testPathSiteID = "/api/v1/sites/"
)

func TestSiteHandlers(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	// Site reads are open to every member; creating and deleting one is a
	// configuration change behind the admin gate.
	_, token := seedTestUser(t, srv, cfg, "grp@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "grp-admin@example.com", true)

	var createdSiteID uuid.UUID

	t.Run("create site", func(t *testing.T) {
		body := map[string]string{"name": "my-site"}
		w := doRequest(srv, http.MethodPost, testPathSites, adminToken, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var g device.Site
		json.NewDecoder(w.Body).Decode(&g)
		assert.Equal(t, "my-site", g.Name)
		createdSiteID = g.ID
	})

	t.Run("list sites", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSites, token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var sites []*device.Site
		json.NewDecoder(w.Body).Decode(&sites)
		assert.Len(t, sites, 1)
	})

	t.Run("get site", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSiteID+createdSiteID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get site not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSiteID+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("create site missing name", func(t *testing.T) {
		body := map[string]string{}
		w := doRequest(srv, http.MethodPost, testPathSites, adminToken, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create site invalid json", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, testPathSites, adminToken, "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get site invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSiteID+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete site invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSiteID+"not-a-uuid", adminToken, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete site", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSiteID+createdSiteID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
