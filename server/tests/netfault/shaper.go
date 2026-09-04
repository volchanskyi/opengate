package main

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// readBufferBytes is the largest a datagram can be, so a read that fills it
	// exactly is the one reading that cannot be told apart from a truncation.
	readBufferBytes = 64 * 1024

	// mappingIdleExpiry is how long a machine's server-facing socket is held
	// after its last datagram. It has to outlive the longest dark window the
	// drill runs: a mapping that expired mid-outage would move the machine to a
	// new server-facing address at restore, turning the outage scenario into
	// the re-addressing one without saying so.
	mappingIdleExpiry = 600 * time.Second

	// reapInterval is how often idle mappings are looked for. Nothing depends
	// on the precision — the window it enforces is ten minutes wide.
	reapInterval = 10 * time.Second
)

// Config is what a shaper needs to stand up.
type Config struct {
	// Listen is the machine-facing address. The drill points the server's
	// certificate name at it with a hostAliases entry, so the machines dial the
	// name on the certificate and arrive here.
	Listen string
	// ServerAddr is the real server's QUIC address.
	ServerAddr string
	// Seed drives every impairment's generator and is recorded in the evidence,
	// so a night can be compared against the one before it.
	Seed uint64
	// IdleExpiry is how long a machine's mapping survives silence.
	IdleExpiry time.Duration
}

// DirectionCounters is what the shaper did with the datagrams travelling one
// way. In is what arrived, Out is what was forwarded, and Dropped is what the
// impairment discarded — three numbers rather than two, because a scenario
// whose drop count does not match its instruction did not run, and In minus Out
// cannot say so while a datagram is still waiting on a delay.
type DirectionCounters struct {
	In      int64 `json:"in"`
	Out     int64 `json:"out"`
	Dropped int64 `json:"dropped"`
}

// Counters is everything the shaper knows about itself, which is what the
// runner reads at every phase boundary and what the evidence bundle keeps.
type Counters struct {
	ToServer  DirectionCounters `json:"to_server"`
	ToMachine DirectionCounters `json:"to_machine"`
	Machines  int               `json:"machines"`
	Rebinds   int64             `json:"rebinds"`
	Seed      uint64            `json:"seed"`
	Profile   Profile           `json:"profile"`
}

// directionTally is the live form of DirectionCounters.
type directionTally struct {
	in      atomic.Int64
	out     atomic.Int64
	dropped atomic.Int64
}

func (t *directionTally) snapshot() DirectionCounters {
	return DirectionCounters{In: t.in.Load(), Out: t.out.Load(), Dropped: t.dropped.Load()}
}

// mapping is one machine's path through the shaper: the address it dials from,
// and the server-facing socket its traffic leaves by. One socket per machine,
// so the server sees a distinct source per machine and every reply routes back
// to the machine that asked — the shape any address translator has.
type mapping struct {
	machine *net.UDPAddr
	conn    *net.UDPConn
}

// Shaper forwards datagrams between machines and the server, impairing them on
// the way through.
type Shaper struct {
	listener   *net.UDPConn
	serverAddr *net.UDPAddr
	impairer   *Impairer
	seed       uint64
	idleExpiry time.Duration

	toServer  directionTally
	toMachine directionTally
	rebinds   atomic.Int64

	mu       sync.Mutex
	mappings map[string]*mapping
	seen     *mappingTable

	closeOnce sync.Once
	closed    chan struct{}
	// inFlight holds the datagrams waiting out a delay, so Close does not
	// return while a timer is still about to write to a socket it closed.
	inFlight sync.WaitGroup
}

// NewShaper binds the machine-facing socket and resolves the server's address.
func NewShaper(cfg Config) (*Shaper, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", cfg.ServerAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve the server address %q: %w", cfg.ServerAddr, err)
	}
	listenAddr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("resolve the listen address %q: %w", cfg.Listen, err)
	}
	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %q: %w", cfg.Listen, err)
	}

	expiry := cfg.IdleExpiry
	if expiry <= 0 {
		expiry = mappingIdleExpiry
	}
	s := &Shaper{
		listener:   listener,
		serverAddr: serverAddr,
		impairer:   NewImpairer(cfg.Seed),
		seed:       cfg.Seed,
		idleExpiry: expiry,
		mappings:   map[string]*mapping{},
		seen:       newMappingTable(expiry),
		closed:     make(chan struct{}),
	}
	go s.reap()
	return s, nil
}

