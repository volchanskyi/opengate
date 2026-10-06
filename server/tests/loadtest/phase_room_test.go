package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseReadsTheGeneratorsRoomOverItsOwnWindow(t *testing.T) {
	accounts := &cpuSequence{values: []cgroupCPU{
		{UsageMicros: 5_000_000, RefusedMicros: 1_000_000, Refusals: true},
		{UsageMicros: 35_000_000, RefusedMicros: 7_000_000, Refusals: true},
	}}
	room := GeneratorRoom{ReadCPU: accounts.read, Allowance: 1}

	phase := Phase{Name: "step-16000", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Generator: room})
	require.NoError(t, err)

	require.NotNil(t, result.GeneratorCPUHeadroomPercent)
	require.NotNil(t, result.GeneratorCPURefusedPercent)
	assert.InDelta(t, 50.0, *result.GeneratorCPUHeadroomPercent, 0.01)
	assert.InDelta(t, 10.0, *result.GeneratorCPURefusedPercent, 0.01)
}

func TestAGeneratorWithNoAllowanceHasNoRoomToReadPerPhase(t *testing.T) {
	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{})
	require.NoError(t, err)

	assert.Nil(t, result.GeneratorCPUHeadroomPercent)
	assert.Nil(t, result.GeneratorCPURefusedPercent)
}

func TestAKernelThatCountsNoRefusalsLeavesTheRefusedShareAbsent(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":  "100000 100000\n",
		"cpu.stat": "usage_usec 0\n",
	})
	room := GeneratorRoom{ReadCPU: func() (cgroupCPU, bool) { return readCgroupCPU(files) }, Allowance: 1}

	headroom, refused := room.Bracket()(time.Minute)
	require.NotNil(t, headroom)
	assert.InDelta(t, 100.0, *headroom, 0.01)
	assert.Nil(t, refused)
}

func TestTheGeneratorsFingerprintIsItsAllowanceRatherThanTheBox(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":    "100000 100000\n",
		"cpu.stat":   "usage_usec 0\n",
		"memory.max": "10737418240\n",
	})
	box := Fingerprint{Kind: "generator", CPUs: 4, MemoryBytes: 16_766_414_848}

	shape := withinAllowance(box, files)
	assert.InDelta(t, 1.0, shape.CPUs, 0.001)
	assert.EqualValues(t, 10_737_418_240, shape.MemoryBytes)
}

func TestAGeneratorWithNoAllowanceIsTheBox(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":  "max 100000\n",
		"cpu.stat": "usage_usec 0\n",
	})
	box := Fingerprint{Kind: "generator", CPUs: 4, MemoryBytes: 16_766_414_848}

	assert.Equal(t, box, withinAllowance(box, files))
}

// Datagrams dropped on a full socket receive buffer are the RcvbufErrors column of the Udp line;
// the UdpLite line is a different protocol.
const snmpPage = `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests OutDiscards OutNoRoutes ReasmTimeout ReasmReqds ReasmOKs ReasmFails FragOKs FragFails FragCreates OutTransmits
Ip: 1 64 3510284 0 0 0 0 0 3510280 3459172 18 0 0 0 0 0 0 0 0 3459172
Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors InCsumErrors IgnoredMulti MemErrors
Udp: 403489 345 3107 411941 3107 0 0 12 0
UdpLite: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors InCsumErrors IgnoredMulti MemErrors
UdpLite: 0 0 0 0 99 0 0 0 0
`

func TestTheReceiveBufferDropsAreReadByNameFromTheUdpLine(t *testing.T) {
	dropped, ok := ParseUDPReceiveBufferErrors(snmpPage)
	require.True(t, ok)
	assert.EqualValues(t, 3107, dropped)
}

func TestAPageWithNoUdpLineIsNoReading(t *testing.T) {
	_, ok := ParseUDPReceiveBufferErrors("Ip: Forwarding\nIp: 1\n")
	assert.False(t, ok)
}

func TestAPhaseCountsTheDatagramsEachEndDroppedDuringIt(t *testing.T) {
	generator := &counterSequence{values: []int64{3107, 3507}}
	target := &counterSequence{values: []int64{12, 12}}
	drops := NetworkDrops{ReadGenerator: generator.read, ReadTarget: target.read}

	phase := Phase{Name: "step-16000", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Network: drops})
	require.NoError(t, err)

	require.NotNil(t, result.GeneratorUDPReceiveErrors)
	require.NotNil(t, result.TargetUDPReceiveErrors)
	assert.EqualValues(t, 400, *result.GeneratorUDPReceiveErrors)
	assert.EqualValues(t, 0, *result.TargetUDPReceiveErrors)
}

func TestAnEndWhoseCountersCannotBeReadReportsNothing(t *testing.T) {
	generator := &counterSequence{values: []int64{10, 10}}
	drops := NetworkDrops{ReadGenerator: generator.read}

	generatorDrops, targetDrops := drops.Bracket()()
	require.NotNil(t, generatorDrops)
	assert.EqualValues(t, 0, *generatorDrops)
	assert.Nil(t, targetDrops)
}

func TestACounterThatWentBackwardsIsNoReading(t *testing.T) {
	target := &counterSequence{values: []int64{500, 3}}
	drops := NetworkDrops{ReadTarget: target.read}

	_, targetDrops := drops.Bracket()()
	assert.Nil(t, targetDrops)
}

type cpuSequence struct {
	values []cgroupCPU
	asked  int
}

func (c *cpuSequence) read() (cgroupCPU, bool) {
	value := c.values[min(c.asked, len(c.values)-1)]
	c.asked++
	return value, true
}

type counterSequence struct {
	values []int64
	asked  int
}

func (c *counterSequence) read() (int64, bool) {
	value := c.values[min(c.asked, len(c.values)-1)]
	c.asked++
	return value, true
}
