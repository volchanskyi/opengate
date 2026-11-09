package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// vitalCount is the vitals a machine reports a minute, the readings one catch-up minute holds.
const vitalCount = 18

// vitals names the probe's dimensions; the store's cost follows their count.
var vitals = func() []string {
	names := make([]string, vitalCount)
	for i := range names {
		names[i] = "vital." + string(rune('a'+i))
	}
	return names
}()

// keptBody is a transport that keeps a request's body and answers it as stored, so the shipped
// client encodes without sending.
type keptBody struct {
	mu   sync.Mutex
	body []byte
}

func (k *keptBody) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.body = body
	k.mu.Unlock()
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")),
		Header: http.Header{}, Request: r}, nil
}

// encoder turns readings into the import lines the server writes, by running the server's own
// client against a transport that keeps the body.
type encoder struct {
	mu     sync.Mutex
	kept   *keptBody
	client *telemetry.VMClient
}

func newEncoder() *encoder {
	kept := &keptBody{}
	return &encoder{kept: kept, client: telemetry.NewVMClient("http://encoder.invalid", &http.Client{Transport: kept})}
}

// lines is device's readings as the server's import body.
func (e *encoder) lines(tenant, device uuid.UUID, samples []telemetry.Sample) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.client.WriteSamples(context.Background(), tenant, device, samples); err != nil {
		return nil, err
	}
	e.kept.mu.Lock()
	defer e.kept.mu.Unlock()
	return e.kept.body, nil
}
