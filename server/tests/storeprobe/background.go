package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// backgroundConfig is the load production's store carries beside machine readings: its scrape.
type backgroundConfig struct {
	store  string
	series int
	every  time.Duration
}

// backgroundBody is one interval of the background series, each once, stamped at now.
func backgroundBody(series int, now time.Time) []byte {
	var body bytes.Buffer
	stamp := now.UnixMilli()
	for i := range series {
		_, _ = fmt.Fprintf(&body, "opengate_perf_background{job=\"scrape\",instance=\"i%d\",series=\"s%d\"} %d %d\n",
			i%40, i, i%97, stamp)
	}
	return body.Bytes()
}

// runBackground writes every series each interval until ctx ends; a refused write is retried at
// the next interval.
func runBackground(ctx context.Context, cfg backgroundConfig) error {
	im := importer{url: cfg.store + "/api/v1/import/prometheus", client: &http.Client{Timeout: cfg.every}}
	tick := time.NewTicker(cfg.every)
	defer tick.Stop()
	for {
		_ = im.send(ctx, backgroundBody(cfg.series, time.Now()))
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
