package app

import (
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// defaultInternalListen is where the cluster-only listener binds when the
// caller names no address. It is adjacent to the public port so a reader of a
// pod spec can see at a glance that the process serves two doors.
const defaultInternalListen = ":8081"

// internalReadHeaderTimeout bounds how long a client may take to send its
// request headers. The listener is unauthenticated by design — nothing but the
// cluster can route to it — so the one thing it must not offer is a socket held
// open for free.
const internalReadHeaderTimeout = 10 * time.Second

// newInternalServer builds the listener only the cluster may reach: the
// Prometheus exposition a scraper reads, and the profiler an operator attaches
// to a pod that is misbehaving.
//
// It is a second listener rather than a path carve-out because there is nowhere
// to carve. The process serves its API, its websockets and the SPA from one
// router behind one catch-all ingress rule, so any path mounted on it is
// published; and the ingress controller runs with snippet annotations off, so
// the edge cannot deny a path either. A separate port is the only boundary that
// exists in both directions: the Service publishes the port a scraper needs and
// the ingress publishes neither the port nor the paths.
//
// The registry may be nil — a caller that assembled the product without
// instrumentation still gets the profiler, which needs nothing wired.
func newInternalServer(addr string, registry *prometheus.Registry) *http.Server {
	if addr == "" {
		addr = defaultInternalListen
	}

	mux := http.NewServeMux()
	if registry != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	}

	// Registered by hand rather than by importing for the side effect: the
	// package's init installs these on http.DefaultServeMux, which is not the
	// mux this process serves and is reachable from anywhere in the binary.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: internalReadHeaderTimeout,
		// No WriteTimeout: a CPU profile streams for the number of seconds it
		// was asked for, and a deadline shorter than that truncates the very
		// artefact the operator attached to collect.
		IdleTimeout: 120 * time.Second,
	}
}
