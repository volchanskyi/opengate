package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// inventoryFieldChars is just under the longest name and version the server stores whole, so a
// padded report is made of few rows of full width.
const inventoryFieldChars = 255

// inventoryPayload is an encoded package inventory as close to size bytes as fits under it, its
// rows of full width. One payload is shared by every machine of a call.
func inventoryPayload(size int) []byte {
	codec := &protocol.Codec{}
	build := func(rows int) []byte {
		packages := make([]protocol.DiscoveredPackage, rows)
		for i := range packages {
			name := fmt.Sprintf("lib-%07d-", i)
			packages[i] = protocol.DiscoveredPackage{
				Name:    name + strings.Repeat("x", inventoryFieldChars-len(name)),
				Version: strings.Repeat("9", inventoryFieldChars),
			}
		}
		payload, err := codec.EncodeControl(&protocol.ControlMessage{
			Type: protocol.MsgDiscoveryReport, TS: time.Now().Unix(), Packages: packages,
		})
		if err != nil {
			panic(fmt.Sprintf("encode a padded inventory: %v", err))
		}
		return payload
	}
	one := len(build(1))
	perRow := len(build(2)) - one
	rows := max(1, (size-one)/perRow+1)
	payload := build(rows)
	for len(payload) > size && rows > 1 {
		rows--
		payload = build(rows)
	}
	return payload
}

// inventoryCall is the fleet-wide call for one package inventory from every held machine, as a
// fleet-wide reinstall makes each send one; a machine answers each call once.
type inventoryCall struct {
	mu      sync.Mutex
	epoch   int
	payload []byte
	sent    atomic.Int64
}

// call asks every held machine for one inventory of size bytes.
func (c *inventoryCall) call(size int) {
	payload := inventoryPayload(size)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	c.payload = payload
}

// due is the payload of a call the machine that answered seen has not answered, and its epoch.
func (c *inventoryCall) due(seen int) ([]byte, int) {
	if c == nil {
		return nil, seen
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch == seen {
		return nil, seen
	}
	return c.payload, c.epoch
}

func (c *inventoryCall) noteSent() { c.sent.Add(1) }

// sentCount is how many machines have written an inventory since the run began.
func (c *inventoryCall) sentCount() int64 { return c.sent.Load() }

// InventoryBurst calls a phase's inventory and reads, at its two ends and every 5 s between,
// what the machines sent and the server's count of inventories it stored.
type InventoryBurst struct {
	Call   func(bytes int)
	Sent   func() int64
	Stored func() (int64, bool)
}

// inventoryWatchEvery is how often a phase asks the server how many inventories it has stored.
const inventoryWatchEvery = 5 * time.Second

// inventoryWatch is one phase's inventory: its opening counts, and the first 5 s mark from the
// phase's start at which the server had stored one for each of the phase's machines.
type inventoryWatch struct {
	burst        InventoryBurst
	machines     int64
	startedAt    time.Time
	next         time.Time
	sentBefore   int64
	storedBefore int64
	opened       bool
	allAt        time.Time
}

// Bracket calls for an inventory of size bytes from machines machines, opening at startedAt; a
// phase that asks for none reads none.
func (b InventoryBurst) Bracket(size, machines int, startedAt time.Time) *inventoryWatch {
	if size <= 0 || b.Call == nil || b.Sent == nil {
		return nil
	}
	if b.Stored == nil {
		b.Stored = func() (int64, bool) { return 0, false }
	}
	w := &inventoryWatch{burst: b, machines: int64(machines), startedAt: startedAt,
		next: startedAt.Add(inventoryWatchEvery), sentBefore: b.Sent()}
	w.storedBefore, w.opened = b.Stored()
	b.Call(size)
	return w
}

// watching reports whether the server's count is still worth asking for.
func (w *inventoryWatch) watching() bool {
	return w != nil && w.opened && w.allAt.IsZero()
}

// observe asks the server's count at at, and keeps at as the moment every machine's was stored.
func (w *inventoryWatch) observe(at time.Time) {
	if stored, ok := w.burst.Stored(); ok && stored-w.storedBefore >= w.machines {
		w.allAt = at
	}
}

// sleep waits d on the clock, asking the server's count at every 5 s mark from the phase's start
// until every machine's inventory is stored; a mark already past is asked at once.
func (w *inventoryWatch) sleep(clock Clock, d time.Duration) {
	if !w.watching() {
		clock.Sleep(d)
		return
	}
	until := clock.Now().Add(d)
	for {
		now := clock.Now()
		if !now.Before(until) {
			return
		}
		if w.next.After(until) || !w.watching() {
			clock.Sleep(until.Sub(now))
			return
		}
		if w.next.After(now) {
			clock.Sleep(w.next.Sub(now))
		}
		at := clock.Now()
		w.observe(at)
		for !w.next.After(at) {
			w.next = w.next.Add(inventoryWatchEvery)
		}
	}
}

// close reads what was sent and stored over the phase, and the seconds from its start until every
// machine's inventory was stored, absent where the server's count never reached them.
func (w *inventoryWatch) close() (sent, stored *int64, allSeconds *float64) {
	if w == nil {
		return nil, nil, nil
	}
	sentCount := w.burst.Sent() - w.sentBefore
	storedAfter, closed := w.burst.Stored()
	if !w.opened || !closed {
		return &sentCount, nil, nil
	}
	storedCount := storedAfter - w.storedBefore
	if w.allAt.IsZero() {
		return &sentCount, &storedCount, nil
	}
	seconds := w.allAt.Sub(w.startedAt).Seconds()
	return &sentCount, &storedCount, &seconds
}

// inventoriesStoredMetric is the server's count of package inventories it stored.
const inventoriesStoredMetric = "opengate_inventory_reports_stored_total"

// parseInventoriesStored reads the server's stored-inventory count off its page.
func parseInventoriesStored(page string) (int64, bool) {
	for _, line := range strings.Split(page, "\n") {
		if name, _, value, ok := splitSample(strings.TrimSpace(line)); ok && name == inventoriesStoredMetric {
			return int64(value), true
		}
	}
	return 0, false
}

// NewInventoryBurst answers a phase's call through the fleet's shared call and reads the server's
// count off metricsURL; no address leaves the count unread.
func NewInventoryBurst(call *inventoryCall, metricsURL string) InventoryBurst {
	burst := InventoryBurst{Call: call.call, Sent: call.sentCount}
	if metricsURL != "" {
		burst.Stored = func() (int64, bool) {
			page, err := fetchExpositionPage(metricsURL)
			if err != nil {
				return 0, false
			}
			return parseInventoriesStored(page)
		}
	}
	return burst
}
