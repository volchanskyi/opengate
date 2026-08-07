package protocol

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// controlMessageReflect is a defined type over ControlMessage. Defined types do
// not inherit the methods of their underlying type, so marshalling this shape
// goes through vmihailenco/msgpack's generic reflection-based struct encoder
// while ControlMessage itself uses its hand-written EncodeMsgpack. That makes it
// the reference implementation these tests diff the hand-written encoder against.
type controlMessageReflect ControlMessage

// marshalReflect encodes msg through the reflection-based struct encoder.
func marshalReflect(t *testing.T, msg *ControlMessage) []byte {
	t.Helper()
	data, err := msgpack.Marshal((*controlMessageReflect)(msg))
	require.NoError(t, err)
	return data
}

// nonZeroValue returns a non-zero value of type typ, used to populate exactly
// one ControlMessage field at a time.
func nonZeroValue(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		return reflect.ValueOf("x").Convert(typ)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(int64(7)).Convert(typ)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(uint64(7)).Convert(typ)
	case reflect.Float32, reflect.Float64:
		return reflect.ValueOf(1.5).Convert(typ)
	case reflect.Bool:
		return reflect.ValueOf(true).Convert(typ)
	case reflect.Slice:
		// A one-element slice is non-empty regardless of the element's own value.
		return reflect.MakeSlice(typ, 1, 1)
	case reflect.Pointer:
		p := reflect.New(typ.Elem())
		if typ.Elem().Kind() == reflect.Bool {
			p.Elem().SetBool(true)
		}
		return p
	default:
		t.Fatalf("nonZeroValue: unhandled kind %s for type %s", typ.Kind(), typ)
		return reflect.Value{}
	}
}

// TestEncodeControlMatchesReflectionPerField walks every field of
// ControlMessage and asserts the hand-written encoder emits byte-identical
// msgpack to the reflection encoder when only that field is populated. This is
// the wire-compatibility guard: it fails on any dropped field, wrong key name,
// wrong field order, or wrong omitempty semantics — for every field, not just
// the ones a hand-picked fixture happens to cover.
func TestEncodeControlMatchesReflectionPerField(t *testing.T) {
	c := &Codec{}
	typ := reflect.TypeOf(ControlMessage{})

	for i := range typ.NumField() {
		field := typ.Field(i)
		t.Run(field.Name, func(t *testing.T) {
			msg := &ControlMessage{Type: MsgAgentRegister}
			reflect.ValueOf(msg).Elem().Field(i).Set(nonZeroValue(t, field.Type))

			got, err := c.EncodeControl(msg)
			require.NoError(t, err)
			assert.Equal(t, marshalReflect(t, msg), got,
				"field %s: hand-written encoder must be byte-identical to the reflection encoder",
				field.Name)
		})
	}
}

// TestEncodeControlMatchesReflectionAllFieldsSet populates every field at once,
// which pins the emitted field ordering and the map length across the whole
// struct rather than one field at a time.
func TestEncodeControlMatchesReflectionAllFieldsSet(t *testing.T) {
	c := &Codec{}
	msg := &ControlMessage{}
	v := reflect.ValueOf(msg).Elem()
	typ := v.Type()
	for i := range typ.NumField() {
		v.Field(i).Set(nonZeroValue(t, typ.Field(i).Type))
	}

	got, err := c.EncodeControl(msg)
	require.NoError(t, err)
	assert.Equal(t, marshalReflect(t, msg), got)
}

// TestEncodeControlMatchesReflectionZeroValue covers the degenerate message:
// only the non-omitempty Type key survives, and it is emitted even when empty.
func TestEncodeControlMatchesReflectionZeroValue(t *testing.T) {
	c := &Codec{}
	msg := &ControlMessage{}

	got, err := c.EncodeControl(msg)
	require.NoError(t, err)
	assert.Equal(t, marshalReflect(t, msg), got)
}

