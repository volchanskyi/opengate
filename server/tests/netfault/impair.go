package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Direction is which way a datagram is travelling, so impairments can differ per direction.
type Direction int

const (
	// ToServer is a datagram the machine sent, on its way to the server.
	ToServer Direction = iota
	// ToMachine is a datagram the server sent, on its way back.
	ToMachine
)

// String is the name this direction carries in the counters and the evidence.
func (d Direction) String() string {
	if d == ToServer {
		return "to_server"
	}
	return "to_machine"
}

// Profile is the impairment in force; the zero value forwards everything untouched.
type Profile struct {
	// Blackhole drops everything both ways and outranks every other field.
	Blackhole bool
	// LossToServer and LossToMachine are the fraction of datagrams discarded per direction.
	LossToServer  float64
	LossToMachine float64
	// DelayEachWay holds every datagram for this long before forwarding it, in both directions.
	DelayEachWay time.Duration
	// RateBitsPerSec is the shared uplink toward the server carrying every machine's traffic;
	// zero leaves the rate unshaped.
	RateBitsPerSec int64
	// MaxQueue is how long a backlog the link holds before tail-dropping; required with a rate.
	MaxQueue time.Duration
}

// profileWire is the JSON form of a Profile, with durations in milliseconds.
type profileWire struct {
	Blackhole      bool    `json:"blackhole"`
	LossToServer   float64 `json:"loss_to_server"`
	LossToMachine  float64 `json:"loss_to_machine"`
	DelayEachWayMS int64   `json:"delay_each_way_ms"`
	RateBitsPerSec int64   `json:"rate_bits_per_sec"`
	MaxQueueMS     int64   `json:"max_queue_ms"`
}

// MarshalJSON writes the profile in the units the runner speaks.
func (p Profile) MarshalJSON() ([]byte, error) {
	return json.Marshal(profileWire{
		Blackhole:      p.Blackhole,
		LossToServer:   p.LossToServer,
		LossToMachine:  p.LossToMachine,
		DelayEachWayMS: p.DelayEachWay.Milliseconds(),
		RateBitsPerSec: p.RateBitsPerSec,
		MaxQueueMS:     p.MaxQueue.Milliseconds(),
	})
}

// UnmarshalJSON reads a profile back without validating it; Validate rejects impossible ones.
func (p *Profile) UnmarshalJSON(raw []byte) error {
	var wire profileWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	*p = Profile{
		Blackhole:      wire.Blackhole,
		LossToServer:   wire.LossToServer,
		LossToMachine:  wire.LossToMachine,
		DelayEachWay:   time.Duration(wire.DelayEachWayMS) * time.Millisecond,
		RateBitsPerSec: wire.RateBitsPerSec,
		MaxQueue:       time.Duration(wire.MaxQueueMS) * time.Millisecond,
	}
	return nil
}

// Validate reports why this profile cannot run as an impairment, so a mistyped scenario fails
// where it was typed.
func (p Profile) Validate() error {
	var problems []error
	if p.LossToServer < 0 || p.LossToServer > 1 {
		problems = append(problems, fmt.Errorf("loss toward the server is %v, and a fraction of datagrams is between 0 and 1", p.LossToServer))
	}
	if p.LossToMachine < 0 || p.LossToMachine > 1 {
		problems = append(problems, fmt.Errorf("loss toward the machine is %v, and a fraction of datagrams is between 0 and 1", p.LossToMachine))
	}
	if p.DelayEachWay < 0 {
		problems = append(problems, fmt.Errorf("the delay is %v, and a link does not deliver a datagram before it was sent", p.DelayEachWay))
	}
	if p.RateBitsPerSec < 0 {
		problems = append(problems, fmt.Errorf("the rate is %d bits per second, and a link does not carry a negative number of them", p.RateBitsPerSec))
	}
	if p.MaxQueue < 0 {
		problems = append(problems, fmt.Errorf("the queue depth is %v, and a router does not hold a datagram for a negative time", p.MaxQueue))
	}
	if p.RateBitsPerSec > 0 && p.MaxQueue <= 0 {
		problems = append(problems, errors.New("a rate was set with no queue to hold what does not fit in it, which is not a link — state the depth the router buffers to"))
	}
	return errors.Join(problems...)
}

