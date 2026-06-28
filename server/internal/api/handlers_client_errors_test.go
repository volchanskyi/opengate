package api

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportClientError(t *testing.T) {
	t.Parallel()

	newLoggingServer := func() (*Server, *bytes.Buffer) {
		var buf bytes.Buffer
		srv := NewServer(ServerConfig{Logger: slog.New(slog.NewTextHandler(&buf, nil))})
		return srv, &buf
	}

	t.Run("happy path logs and returns 204", func(t *testing.T) {
		t.Parallel()
		srv, buf := newLoggingServer()
		src := "ErrorBoundary"
		stack := "TypeError: x is undefined\n  at Foo"
		resp, err := srv.ReportClientError(context.Background(), ReportClientErrorRequestObject{
			Body: &ReportClientErrorJSONRequestBody{Message: "boom", Source: &src, Stack: &stack},
		})
		require.NoError(t, err)
		assert.IsType(t, ReportClientError204Response{}, resp)
		out := buf.String()
		assert.Contains(t, out, "client error")
		assert.Contains(t, out, "boom")
	})

	t.Run("nil body is rejected", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLoggingServer()
		resp, err := srv.ReportClientError(context.Background(), ReportClientErrorRequestObject{})
		require.NoError(t, err)
		assert.IsType(t, ReportClientError400JSONResponse{}, resp)
	})

	t.Run("empty message is rejected", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLoggingServer()
		resp, err := srv.ReportClientError(context.Background(), ReportClientErrorRequestObject{
			Body: &ReportClientErrorJSONRequestBody{Message: ""},
		})
		require.NoError(t, err)
		assert.IsType(t, ReportClientError400JSONResponse{}, resp)
	})

	t.Run("oversize fields are truncated in the log", func(t *testing.T) {
		t.Parallel()
		srv, buf := newLoggingServer()
		bigStack := strings.Repeat("A", 5000)
		bigMsg := strings.Repeat("B", 5000)
		_, err := srv.ReportClientError(context.Background(), ReportClientErrorRequestObject{
			Body: &ReportClientErrorJSONRequestBody{Message: bigMsg, Stack: &bigStack},
		})
		require.NoError(t, err)
		// Neither the full 5000-char stack nor message is written verbatim.
		assert.NotContains(t, buf.String(), bigStack)
		assert.NotContains(t, buf.String(), bigMsg)
	})
}
