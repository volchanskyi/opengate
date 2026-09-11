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

// What the server said, and waiting for a row it has not written yet.
//
// A call that comes back with the wrong status used to report only the number.
// The filing path answers 404 for two different things — no such machine, and
// no such customer — so a run that met one of them could say that something was
// missing and never which, and the nightly that met it spent two nights on the
// question. The server names the missing thing in its reply; the caller simply
// threw the reply away.
//
// The one that was happening is worth stating, because it is a property of the
// transport rather than a fault: a machine's row is written when the server has
// finished reading its register frame, and the machine's own write returns as
// soon as the bytes are buffered locally. Nothing comes back down the stream to
// say the row landed. So a filing that follows the arrival straight away is a
// race the machine cannot see it is in, and it lost it on every one of the
// first five machines of a nightly.

// apiRefusal is a call the server answered with a status the caller did not
// ask for, carrying the server's own account of why.
type apiRefusal struct {
	Method string
	Path   string
	Status int
	Want   int
	// Detail is what the server said about it, trimmed to a line. It is the
	// half that names which of two identical statuses this one was.
	Detail string
}

func (r *apiRefusal) Error() string {
	if r.Detail == "" {
		return fmt.Sprintf("%s %s: server answered %d, expected %d", r.Method, r.Path, r.Status, r.Want)
	}
	return fmt.Sprintf("%s %s: server answered %d (%s), expected %d",
		r.Method, r.Path, r.Status, r.Detail, r.Want)
}

// saidItHasNoSuchThing reports whether err is the server saying the thing the
// call named is not there.
func saidItHasNoSuchThing(err error) bool {
	var refusal *apiRefusal
	return errors.As(err, &refusal) && refusal.Status == http.StatusNotFound
}

// filingWaitAttempts is how many times a filing asks for a machine the server
// says it does not have, and filingWaitStep is how much longer it waits between
// each. Together they are a window of three seconds, which is the gap between a
// machine's register frame leaving and its row landing on a target under the
// load a nightly puts on one.
//
// The wait is bounded because the same answer covers a customer that genuinely
// is not there, and asking again for that spends a request the arrivals need.
// Six attempts on each of the first five machines and the run stops asking
// altogether, which is a cost small enough to pay for telling the two apart.
const (
	filingWaitAttempts = 6
	filingWaitStep     = 200 * time.Millisecond
)

// waitForTheRow makes the call, and makes it again while the server says it has
// no such thing. It hands back the last refusal, so what the run reports is the
// server's final word rather than its first.
func waitForTheRow(call func() error) error {
	var err error
	for attempt := 1; attempt <= filingWaitAttempts; attempt++ {
		err = call()
		if err == nil || !saidItHasNoSuchThing(err) {
			return err
		}
		if attempt < filingWaitAttempts {
			time.Sleep(time.Duration(attempt) * filingWaitStep)
		}
	}
	return err
}

// detailMaxBytes is how much of a refused reply is read. The bodies are one
// short JSON object; anything longer is a page nobody meant to send here, and
// reading it into an error message would bury the message.
const detailMaxBytes = 512

// detailOf is what the server said about a refusal, as one line.
func detailOf(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, detailMaxBytes))
	if err != nil {
		return ""
	}

	// The servers here answer with {"error": "..."} and nothing else, so the
	// message alone is what a reader wants. A body in any other shape travels
	// whole rather than being dropped: an unexpected reply is exactly the case
	// where the text matters most.
	var said struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &said); err == nil && said.Error != "" {
		return said.Error
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}
