package protocol

// Error texts an agent answers a request with when it does not run it; the server reads them.
const (
	// RequestBusy answers a request arriving while another of its kind still runs.
	RequestBusy = "busy"
	// RequestTimedOut answers a request the agent stopped at its own deadline.
	RequestTimedOut = "timed out"
)
