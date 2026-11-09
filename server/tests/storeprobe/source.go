package main

import (
	"bytes"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// catchUpMetric is the series catch-up writes, the one live readings also write.
const catchUpMetric = "opengate_edge_metric_avg"

// catchUpSource hands out catch-up requests in the shape the shared writer sends: whole readings
// from many machines, each machine's history one minute at a time, no reading twice.
type catchUpSource struct {
	enc     *encoder
	tenant  uuid.UUID
	devices []uuid.UUID
	newest  time.Time
	// minute counts back from newest, and device walks the machines within a minute.
	minute, device int
	pending        [][]byte
}

func newCatchUpSource(enc *encoder, devices int, now time.Time) *catchUpSource {
	ids := make([]uuid.UUID, devices)
	for i := range ids {
		ids[i] = uuid.New()
	}
	return &catchUpSource{enc: enc, tenant: uuid.New(), devices: ids, newest: now.Truncate(time.Minute)}
}

// nextLines is the next machine-minute's readings, one line each.
func (s *catchUpSource) nextLines() ([][]byte, error) {
	ts := s.newest.Add(-time.Duration(s.minute+1) * time.Minute)
	samples := make([]telemetry.Sample, len(vitals))
	for i, dim := range vitals {
		samples[i] = telemetry.Sample{Name: catchUpMetric, Value: float64(i), TS: ts, Labels: map[string]string{"dim": dim}}
	}
	body, err := s.enc.lines(s.tenant, s.devices[s.device], samples)
	if err != nil {
		return nil, err
	}
	s.device++
	if s.device == len(s.devices) {
		s.device, s.minute = 0, s.minute+1
	}
	return bytes.SplitAfter(bytes.TrimSuffix(body, []byte("\n")), []byte("\n")), nil
}

// next is a request body of whole readings no larger than budget, and how many it holds.
func (s *catchUpSource) next(budget int) ([]byte, int, error) {
	var body bytes.Buffer
	readings := 0
	for {
		if len(s.pending) == 0 {
			lines, err := s.nextLines()
			if err != nil {
				return nil, 0, err
			}
			s.pending = lines
		}
		line := s.pending[0]
		if len(line) > 0 && line[len(line)-1] != '\n' {
			line = append(line, '\n')
		}
		if body.Len()+len(line) > budget {
			return body.Bytes(), readings, nil
		}
		body.Write(line)
		readings++
		s.pending = s.pending[1:]
	}
}

// liveSecond is one second of live readings from whole machines, at least rate of them.
func liveSecond(enc *encoder, rate int, now time.Time) ([]byte, int, error) {
	var body bytes.Buffer
	readings := 0
	tenant := uuid.New()
	for readings < rate {
		samples := make([]telemetry.Sample, len(vitals))
		for i, dim := range vitals {
			samples[i] = telemetry.Sample{Name: catchUpMetric, Value: float64(i), TS: now, Labels: map[string]string{"dim": dim}}
		}
		lines, err := enc.lines(tenant, uuid.New(), samples)
		if err != nil {
			return nil, 0, err
		}
		body.Write(lines)
		readings += len(samples)
	}
	return body.Bytes(), readings, nil
}