// TestEncodeControlMatchesReflectionRoundTrip asserts the hand-written encoder
// still decodes back to an equal message through the reflection-based decoder,
// for representative messages of several types.
func TestEncodeControlMatchesReflectionRoundTrip(t *testing.T) {
	tr := true
	tests := []struct {
		name string
		msg  *ControlMessage
	}{
		{
			name: "agent register",
			msg: &ControlMessage{
				Type:         MsgAgentRegister,
				Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal},
				Hostname:     "host-1",
				OS:           "linux",
				Arch:         "amd64",
				Version:      "0.1.0",
			},
		},
		{
			name: "heartbeat",
			msg:  &ControlMessage{Type: MsgAgentHeartbeat, Timestamp: 1700000000},
		},
		{
			name: "health summary with breaches",
			msg: &ControlMessage{
				Type:            MsgAgentHealthSummary,
				TS:              1700000001,
				TenantID:        "tenant-1",
				NodeAnomalyRate: 0.25,
				PerFamilyRates:  []FamilyAnomalyRate{{Family: "cpu", Rate: 0.5}},
				RecentBitmask:   []byte{0x01, 0x02},
				Breaches:        []AlertBreach{{RuleID: "r1", Metric: "cpu", Value: 9}},
			},
		},
		{
			name: "session request with permissions",
			msg: &ControlMessage{
				Type:        MsgSessionRequest,
				Token:       SessionToken("tok"),
				Permissions: &Permissions{Desktop: true, Terminal: true},
			},
		},
		{
			name: "maintenance false stays on the wire",
			msg:  &ControlMessage{Type: MsgSetMaintenanceMode, Enabled: new(bool)},
		},
		{
			name: "maintenance true",
			msg:  &ControlMessage{Type: MsgSetMaintenanceMode, Enabled: &tr},
		},
	}

	c := &Codec{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.EncodeControl(tc.msg)
			require.NoError(t, err)
			assert.Equal(t, marshalReflect(t, tc.msg), got)

			decoded, err := c.DecodeControl(got)
			require.NoError(t, err)
			assert.Equal(t, tc.msg, decoded)
		})
	}
}

// TestEncodeControlAllocationBudget pins the allocation cost of encoding a
// control message so it stays independent of how many fields ControlMessage
// declares. The reflection encoder heap-boxes every omitempty field on every
// call to test it for emptiness, so its cost grew with each new protocol field;
// the hand-written encoder tests emptiness with direct typed comparisons and
// allocates only for the output buffer and the values actually emitted.
func TestEncodeControlAllocationBudget(t *testing.T) {
	c := &Codec{}
	msg := &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal, CapFileManager},
		Hostname:     "test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	}

	avg := testing.AllocsPerRun(200, func() {
		if _, err := c.EncodeControl(msg); err != nil {
			t.Fatalf("encode: %v", err)
		}
	})

	const budget = 12
	assert.LessOrEqualf(t, avg, float64(budget),
		"EncodeControl allocated %.0f objects/op (budget %d); a per-field cost has crept back in",
		avg, budget)
}

// TestEncodeControlAllocationsDoNotScaleWithFieldCount asserts the encoder's
// allocation cost tracks the fields a message actually carries, not the field
// count of the ControlMessage union: a one-field heartbeat must not pay for the
// register message's fields.
func TestEncodeControlAllocationsDoNotScaleWithFieldCount(t *testing.T) {
	c := &Codec{}
	heartbeat := &ControlMessage{Type: MsgAgentHeartbeat, Timestamp: 1700000000}

	avg := testing.AllocsPerRun(200, func() {
		if _, err := c.EncodeControl(heartbeat); err != nil {
			t.Fatalf("encode: %v", err)
		}
	})

	const budget = 6
	assert.LessOrEqualf(t, avg, float64(budget),
		"encoding a two-field heartbeat allocated %.0f objects/op (budget %d)", avg, budget)
}
