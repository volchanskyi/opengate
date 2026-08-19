package protocol

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// The decoder allocates one ControlMessage per frame, and the union carries a
// field for every message type, so the struct's own width is what a decode
// costs on the server's hottest path. Go rounds a heap allocation up to a size
// class, which is why the cost moves in steps rather than by the bytes a new
// field adds: crossing 1408 took BenchmarkCodec_DecodeControl from 1592 to 1848
// B/op for eight bytes of new fields.
//
// The bound below is the current size class. A field that crosses it costs
// every decode in the fleet another step, so the growth is a decision to take
// deliberately: widen the bound here in the same commit, and move
// benchmarks/baseline.json with it.
const controlMessageSizeClass = 1536

func TestControlMessageFitsItsAllocationSizeClass(t *testing.T) {
	size := unsafe.Sizeof(ControlMessage{})
	require.LessOrEqual(t, int(size), controlMessageSizeClass,
		"ControlMessage is %d bytes, past the %d-byte size class every decode allocates; "+
			"crossing a class raises B/op for every control frame the server reads",
		size, controlMessageSizeClass)
}

// The negative case: a union one field wider than the class must be rejected by
// the same rule, so the guard is known to fail rather than merely to pass.
func TestControlMessageSizeClassRejectsAWiderUnion(t *testing.T) {
	type widerThanTheClass struct {
		ControlMessage
		_ [controlMessageSizeClass]byte
	}
	require.Greater(t, int(unsafe.Sizeof(widerThanTheClass{})), controlMessageSizeClass)
}
