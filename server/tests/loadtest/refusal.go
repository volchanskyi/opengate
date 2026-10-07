package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiRefusal is a call answered with an unexpected status, carrying the server's account of why.
type apiRefusal struct {
	Method string
	Path   string
	Status int
	Want   int
	// Detail is the server's explanation trimmed to a line; it tells identical statuses apart.
	Detail string
}

func (r *apiRefusal) Error() string {
	if r.Detail == "" {
		return fmt.Sprintf("%s %s: server answered %d, expected %d", r.Method, r.Path, r.Status, r.Want)
	}
	return fmt.Sprintf("%s %s: server answered %d (%s), expected %d",
		r.Method, r.Path, r.Status, r.Detail, r.Want)
}

// saidItHasNoSuchThing reports whether err is the server answering 404 for what the call named.
func saidItHasNoSuchThing(err error) bool {
	var refusal *apiRefusal
	return errors.As(err, &refusal) && refusal.Status == http.StatusNotFound
}

// A machine's row lands after the server reads its register frame, which the machine cannot see;
// six attempts with doubling waits span 15.5 seconds, past the ten seconds seen on busy legs.
const (
	filingWaitAttempts = 6
	filingWaitFirst    = 500 * time.Millisecond
)

// filingWaitDelay is how long the filing waits after its attempt-th request.
func filingWaitDelay(attempt int) time.Duration {
	return filingWaitFirst << (attempt - 1)
}

// waitForTheRow repeats the call while the server answers 404 and returns the last refusal.
// The wait is bounded because the same answer covers a customer that does not exist.
func waitForTheRow(call func() error) error {
	var err error
	for attempt := 1; attempt <= filingWaitAttempts; attempt++ {
		err = call()
		if err == nil || !saidItHasNoSuchThing(err) {
			return err
		}
		if attempt < filingWaitAttempts {
			time.Sleep(filingWaitDelay(attempt))
		}
	}
	return err
}

// detailMaxBytes caps how much of a refused reply is read; real bodies are one short JSON object.
const detailMaxBytes = 512

// detailOf is what the server said about a refusal, as one line.
func detailOf(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, detailMaxBytes))
	if err != nil {
		return ""
	}

	// A {"error": "..."} body yields the message alone; any other shape travels whole.
	var said struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &said); err == nil && said.Error != "" {
		return said.Error
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}
