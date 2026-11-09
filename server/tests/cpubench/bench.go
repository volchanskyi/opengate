package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"io"
	"runtime"
	"time"
)

// work is the fixed input every round hashes and compresses: repeating text, as logs and metric
// lines are.
var work = bytes.Repeat([]byte("opengate_edge_metric_avg{dim=\"cpu.total\"} 42.5 1700000000000\n"), 1024)

// Result is one benchmark run: the rounds of fixed work done, over how long, on what.
type Result struct {
	Rounds          int     `json:"rounds"`
	Seconds         float64 `json:"seconds"`
	RoundsPerSecond float64 `json:"rounds_per_second"`
	Arch            string  `json:"arch"`
	Go              string  `json:"go"`
}

// round hashes and compresses the fixed input once and returns the digest of the compressed bytes.
func round() [sha256.Size]byte {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write(work)
	_ = zw.Close()
	_ = sha256.Sum256(work)
	return sha256.Sum256(compressed.Bytes())
}

// run repeats rounds until window has passed.
func run(window time.Duration) Result {
	started := time.Now()
	rounds := 0
	for time.Since(started) < window {
		_ = round()
		rounds++
	}
	seconds := time.Since(started).Seconds()
	return Result{Rounds: rounds, Seconds: seconds, RoundsPerSecond: float64(rounds) / seconds,
		Arch: runtime.GOARCH, Go: runtime.Version()}
}

func write(w io.Writer, result Result) error {
	return json.NewEncoder(w).Encode(result)
}
