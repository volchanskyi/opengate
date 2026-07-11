package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestGetDeviceHistory(t *testing.T) {
	t.Parallel()
	const window = "from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z"
	tests := []struct {
		name       string
		online     bool
		found      bool
		owned      bool
		query      string
		wantStatus int
	}{
		{"device not found", false, false, true, "dim=cpu.total&" + window, http.StatusNotFound},
		{"not owner", true, true, false, "dim=cpu.total&" + window, http.StatusForbidden},
		{"offline", false, true, true, "dim=cpu.total&" + window, http.StatusNotFound},
		{"empty dim", true, true, true, "dim=&" + window, http.StatusBadRequest},
		{"window not increasing", true, true, true, "dim=cpu.total&from=2026-01-01T02:00:00Z&to=2026-01-01T01:00:00Z", http.StatusBadRequest},
		// Online but the agent never advertised Backfill: the broker refuses and
		// the pull is reported unavailable rather than hanging.
		{"online without capability", true, true, true, "dim=cpu.total&" + window, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupDeviceTest(t, tt.online)

			token := env.ownerToken
			if !tt.owned {
				other := testutil.SeedUser(t, env.ctx, env.store)
				var err error
				token, err = env.generateToken(other.ID, other.Email, other.IsAdmin)
				require.NoError(t, err)
			}

			targetID := env.device.ID
			if !tt.found {
				targetID = uuid.New()
			}

			path := "/api/v1/devices/" + targetID.String() + "/history?" + tt.query
			w := doRequest(env.srv, http.MethodGet, path, token, nil)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestClampHistoryMaxPoints(t *testing.T) {
	assert.Equal(t, uint32(defaultHistoryMaxPoints), clampHistoryMaxPoints(nil))
	lo := minHistoryMaxPoints - 5
	assert.Equal(t, uint32(minHistoryMaxPoints), clampHistoryMaxPoints(&lo))
	hi := maxHistoryMaxPoints + 100
	assert.Equal(t, uint32(maxHistoryMaxPoints), clampHistoryMaxPoints(&hi))
	mid := 500
	assert.Equal(t, uint32(500), clampHistoryMaxPoints(&mid))
}

func TestHistoryPointsToAPI(t *testing.T) {
	out := historyPointsToAPI([]protocol.HistoryPoint{{TS: 1, Value: 2.5}, {TS: 3, Value: 4}})
	require.Len(t, out, 2)
	assert.Equal(t, int64(3), out[1].Ts)
	assert.InEpsilon(t, 4.0, out[1].Value, 1e-9)
	assert.Empty(t, historyPointsToAPI(nil))
}

func TestHistoryBrokerErrorResponse(t *testing.T) {
	capResp, err := historyBrokerErrorResponse(agentapi.ErrCapabilityNotAdvertised)
	require.NoError(t, err)
	assert.IsType(t, GetDeviceHistory404JSONResponse{}, capResp)

	busyResp, err := historyBrokerErrorResponse(agentapi.ErrHistoryBusy)
	require.NoError(t, err)
	assert.IsType(t, GetDeviceHistory409JSONResponse{}, busyResp)

	timeoutResp, err := historyBrokerErrorResponse(context.DeadlineExceeded)
	require.NoError(t, err)
	assert.IsType(t, GetDeviceHistory504JSONResponse{}, timeoutResp)

	// An unexpected error is surfaced as a 500 (returned as a non-nil error).
	resp, err := historyBrokerErrorResponse(errors.New("boom"))
	assert.Nil(t, resp)
	assert.Error(t, err)
}
