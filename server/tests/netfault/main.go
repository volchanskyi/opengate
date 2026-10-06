// Package main implements the in-path datagram forwarder that impairs a machine's link on command;
// the worker node ships no kernel network emulator, so the network drill carries its own.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// controlShutdownGrace bounds how long the control endpoint finishes in-flight requests on stop.
const controlShutdownGrace = 5 * time.Second

func main() {
	os.Exit(run())
}

// run returns the process exit code so every socket it opened is released on every path.
func run() int {
	listen := flag.String("listen", ":9090", "machine-facing UDP address: where the drill's machines dial")
	server := flag.String("server", "", "the real server's QUIC address, which every datagram is forwarded to")
	control := flag.String("control", ":9091", "cluster-internal HTTP address the runner commands scenarios through")
	seed := flag.Uint64("seed", 1, "the seed every impairment draws from, recorded in the evidence so two nights are comparable")
	flag.Parse()

	if *server == "" {
		log.Print("refusing to run: no server address — a forwarder with nowhere to forward to is a blackhole that reports itself as a healthy link")
		return 1
	}

	shaper, err := NewShaper(Config{
		Listen:     *listen,
		ServerAddr: *server,
		Seed:       *seed,
		IdleExpiry: mappingIdleExpiry,
	})
	if err != nil {
		log.Printf("refusing to run: %v", err)
		return 1
	}
	defer shaper.Close()

	controlSrv := &http.Server{
		Addr:              *control,
		Handler:           NewControl(shaper),
		ReadHeaderTimeout: 10 * time.Second,
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		if err := controlSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control endpoint: %v", err)
		}
	}()

	log.Printf("link shaper: machines dial %s, forwarding to %s, commanded on %s, seed %d",
		shaper.ListenAddr(), *server, *control, *seed)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		// The control endpoint outlives the forwarder by the grace so a runner reading the final
		// counters gets an answer.
		shaper.Close()
		ctx, cancel := context.WithTimeout(context.Background(), controlShutdownGrace)
		defer cancel()
		_ = controlSrv.Shutdown(ctx)
	}()

	shaper.Serve()
	<-stopped
	return 0
}
