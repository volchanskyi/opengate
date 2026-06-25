package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"pgregory.net/rapid"
)

// Property-based coverage for the model→API converters, the nil-deref helpers,
// and the device-logs pagination math. Complements the example-based cases in
// converters_test.go. rapid.Check always runs under `go test` and explores a
// bounded number of cases deterministically, per tests-determinism.md.

// TestProperty_DevicesToAPI_PreservesOrderAndLength asserts the slice converter
// preserves length and per-element identity/order — i.e. it is a faithful 1:1
// map, never reordering, dropping, or duplicating a device.
func TestProperty_DevicesToAPI_PreservesOrderAndLength(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		ds := make([]*device.Device, n)
		for i := range ds {
			ds[i] = &device.Device{
				ID:       uuid.New(),
				GroupID:  uuid.New(),
				Hostname: rapid.String().Draw(t, "hostname"),
				OS:       rapid.String().Draw(t, "os"),
			}
		}

		out := devicesToAPI(ds)
		require.Len(t, out, n)
		for i := range ds {
			require.Equal(t, ds[i].ID, out[i].Id)
			require.Equal(t, ds[i].Hostname, out[i].Hostname)
			require.Equal(t, ds[i].OS, out[i].Os)
		}
	})
}

// TestProperty_DeviceToAPI_OsDisplayPointer asserts the OsDisplay optional is
// nil exactly when the source string is empty, and otherwise points to the
// source value — the invariant the handler relies on to omit the field.
func TestProperty_DeviceToAPI_OsDisplayPointer(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		osDisplay := rapid.String().Draw(t, "osDisplay")
		d := &device.Device{ID: uuid.New(), GroupID: uuid.New(), OsDisplay: osDisplay}

		out := deviceToAPI(d)
		if osDisplay == "" {
			require.Nil(t, out.OsDisplay)
		} else {
			require.NotNil(t, out.OsDisplay)
			require.Equal(t, osDisplay, *out.OsDisplay)
		}
	})
}

// TestProperty_DerefHelpers asserts the nil-deref helpers: nil yields the
// fallback (zero / supplied), a non-nil pointer yields its pointee, for
// arbitrary inputs.
func TestProperty_DerefHelpers(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// derefBool
		require.False(t, derefBool(nil))
		b := rapid.Bool().Draw(t, "b")
		require.Equal(t, b, derefBool(&b))

		// derefInt
		fallback := rapid.Int().Draw(t, "fallback")
		require.Equal(t, fallback, derefInt(nil, fallback))
		v := rapid.Int().Draw(t, "v")
		require.Equal(t, v, derefInt(&v, fallback))

		// derefStr
		require.Equal(t, "", derefStr[string](nil))
		s := rapid.String().Draw(t, "s")
		require.Equal(t, s, derefStr(&s))
	})
}

// TestProperty_DeviceLogsToAPI_Pagination asserts the pagination read model:
// Entries length and Total are preserved verbatim, and HasMore is true exactly
// when another page exists beyond the current window (Offset+Limit < Total).
// Random offset/limit/total exercise the Offset+Limit == Total boundary the
// example tests pin only at fixed points.
func TestProperty_DeviceLogsToAPI_Pagination(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(0, 1_000_000).Draw(t, "total")
		offset := rapid.IntRange(0, 1_000_000).Draw(t, "offset")
		limit := rapid.IntRange(0, 1_000_000).Draw(t, "limit")
		nEntries := rapid.IntRange(0, 50).Draw(t, "nEntries")

		entries := make([]device.LogEntry, nEntries)
		filter := device.LogFilter{Offset: offset, Limit: limit}

		resp := deviceLogsToAPI(entries, total, filter)
		require.Len(t, resp.Entries, nEntries)
		require.Equal(t, total, resp.Total)
		require.Equal(t, offset+limit < total, resp.HasMore)
	})
}
