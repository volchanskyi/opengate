package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Direction is which way a datagram is travelling. Every impairment states one,
// because a customer's connection degrades asymmetrically — the upload is the
// half that fails — and a symmetric fault hides which side the recovery
// machinery is coping with.
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

// Profile is the impairment in force. The zero value forwards everything
// untouched, which is what the drill's baseline and recovery phases ask for.
//
// One struct rather than a command per impairment, because a scenario is a
// combination: the thin-uplink recovery is a rate limit with nothing else on
// it, and the satellite link is a delay with nothing else on it, but neither is
// a mode the shaper enters — they are the same forwarder with different numbers.
type Profile struct {
	// Blackhole drops everything both ways. It outranks every other field: a
	// scenario that asks for darkness gets darkness, and nothing queues behind
	// it to be released the moment it lifts.
	Blackhole bool
	// LossToServer and LossToMachine are the fraction of datagrams discarded in
	// each direction, drawn from a seeded generator.
	LossToServer  float64
	LossToMachine float64
	// DelayEachWay holds every datagram for this long before forwarding it, in
	// both directions — a satellite hop, which is symmetric.
	DelayEachWay time.Duration
	// RateBitsPerSec is the site's shared uplink: one link toward the server
	// carrying every machine's traffic. Zero leaves the rate alone.
	RateBitsPerSec int64
	// MaxQueue is how much the link will hold before it starts dropping. A real
	// router has a finite buffer and tail-drops past the end of it; without a
	// bound the shaper would accumulate an unbounded backlog and report a
	// latency no customer's connection would ever produce. Required whenever a
	// rate is set, because a rate without one is not a link.
	MaxQueue time.Duration
}

// profileWire is how a Profile crosses the control endpoint and lands in the
// evidence bundle. Durations travel as milliseconds: a scenario is written by a
// shell script, and Go's duration encoding is nanoseconds, which is a decimal
// point a runner would eventually get wrong.
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

// UnmarshalJSON reads one back. It does not validate: a caller decides what to
// do with an impossible profile, and Validate is where that decision is made.
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

// Validate reports why this profile is not an impairment the shaper can run.
//
// The refusal is here rather than in the forwarder because a scenario that
// mistyped its instruction must fail where it was typed. A shaper that quietly
// reduced an impossible number to one it could run would produce a measurement
// of some impairment nobody named.
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
	// Drop discards it. The link lost it, so nothing is sent and nothing is
	// retried — that is the transport's business, which is what the drill is
	// there to watch.
	Drop bool
	// Delay holds it for this long first. Zero forwards it immediately.
	Delay time.Duration
}

// Impairer decides each datagram's fate under the profile currently in force.
//
// It takes the time as an argument rather than reading a clock, and draws from
// a generator seeded at startup rather than from the global one. Both are what
// make the impairments testable in process with no cluster and no sleeping, and
// the seed is what makes two nights comparable: the same seed, given the same
// datagrams, drops the same ones.
type Impairer struct {
	mu      sync.Mutex
	profile Profile
	seed    uint64

	// One generator per direction, so a datagram arriving one way cannot change
	// which datagram is dropped the other way. Sharing one would make every
	// measurement depend on the interleaving of two independent arrival
	// streams, which is the one thing a drill cannot reproduce.
	random map[Direction]*stream

	// linkFree is when the shared uplink finishes carrying everything already
	// queued on it. A datagram arriving before then waits for the link, which
	// is what one link shared by a site's machines does.
	linkFree time.Time
}

// NewImpairer starts an impairer passing everything through, drawing from the
// given seed.
func NewImpairer(seed uint64) *Impairer {
	return &Impairer{seed: seed, random: newGenerators(seed)}
}

// The two directions are seeded from the same run seed, separated by a constant
// of their own, so one seed reproduces the whole night while neither
// direction's draws depend on the other's.
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

// stream is the bit-source every impairment draws from.
//
// It is written out here rather than taken from the standard library because
// the drill's whole reproducibility claim rests on the sequence being stable:
// two nights with the same seed have to drop the same datagrams, or a trend
// compares a run against a differently-unlucky one and calls the difference a
// regression. A library's output sequence is not a compatibility promise, so a
// toolchain upgrade could change which datagrams a seed drops with nothing
// anywhere saying so. Nine lines of arithmetic that cannot change is the
// cheaper half of that trade.
//
// It is not, and must never be used as, a source of secrets. Nothing here
// guards anything: it decides which datagrams a test link discards.
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

// fraction is the next value as a number in [0, 1), which is the form every
// loss comparison wants. The top 53 bits are the ones a float64 can carry
// without rounding.
func (s *stream) fraction() float64 {
	return float64(s.next()>>11) / (1 << 53)
}

// Set puts a profile in force, reporting why it will not if it cannot.
//
// A new profile is a new link: the generators restart and the uplink is empty.
// Carrying a queue across a phase boundary would release the previous phase's
// backlog into the phase that follows it, and the measurement would be of the
// phase before.
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
	// The rate is the site's uplink, so it shapes what the machines send and
	// leaves what they receive alone.
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

// queueOnLink puts one datagram on the shared uplink and reports how long it
// waits, or that the link's buffer is full and it was dropped.
//
// The link is modelled as it behaves: it carries one datagram at a time at the
// rate it runs at, so a datagram arriving while it is busy starts when the ones
// ahead of it finish. What is already queued is the wait a new arrival is
// quoted, and past the buffer's depth the router drops rather than queues.
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
