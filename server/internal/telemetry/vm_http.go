package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getChecked issues a GET for path and returns the response with its body open; a transport
// failure or non-200 status becomes an error, and on success the caller owns resp.Body.
func (v *VMClient) getChecked(ctx context.Context, path, op string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build vm %s request: %w", op, err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get vm %s: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, vmStatusError(resp, op)
	}
	return resp, nil
}

// sendChecked runs a prepared request, succeeds on any 2xx status and always closes the body.
func (v *VMClient) sendChecked(req *http.Request, verb, op string) error {
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s vm %s: %w", verb, op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return vmStatusError(resp, op)
	}
	return nil
}

// vmStatusError formats a non-success VM response into an error, including a
// bounded snippet of the response body for context.
func vmStatusError(resp *http.Response, op string) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("vm %s status %d: %s", op, resp.StatusCode, strings.TrimSpace(string(msg)))
}
