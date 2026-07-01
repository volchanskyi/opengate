package integration

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentRequests(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	token := env.register(t, "concurrent@example.com", "pass1234")

	// Create a group for device listing
	resp := env.doJSON(t, http.MethodPost, pathGroups, token, map[string]string{"name": "concurrent-group"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var group device.Group
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&group))
	resp.Body.Close()

	// Fire 20 concurrent requests across different endpoints
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var resp *http.Response
			switch i % 4 {
			case 0:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/health", "", nil)
			case 1:
				resp = env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
			case 2:
				resp = env.doJSON(t, http.MethodGet, pathGroups, token, nil)
			case 3:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), token, nil)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- http.ErrAbortHandler
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	assert.Empty(t, errors, "some concurrent requests failed")
}
