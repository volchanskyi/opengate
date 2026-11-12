package main

// PhaseRegistration reads the server's registration timing at a phase's two ends, so each phase
// states the sign-ins of that phase alone.
type PhaseRegistration struct {
	Read func() (ServerRegistration, bool)
}

// NewPhaseRegistration reads the server's own page; no address leaves every phase unread.
func NewPhaseRegistration(metricsURL string) PhaseRegistration {
	if metricsURL == "" {
		return PhaseRegistration{}
	}
	return PhaseRegistration{Read: func() (ServerRegistration, bool) {
		reading, err := FetchServerRegistration(metricsURL)
		return reading, err == nil
	}}
}

// Bracket takes the opening reading and returns a closer yielding the phase's median and 95th
// percentile, absent where an end went unread or no machine signed in.
func (r PhaseRegistration) Bracket() func() (p50, p95 *float64) {
	if r.Read == nil {
		return func() (*float64, *float64) { return nil, nil }
	}
	before, opened := r.Read()
	return func() (*float64, *float64) {
		after, closed := r.Read()
		if !opened || !closed {
			return nil, nil
		}
		since := after.Since(before)
		if !since.Measured() {
			return nil, nil
		}
		p50, p95 := since.QuantileMs(0.5), since.QuantileMs(0.95)
		return &p50, &p95
	}
}
