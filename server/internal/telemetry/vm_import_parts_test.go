package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// roundTrip answers every request with 204 after doing what with does with it.
type roundTrip func(*http.Request)

func (rt roundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	rt(req)
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")),
		Request: req}, nil
}

func TestAnImportSendsItsPartsAsOneBodyOfTheirLength(t *testing.T) {
	var body string
	var length int64
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		body, length = string(read), r.ContentLength
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(store.Close)

	err := telemetry.NewVMClient(store.URL, nil).Import(context.Background(),
		[]byte("cpu{d=\"a\"} 1 1\n"), []byte("cpu{d=\"b\"} 2 1\n"))

	require.NoError(t, err)
	assert.Equal(t, "cpu{d=\"a\"} 1 1\ncpu{d=\"b\"} 2 1\n", body)
	assert.Equal(t, int64(len(body)), length, "the store is told the length up front")
}

func TestAnImportReturnsOnlyOnceTheTransportClosedItsBody(t *testing.T) {
	var closed atomic.Bool
	client := &http.Client{Transport: roundTrip(func(req *http.Request) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			_, _ = io.Copy(io.Discard, req.Body)
			closed.Store(true)
			_ = req.Body.Close()
		}()
	})}

	require.NoError(t, telemetry.NewVMClient("http://store.invalid", client).Import(context.Background(),
		[]byte("cpu 1 1\n")))

	assert.True(t, closed.Load(), "the parts are the caller's again only once nothing reads them")
}

func TestAnImportCanBeSentAgainFromItsParts(t *testing.T) {
	var again string
	client := &http.Client{Transport: roundTrip(func(req *http.Request) {
		_ = req.Body.Close()
		require.NotNil(t, req.GetBody, "a request on a connection found dead is sent again")
		body, err := req.GetBody()
		require.NoError(t, err)
		read, err := io.ReadAll(body)
		require.NoError(t, err)
		again = string(read)
		_ = body.Close()
	})}

	require.NoError(t, telemetry.NewVMClient("http://store.invalid", client).Import(context.Background(),
		[]byte("cpu 1 1\n"), []byte("mem 2 1\n")))

	assert.Equal(t, "cpu 1 1\nmem 2 1\n", again)
}

func TestAnImportThatCannotBeBuiltFailsWithoutWaiting(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- telemetry.NewVMClient("http://store.invalid/\x7f", nil).Import(context.Background(), []byte("cpu 1 1\n"))
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("an import no request was built for waited on a body nothing read")
	}
}