// ListenAddr is the address machines dial. It does not move when the shaper
// re-addresses: the re-addressing scenario is about the path the server sees,
// and moving both ends at once would be two changes rather than one.
func (s *Shaper) ListenAddr() *net.UDPAddr { return s.listener.LocalAddr().(*net.UDPAddr) }

// SetProfile puts an impairment in force, refusing one the shaper cannot run.
func (s *Shaper) SetProfile(p Profile) error { return s.impairer.Set(p) }

// Machines is how many machines currently hold a mapping.
func (s *Shaper) Machines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mappings)
}

// Counters is the shaper's account of itself.
func (s *Shaper) Counters() Counters {
	return Counters{
		ToServer:  s.toServer.snapshot(),
		ToMachine: s.toMachine.snapshot(),
		Machines:  s.Machines(),
		Rebinds:   s.rebinds.Load(),
		Seed:      s.seed,
		Profile:   s.impairer.Profile(),
	}
}

// Serve carries traffic until the shaper is closed.
func (s *Shaper) Serve() {
	buf := make([]byte, readBufferBytes)
	for {
		n, from, err := s.listener.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if err := checkRead(n, len(buf)); err != nil {
			// A truncated datagram would read downstream as corruption the
			// drill never asked for, and the run would report a finding about
			// the product that belongs to the instrument. There is nothing to
			// recover: the rest of that datagram is gone.
			panic(err)
		}
		s.toServer.in.Add(1)

		payload := make([]byte, n)
		copy(payload, buf[:n])
		s.handleFromMachine(from, payload)
	}
}

func (s *Shaper) handleFromMachine(from *net.UDPAddr, payload []byte) {
	verdict := s.impairer.Decide(ToServer, len(payload), time.Now())
	if verdict.Drop {
		s.toServer.dropped.Add(1)
		return
	}
	m, err := s.mappingFor(from)
	if err != nil {
		// A machine whose path could not be opened is not a machine the drill
		// impaired, so it is counted as dropped rather than forwarded and the
		// scenario's own counter check is what notices.
		s.toServer.dropped.Add(1)
		return
	}
	s.after(verdict.Delay, func() {
		if _, err := m.conn.Write(payload); err == nil {
			s.toServer.out.Add(1)
		}
	})
}

// mappingFor returns the machine's path, opening one the first time the machine
// speaks. A machine that keeps talking keeps its mapping: minting a fresh one
// per datagram would give the server a new source address per packet, which is
// a re-addressing scenario nobody asked for.
func (s *Shaper) mappingFor(from *net.UDPAddr) (*mapping, error) {
	key := from.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen.touch(key, time.Now())
	if m, ok := s.mappings[key]; ok {
		return m, nil
	}

	conn, err := net.DialUDP("udp", nil, s.serverAddr)
	if err != nil {
		return nil, fmt.Errorf("open a server-facing socket for %s: %w", key, err)
	}
	m := &mapping{machine: cloneAddr(from), conn: conn}
	s.mappings[key] = m
	go s.readFromServer(m, conn)
	return m, nil
}

// readFromServer carries one machine's replies back to it. It reads from the
// socket it was handed rather than from the mapping, so a re-addressing that
// replaces the mapping's socket does not leave two goroutines reading the same
// one.
func (s *Shaper) readFromServer(m *mapping, conn *net.UDPConn) {
	buf := make([]byte, readBufferBytes)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		if err := checkRead(n, len(buf)); err != nil {
			panic(err)
		}
		s.toMachine.in.Add(1)

		verdict := s.impairer.Decide(ToMachine, n, time.Now())
		if verdict.Drop {
			s.toMachine.dropped.Add(1)
			continue
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])
		s.after(verdict.Delay, func() {
			if _, err := s.listener.WriteToUDP(payload, m.machine); err == nil {
				s.toMachine.out.Add(1)
			}
		})
	}
}

