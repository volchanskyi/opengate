// Command storeprobe measures what the metrics store takes at production's limits, and writes the
// scrape load production's store carries.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "probe", "probe, quiet or background")
	store := flag.String("store", "http://127.0.0.1:8428", "the metrics store")
	senders := flag.Int("senders", 1, "catch-up senders writing at once")
	duration := flag.Duration("duration", 15*time.Minute, "how long the probe writes")
	devices := flag.Int("devices", 5000, "machines the catch-up history belongs to")
	liveRate := flag.Int("live-rate", 2000, "live readings each second, in one request")
	quiet := flag.Duration("quiet", time.Minute, "the quiet window the quiet mode reads")
	cpuLimit := flag.Float64("store-cpus", 0.5, "the store's processor allowance")
	memoryLimit := flag.Float64("store-memory-bytes", 512<<20, "the store's memory limit")
	series := flag.Int("series", 14400, "background series written each interval")
	every := flag.Duration("every", 15*time.Second, "the background interval, production's scrape")
	out := flag.String("out", "", "file the reading is written to; empty writes standard output")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := probeConfig{store: *store, senders: *senders, duration: *duration, requestBytes: requestBytes,
		liveRate: *liveRate, liveEvery: time.Second, devices: *devices, quiet: *quiet,
		cpuLimit: *cpuLimit, memoryLimit: *memoryLimit, statusTimeout: storeReadTimeout}

	var reading any
	var err error
	switch *mode {
	case "background":
		err = runBackground(ctx, backgroundConfig{store: *store, series: *series, every: *every})
	case "quiet":
		reading, err = readQuiet(ctx, cfg)
	case "probe":
		reading, err = runProbe(ctx, cfg)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "storeprobe: %v\n", err)
		os.Exit(1)
	}
	if reading == nil {
		return
	}
	w := os.Stdout
	if *out != "" {
		if w, err = os.Create(*out); err != nil {
			fmt.Fprintf(os.Stderr, "storeprobe: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = w.Close() }()
	}
	if err := writeJSON(w, reading); err != nil {
		fmt.Fprintf(os.Stderr, "storeprobe: %v\n", err)
		os.Exit(1)
	}
}
