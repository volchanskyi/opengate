package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"pgregory.net/rapid"
)

func TestProperty_DevicesToAPI_PreservesOrderAndLength(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		ds := make([]*device.Device, n)
		for i := range ds {
			ds[i] = &device.Device{
				ID:       uuid.New(),
				SiteID:   uuid.New(),
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

func TestProperty_DeviceToAPI_OsDisplayPointer(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		osDisplay := rapid.String().Draw(t, "osDisplay")
		d := &device.Device{ID: uuid.New(), SiteID: uuid.New(), OsDisplay: osDisplay}

		out := deviceToAPI(d)
		if osDisplay == "" {
			require.Nil(t, out.OsDisplay)
		} else {
			require.NotNil(t, out.OsDisplay)
			require.Equal(t, osDisplay, *out.OsDisplay)
		}
	})
}

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

func TestProperty_DeviceLogsToAPI_Pagination(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(0, 1_000_000).Draw(t, "total")
		offset := rapid.IntRange(0, 1_000_000).Draw(t, "offset")
		limit := rapid.IntRange(0, 1_000_000).Draw(t, "limit")
		nEntries := rapid.IntRange(0, 50).Draw(t, "nEntries")

		entries := make([]device.LogEntry, nEntries)
		filter := device.LogFilter{Offset: offset, Limit: limit}

		resp := deviceLogsToAPI(entries, total, nil, filter)
		require.Len(t, resp.Entries, nEntries)
		require.Equal(t, total, resp.Total)
		require.Equal(t, offset+limit < total, resp.HasMore)
	})
}
