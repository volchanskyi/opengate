package telemetry

import (
	"bytes"
	"time"

	"github.com/google/uuid"
)

// Scope names the machine a reading belongs to. The connection sets it; a reading never does.
type Scope struct {
	Tenant uuid.UUID
	Device uuid.UUID
}

// encodedReadings is one hand-off's readings in the store's import format, encoded once.
type encodedReadings struct {
	body    []byte
	kept    int
	refused int
	// first is the earliest reading kept, which a dropped live minute is asked for again from.
	first time.Time
}

// encodeReadings checks each reading on its own and encodes the ones the store accepts under the
// scope's tenant and device; a reading the store would refuse is counted and costs nothing else.
func encodeReadings(scope Scope, samples []Sample) encodedReadings {
	var out encodedReadings
	var body bytes.Buffer
	for _, sample := range samples {
		if err := writePrometheusSample(&body, scope.Tenant, scope.Device, sample); err != nil {
			out.refused++
			continue
		}
		out.kept++
		if out.first.IsZero() || sample.TS.Before(out.first) {
			out.first = sample.TS
		}
	}
	out.body = body.Bytes()
	return out
}

// Encoded is a hand-off's readings encoded once, so admission can weigh them before they queue.
type Encoded struct{ readings encodedReadings }

// Encode checks and encodes readings under the scope's tenant and device.
func Encode(scope Scope, samples []Sample) *Encoded {
	return &Encoded{readings: encodeReadings(scope, samples)}
}

// Size is the encoded bytes the readings take in the queue.
func (e *Encoded) Size() int { return len(e.readings.body) }
