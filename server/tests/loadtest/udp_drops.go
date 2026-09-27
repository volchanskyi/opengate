package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The datagrams each end's kernel dropped because a socket's receive buffer
// was full.
//
// A machine speaks QUIC over UDP, and a datagram that arrives at a full buffer
// is dropped by the kernel before either process sees it. Neither end logs the
// loss: the machine retransmits, the handshake stalls, and a rung that ran out
// of buffer reads as a rung where the server was slow. The kernel counts the
// drops per network namespace, so each end's count is read from the namespace
// that end runs in, and a phase carries the difference across itself.

// ownNetworkCounters is this process's own network namespace's page.
const ownNetworkCounters = "/proc/self/net/snmp"

// receiveBufferErrorsColumn is the column that counts a datagram dropped for a
// full receive buffer.
const receiveBufferErrorsColumn = "RcvbufErrors"

// NetworkDrops is how a phase reads each end's drops. Either reader may be
// absent: the target's counters are only reachable where it shares the runner's
// kernel.
type NetworkDrops struct {
	ReadGenerator func() (int64, bool)
	ReadTarget    func() (int64, bool)
}

// Bracket takes the opening counts and returns what closes them.
func (n NetworkDrops) Bracket() func() (generator, target *int64) {
	generatorBefore, generatorOpened := readCounter(n.ReadGenerator)
	targetBefore, targetOpened := readCounter(n.ReadTarget)
	return func() (*int64, *int64) {
		return droppedSince(n.ReadGenerator, generatorBefore, generatorOpened),
			droppedSince(n.ReadTarget, targetBefore, targetOpened)
	}
}

func readCounter(read func() (int64, bool)) (int64, bool) {
	if read == nil {
		return 0, false
	}
	return read()
}

// droppedSince is the drops since the opening count. A counter that went
// backwards belongs to a network stack that was replaced inside the phase, and
// the difference would describe two of them.
func droppedSince(read func() (int64, bool), before int64, opened bool) *int64 {
	after, closed := readCounter(read)
	if !opened || !closed || after < before {
		return nil
	}
	dropped := after - before
	return &dropped
}

// NewNetworkDrops reads this process's own namespace for the generator's drops,
// and the target's page where the run was told where it is.
func NewNetworkDrops(targetCounters string) NetworkDrops {
	drops := NetworkDrops{
		ReadGenerator: func() (int64, bool) { return readReceiveBufferErrors(ownNetworkCounters) },
	}
	if targetCounters != "" {
		drops.ReadTarget = func() (int64, bool) { return readReceiveBufferErrors(targetCounters) }
	}
	return drops
}

func readReceiveBufferErrors(path string) (int64, bool) {
	page, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, false
	}
	return ParseUDPReceiveBufferErrors(string(page))
}

// ParseUDPReceiveBufferErrors reads the receive-buffer drops off a kernel
// counters page. The Udp section is a header line and a value line under the
// same prefix, and the column is found by its name in the header.
func ParseUDPReceiveBufferErrors(page string) (int64, bool) {
	var header []string
	for line := range strings.SplitSeq(page, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Udp:" {
			continue
		}
		if header == nil {
			header = fields
			continue
		}
		for i, name := range header {
			if name != receiveBufferErrorsColumn || i >= len(fields) {
				continue
			}
			value, err := strconv.ParseInt(fields[i], 10, 64)
			return value, err == nil
		}
		return 0, false
	}
	return 0, false
}
