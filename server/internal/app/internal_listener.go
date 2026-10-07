package app

import (
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// defaultInternalListen is where the cluster-only listener binds when no address is given.
const defaultInternalListen = ":8081"

// internalReadHeaderTimeout bounds header reads on the unauthenticated cluster-only listener.
const internalReadHeaderTimeout = 10 * time.Second

// newInternalServer builds the cluster-only listener for metrics and the profiler.
// A separate port is the boundary, since the public router exposes every path mounted on it.
func newInternalServer(addr string, registry *prometheus.Registry) *http.Server {
	if addr == "" {
		addr = defaultInternalListen
	}

	mux := http.NewServeMux()
	if registry != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	}

	// The pprof package's init registers on http.DefaultServeMux, so the handlers are mounted here.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: internalReadHeaderTimeout,
		// No WriteTimeout, since a CPU profile streams for as long as it was asked for.
		IdleTimeout: 120 * time.Second,
	}
}
