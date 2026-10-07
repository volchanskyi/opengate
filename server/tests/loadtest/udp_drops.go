package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ownNetworkCounters is this process's own network namespace's page.
const ownNetworkCounters = "/proc/self/net/snmp"

// receiveBufferErrorsColumn counts datagrams dropped for a full receive buffer.
const receiveBufferErrorsColumn = "RcvbufErrors"

// NetworkDrops reads each end's receive-buffer drops from the end's own network namespace.
// The target's reader is nil where it shares no kernel with the runner.
type NetworkDrops struct {
	ReadGenerator func() (int64, bool)
	ReadTarget    func() (int64, bool)
}

// Bracket takes the opening counts and returns a closer yielding each end's drops since.
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

// droppedSince returns the drops since the opening count, nil when the counter went backwards.
func droppedSince(read func() (int64, bool), before int64, opened bool) *int64 {
	after, closed := readCounter(read)
	if !opened || !closed || after < before {
		return nil
	}
	dropped := after - before
	return &dropped
}

// NewNetworkDrops reads the generator's drops from this namespace and the target's from its page.
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

// ParseUDPReceiveBufferErrors reads the receive-buffer drops off a kernel counters page.
// The Udp section is a header line and a value line, and the column is found by header name.
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
