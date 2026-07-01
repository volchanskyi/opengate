package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestAmtPowerActionNotConnected(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr ApiError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, "device not connected", apiErr.Error)
}

func TestAmtPowerActionUnauthorized(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := AMTPowerRequest{Action: PowerOn}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", "", body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
