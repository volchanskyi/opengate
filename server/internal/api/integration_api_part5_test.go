package api_test

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

	// Create a site for device listing
	resp := env.doJSON(t, http.MethodPost, pathSites, token, map[string]string{"name": "concurrent-site"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var site device.Site
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&site))
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
				resp = env.doJSON(t, http.MethodGet, pathSites, token, nil)
			case 3:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/devices?site_id="+site.ID.String(), token, nil)
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
