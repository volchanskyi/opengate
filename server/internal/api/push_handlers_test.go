package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPushHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "push@example.com", false)

	t.Run("subscribe push", func(t *testing.T) {
		body := map[string]string{
			"endpoint": "https://push.example.com/sub/123",
			"p256dh":   "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8p8REfnFs",
			"auth":     "tBHItJI5svbpC7BmuFnh5A",
		}
		w := doRequest(srv, http.MethodPost, "/api/v1/push/subscribe", token, body)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("subscribe push without auth returns 401", func(t *testing.T) {
		body := map[string]string{
			"endpoint": "https://push.example.com/sub/456",
			"p256dh":   "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8p8REfnFs",
			"auth":     "tBHItJI5svbpC7BmuFnh5A",
		}
		w := doRequest(srv, http.MethodPost, "/api/v1/push/subscribe", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unsubscribe push", func(t *testing.T) {
		// First subscribe
		body := map[string]string{
			"endpoint": "https://push.example.com/sub/789",
			"p256dh":   "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8p8REfnFs",
			"auth":     "tBHItJI5svbpC7BmuFnh5A",
		}
		w := doRequest(srv, http.MethodPost, "/api/v1/push/subscribe", token, body)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Then unsubscribe
		unsub := map[string]string{"endpoint": "https://push.example.com/sub/789"}
		w = doRequest(srv, http.MethodDelete, "/api/v1/push/subscribe", token, unsub)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("unsubscribe push without auth returns 401", func(t *testing.T) {
		body := map[string]string{"endpoint": "https://push.example.com/sub/000"}
		w := doRequest(srv, http.MethodDelete, "/api/v1/push/subscribe", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("get vapid key", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/push/vapid-key", token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		_, ok := resp["public_key"]
		assert.True(t, ok, "response should contain public_key")
	})

	t.Run("get vapid key without auth returns 401", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/push/vapid-key", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