// Verdict is what the shaper does with one datagram.
type Verdict struct {
	// Drop discards the datagram without sending or retrying it.
	Drop bool
	// Delay holds it for this long first. Zero forwards it immediately.
	Delay time.Duration
}

// Impairer decides each datagram's fate under the profile in force, taking the time as an
// argument and drawing from a seeded generator so the same seed drops the same datagrams.
type Impairer struct {
	mu      sync.Mutex
	profile Profile
	seed    uint64

	// One generator per direction keeps each direction's drops independent of the other's arrivals.
	random map[Direction]*stream

	// linkFree is when the shared uplink finishes carrying everything already queued on it.
	linkFree time.Time
}

// NewImpairer starts an impairer passing everything through, drawing from the
// given seed.
func NewImpairer(seed uint64) *Impairer {
	return &Impairer{seed: seed, random: newGenerators(seed)}
}

// Each direction's generator starts from the run seed xor its own constant.
const (
	streamToServer  uint64 = 0x9E3779B97F4A7C15
	streamToMachine uint64 = 0xBF58476D1CE4E5B9
)

func newGenerators(seed uint64) map[Direction]*stream {
	return map[Direction]*stream{
		ToServer:  &stream{state: seed ^ streamToServer},
		ToMachine: &stream{state: seed ^ streamToMachine},
	}
}

// stream is the bit source every impairment draws from; its sequence is fixed so a seed always
// drops the same datagrams. It decides test-link loss only and must never source secrets.
type stream struct {
	state uint64
}

// next advances the stream and returns the next value in it.
func (s *stream) next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// fraction is the next value in [0, 1), built from the top 53 bits a float64 carries exactly.
func (s *stream) fraction() float64 {
	return float64(s.next()>>11) / (1 << 53)
}

// Set puts a validated profile in force; it restarts the generators and empties the uplink queue.
func (i *Impairer) Set(p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.profile = p
	i.random = newGenerators(i.seed)
	i.linkFree = time.Time{}
	return nil
}

// Profile is the impairment currently in force.
func (i *Impairer) Profile() Profile {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.profile
}

// Decide is what happens to one datagram of size bytes travelling in dir at now.
func (i *Impairer) Decide(dir Direction, bytes int, now time.Time) Verdict {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.profile.Blackhole {
		return Verdict{Drop: true}
	}
	if loss := i.lossFor(dir); loss > 0 && i.random[dir].fraction() < loss {
		return Verdict{Drop: true}
	}

	delay := i.profile.DelayEachWay
	// The rate shapes only the uplink toward the server.
	if dir == ToServer && i.profile.RateBitsPerSec > 0 {
		wait, ok := i.queueOnLink(bytes, now)
		if !ok {
			return Verdict{Drop: true}
		}
		delay += wait
	}
	return Verdict{Delay: delay}
}

func (i *Impairer) lossFor(dir Direction) float64 {
	if dir == ToServer {
		return i.profile.LossToServer
	}
	return i.profile.LossToMachine
}

// queueOnLink puts one datagram on the shared uplink and returns its wait, or false when the
// queue is deeper than MaxQueue.
func (i *Impairer) queueOnLink(bytes int, now time.Time) (time.Duration, bool) {
	start := now
	if i.linkFree.After(start) {
		start = i.linkFree
	}
	if queued := start.Sub(now); queued > i.profile.MaxQueue {
		return 0, false
	}
	carry := time.Duration(float64(bytes) * 8 / float64(i.profile.RateBitsPerSec) * float64(time.Second))
	i.linkFree = start.Add(carry)
	return i.linkFree.Sub(now), true
}
