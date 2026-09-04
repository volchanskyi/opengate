// Package main implements the link shaper the nightly network drill runs its
// machines through: an in-path datagram forwarder that sits between a machine
// and the server and impairs the link on command.
//
// It exists because the worker node has no kernel network emulator — the module
// is configured but not shipped in the node image, and it is absent from the
// module index, so nothing in a container can supply it. Every off-the-shelf
// impairment tool is a wrapper around that module, so the drill carries its own
// forwarder instead. Nothing about it is privileged: it is an ordinary
// unprivileged pod holding two sockets.
//
// Usage:
//
//	go run ./tests/netfault/ -listen=:9090 -server=opengate-staging-server:9090 -control=:9091 -seed=1
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

// controlShutdownGrace bounds how long the control endpoint is given to finish
// what it is answering when the shaper is asked to stop.
const controlShutdownGrace = 5 * time.Second

func main() {
	os.Exit(run())
}

// run is the whole shaper, returning the code the process exits with. It is
// separated from main so every socket it opened is released on every path,
// including the ones that end badly.
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
		// The forwarder stops first. The control endpoint outlives it by the
		// grace below so a runner reading the final counters gets an answer
		// rather than a refused connection, which it would have to read as an
		// inconclusive scenario.
		shaper.Close()
		ctx, cancel := context.WithTimeout(context.Background(), controlShutdownGrace)
		defer cancel()
		_ = controlSrv.Shutdown(ctx)
	}()

	shaper.Serve()
	<-stopped
	return 0
}
