package protocol

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// controlMessageSizeClass is the heap size class every decoded ControlMessage allocates; crossing
// it raises B/op for each control frame, so widen it with benchmarks/baseline.json.
const controlMessageSizeClass = 1536

func TestControlMessageFitsItsAllocationSizeClass(t *testing.T) {
	size := unsafe.Sizeof(ControlMessage{})
	require.LessOrEqual(t, int(size), controlMessageSizeClass,
		"ControlMessage is %d bytes, past the %d-byte size class every decode allocates; "+
			"crossing a class raises B/op for every control frame the server reads",
		size, controlMessageSizeClass)
}

func TestControlMessageSizeClassRejectsAWiderUnion(t *testing.T) {
	type widerThanTheClass struct {
		ControlMessage
		_ [controlMessageSizeClass]byte
	}
	require.Greater(t, int(unsafe.Sizeof(widerThanTheClass{})), controlMessageSizeClass)
}
