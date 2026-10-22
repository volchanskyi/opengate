package main

import (
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// firstRoomWait is the first pause before reading a busy processor again; each pause doubles.
const firstRoomWait = 15 * time.Second

// longestRoomWait caps each pause between readings of a busy processor.
const longestRoomWait = 5 * time.Minute

// RoomWait is how long a run waits for the shared machine's processor to have room before it
// walks, and the source that spreads its pauses; a zero Limit refuses at the first busy reading.
type RoomWait struct {
	Limit time.Duration
	// Rand spreads the pauses; nil draws them from the system's random source.
	Rand *rand.Rand
}

// pause is a random time between half and all of step, so readings never lock onto a busy spell
// that repeats on a fixed clock.
func (w RoomWait) pause(step time.Duration) time.Duration {
	half := step / 2
	spread := int64(step-half) + 1
	if w.Rand == nil {
		n, err := crand.Int(crand.Reader, big.NewInt(spread))
		if err != nil {
			return step
		}
		return half + time.Duration(n.Int64())
	}
	return half + time.Duration(w.Rand.Int64N(spread))
}

// NoRoomError is a start the shared machine's processor never made room for within the wait.
type NoRoomError struct {
	Ceiling  float64
	Waited   time.Duration
	Readings []float64
}

// Error names how busy the processor stayed, for how long, against which ceiling.
func (e *NoRoomError) Error() string {
	low, high := slices.Min(e.Readings), slices.Max(e.Readings)
	busy := fmt.Sprintf("%.0f%%", low)
	if fmt.Sprintf("%.0f", low) != fmt.Sprintf("%.0f", high) {
		busy = fmt.Sprintf("%.0f–%.0f%%", low, high)
	}
	return fmt.Sprintf("the shared server stayed %s busy for %s, past the %.0f%% the run allows",
		busy, spokenDuration(e.Waited), e.Ceiling)
}

// spokenDuration is a wait in whole minutes, or in seconds when under one.
func spokenDuration(d time.Duration) string {
	switch minutes := int(d / time.Minute); {
	case minutes == 1:
		return "1 minute"
	case minutes > 1:
		return fmt.Sprintf("%d minutes", minutes)
	default:
		return fmt.Sprintf("%d seconds", int(d/time.Second))
	}
}

// waitForRoom returns the first reading that leaves room to start. A busy processor is read again
// after growing pauses until the wait runs out; a node short of memory or disk is refused at once.
func waitForRoom(limits Safety, wait RoomWait, clock Clock, read SafetyReader) (NodeReading, error) {
	reading := read()
	err := CheckRoomToStart(limits, reading)
	if err == nil || wait.Limit <= 0 || CheckRoomToContinue(limits, reading) != nil {
		return reading, err
	}

	began := clock.Now()
	readings := []float64{reading.CPUPercent}
	for step := firstRoomWait; ; step = min(2*step, longestRoomWait) {
		left := wait.Limit - clock.Now().Sub(began)
		if left <= 0 {
			return reading, &NoRoomError{Ceiling: limits.MaxNodeCPUPercent, Waited: clock.Now().Sub(began),
				Readings: readings}
		}
		clock.Sleep(min(wait.pause(step), left))
		reading = read()
		if err = CheckRoomToStart(limits, reading); err == nil {
			return reading, nil
		}
		if CheckRoomToContinue(limits, reading) != nil {
			return reading, err
		}
		readings = append(readings, reading.CPUPercent)
	}
}

// awaitRoom waits for room on the shared machine and returns a reader whose first answer is the
// reading that found it, so the walk's own start check judges that reading.
func awaitRoom(limits Safety, wait RoomWait, clock Clock, read SafetyReader) (SafetyReader, error) {
	found, err := waitForRoom(limits, wait, clock, read)
	if err != nil {
		return nil, err
	}
	answered := false
	return func() NodeReading {
		if !answered {
			answered = true
			return found
		}
		return read()
	}, nil
}

// notStarted is the bundle of a run that never walked: why, against which ceiling, and every
// reading it took while it waited.
type notStarted struct {
	Reason         string    `json:"reason"`
	CeilingPercent float64   `json:"ceiling_percent"`
	WaitedSeconds  float64   `json:"waited_seconds"`
	Readings       []float64 `json:"readings_percent"`
}

// writeNotStartedBundle leaves the refusal where the run's bundle goes, so the night's verdict
// names the busy server.
func writeNotStartedBundle(dir string, refusal *NoRoomError) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("not-started bundle: %w", err)
	}
	raw, err := json.MarshalIndent(map[string]notStarted{"not_started": {
		Reason:         refusal.Error(),
		CeilingPercent: refusal.Ceiling,
		WaitedSeconds:  refusal.Waited.Seconds(),
		Readings:       refusal.Readings,
	}}, "", "  ")
	if err != nil {
		return fmt.Errorf("not-started bundle: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, bundleFileName), append(raw, '\n'), 0o600)
}
