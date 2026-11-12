package telemetry

import (
	"encoding/hex"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// tenantLabel and deviceLabel are the labels the server stamps on every reading it writes.
const (
	tenantLabel = "tenant_id"
	deviceLabel = "device_id"
)

// uuidLen is a machine or tenant identifier's length in its dashed text form.
const uuidLen = 36

// lineOverhead bounds the bytes a line spends beyond its name and labels: braces, the two
// stamped labels, the value, the timestamp and the separators between them.
const lineOverhead = 2 + 2*(len(tenantLabel)+3+uuidLen+1) + 1 + 24 + 1 + 20 + 1

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

// encodeReadings encodes the readings the store accepts under the scope's tenant and device into a
// body sized once to its bound; a reading the store would refuse is counted and costs nothing else.
func encodeReadings(scope Scope, samples []Sample) encodedReadings {
	var out encodedReadings
	bound := 0
	for i := range samples {
		bound += lineBound(&samples[i])
	}
	body := make([]byte, 0, bound)
	keys := make([]string, 0, 4)
	for _, sample := range samples {
		line, sorted, err := appendSample(body, keys, scope, sample)
		keys = sorted
		if err != nil {
			out.refused++
			continue
		}
		body = line
		out.kept++
		if out.first.IsZero() || sample.TS.Before(out.first) {
			out.first = sample.TS
		}
	}
	out.body = body
	return out
}

// lineBound is the most bytes a reading's line can take, each escaped byte taking two.
func lineBound(sample *Sample) int {
	n := len(sample.Name) + lineOverhead
	for k, v := range sample.Labels {
		n += len(k) + len(v) + 4
		for i := 0; i < len(v); i++ {
			if c := v[i]; c == '\\' || c == '\n' || c == '"' {
				n++
			}
		}
	}
	return n
}

// appendSample appends one reading as a line of the store's import format, its labels sorted
// with the scope's tenant and device among them; keys is scratch space handed back for reuse.
func appendSample(dst []byte, keys []string, scope Scope, sample Sample) ([]byte, []string, error) {
	if err := CheckSample(sample); err != nil {
		return dst, keys, err
	}
	at := sample.TS
	if at.IsZero() {
		at = time.Now()
	}
	keys = append(keys[:0], tenantLabel, deviceLabel)
	for k := range sample.Labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	dst = append(dst, sample.Name...)
	dst = append(dst, '{')
	for i, k := range keys {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, k...)
		dst = append(dst, '=', '"')
		switch k {
		case tenantLabel:
			dst = appendUUID(dst, scope.Tenant)
		case deviceLabel:
			dst = appendUUID(dst, scope.Device)
		default:
			dst = appendEscaped(dst, sample.Labels[k])
		}
		dst = append(dst, '"')
	}
	dst = append(dst, '}', ' ')
	dst = strconv.AppendFloat(dst, sample.Value, 'g', -1, 64)
	dst = append(dst, ' ')
	dst = strconv.AppendInt(dst, at.UnixMilli(), 10)
	return append(dst, '\n'), keys, nil
}

// appendUUID appends id in its dashed text form, the form uuid.UUID.String prints.
func appendUUID(dst []byte, id uuid.UUID) []byte {
	var text [uuidLen]byte
	hex.Encode(text[0:8], id[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], id[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], id[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], id[8:10])
	text[23] = '-'
	hex.Encode(text[24:], id[10:])
	return append(dst, text[:]...)
}

// appendEscaped appends a label value with its backslashes, line breaks and quotes escaped.
func appendEscaped(dst []byte, v string) []byte {
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '"':
			dst = append(dst, '\\', '"')
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// validMetricName reports whether name is a metric name the store accepts: a letter, underscore
// or colon, then letters, digits, underscores and colons.
func validMetricName(name string) bool {
	return validName(name, true)
}

// validLabelName reports whether name is a label name the store accepts: a letter or underscore,
// then letters, digits and underscores.
func validLabelName(name string) bool {
	return validName(name, false)
}

func validName(name string, colons bool) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', colons && c == ':':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// Encoded is a hand-off's readings encoded once, so admission can weigh them before they queue.
type Encoded struct{ readings encodedReadings }

// Encode checks and encodes readings under the scope's tenant and device.
func Encode(scope Scope, samples []Sample) *Encoded {
	return &Encoded{readings: encodeReadings(scope, samples)}
}

// Size is the encoded bytes the readings take in the queue.
func (e *Encoded) Size() int { return len(e.readings.body) }