// after runs the write, either now or once the link has carried what is queued
// ahead of it. The immediate case is not handed to a timer: the drill's clean
// phases are the majority of its traffic, and every one of them would otherwise
// pay for a goroutine it does not need.
func (s *Shaper) after(delay time.Duration, write func()) {
	if delay <= 0 {
		write()
		return
	}
	s.inFlight.Add(1)
	time.AfterFunc(delay, func() {
		defer s.inFlight.Done()
		select {
		case <-s.closed:
			return
		default:
			write()
		}
	})
}

// Rebind moves every server-facing socket to a new local port, mid-connection,
// leaving the machines' own addresses alone. This is a customer's router
// rebooting at 3 a.m. and handing every machine a new public address.
func (s *Shaper) Rebind() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var problems []error
	for key, m := range s.mappings {
		conn, err := net.DialUDP("udp", nil, s.serverAddr)
		if err != nil {
			problems = append(problems, fmt.Errorf("re-address %s: %w", key, err))
			continue
		}
		// Closing the previous socket is what ends the goroutine reading it.
		_ = m.conn.Close()
		m.conn = conn
		go s.readFromServer(m, conn)
	}
	if err := errors.Join(problems...); err != nil {
		return err
	}
	s.rebinds.Add(1)
	return nil
}

// reap closes the mappings of machines that have gone quiet for longer than the
// idle window, so a run that stands up and tears down many machines does not
// hold a socket for every one of them for the life of the process.
func (s *Shaper) reap() {
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for _, key := range s.seen.expired(now) {
				if m, ok := s.mappings[key]; ok {
					_ = m.conn.Close()
					delete(s.mappings, key)
				}
				s.seen.forget(key)
			}
			s.mu.Unlock()
		}
	}
}

// Close stops the shaper and releases every socket it holds.
func (s *Shaper) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.listener.Close()
		s.mu.Lock()
		for key, m := range s.mappings {
			_ = m.conn.Close()
			delete(s.mappings, key)
		}
		s.mu.Unlock()
		s.inFlight.Wait()
	})
}

// checkRead reports a datagram that did not arrive whole.
//
// Go's ReadFromUDP truncates silently, so a read that exactly fills the buffer
// is the one case where what was forwarded is not what was sent. At 64 KiB no
// real datagram reaches it, which is precisely why a read that does is a defect
// in the instrument rather than a large message.
func checkRead(n, bufferLen int) error {
	if n >= bufferLen {
		return fmt.Errorf("a datagram filled the whole %d-byte read buffer, so it was truncated and what would be forwarded is not what was sent", bufferLen)
	}
	return nil
}

// cloneAddr copies the address a read reported, because the read reuses its own.
func cloneAddr(a *net.UDPAddr) *net.UDPAddr {
	out := &net.UDPAddr{Port: a.Port, Zone: a.Zone}
	out.IP = append(out.IP, a.IP...)
	return out
}

// mappingTable is the idle-expiry policy on its own: which machines have gone
// quiet for long enough that their path can be released. It is separated from
// the sockets so the window can be asserted by an ordinary test that states the
// passage of time rather than waiting out ten minutes of it.
type mappingTable struct {
	expiry   time.Duration
	lastSeen map[string]time.Time
}

func newMappingTable(expiry time.Duration) *mappingTable {
	return &mappingTable{expiry: expiry, lastSeen: map[string]time.Time{}}
}

// touch records that this machine spoke at now.
func (t *mappingTable) touch(key string, now time.Time) { t.lastSeen[key] = now }

// expired names the machines that have been silent past the window, in a fixed
// order so a run that reaps several is reproducible.
func (t *mappingTable) expired(now time.Time) []string {
	var out []string
	for key, seen := range t.lastSeen {
		if now.Sub(seen) > t.expiry {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// forget drops a machine the caller has released.
func (t *mappingTable) forget(key string) { delete(t.lastSeen, key) }
