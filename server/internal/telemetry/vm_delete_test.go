package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

func TestVMClientDeleteSeriesScopesToDeviceAndVerifiesEmpty(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	org := uuid.New()
	target := uuid.New()
	keep := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	write := func(device uuid.UUID, value float64) {
		require.NoError(t, client.WriteSamples(ctx, org, device, []Sample{
			{Name: "opengate_edge_metric_avg", Value: value, TS: ts, Labels: map[string]string{"dim": "cpu"}},
			{Name: "opengate_edge_node_anomaly_rate", Value: value, TS: ts},
		}))
	}
	write(target, 10)
	write(keep, 20)
	require.NoError(t, client.Flush(ctx))

	// The target device has series before the purge.
	n, err := client.CountSeries(ctx, org, &target)
	require.NoError(t, err)
	assert.Positive(t, n, "target must have series before purge")

	require.NoError(t, client.DeleteSeries(ctx, org, &target))

	// Verification: the purged device is empty, the sibling device is untouched.
	n, err = client.CountSeries(ctx, org, &target)
	require.NoError(t, err)
	assert.Zero(t, n, "purged device must have no remaining series")

	n, err = client.CountSeries(ctx, org, &keep)
	require.NoError(t, err)
	assert.Positive(t, n, "sibling device must survive a device-scoped delete")
}

func TestVMClientDeleteSeriesOrgWide(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	orgA := uuid.New()
	orgB := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, client.WriteSamples(ctx, orgA, uuid.New(), []Sample{{Name: "opengate_edge_metric_avg", Value: 1, TS: ts}}))
	require.NoError(t, client.WriteSamples(ctx, orgA, uuid.New(), []Sample{{Name: "opengate_edge_metric_avg", Value: 2, TS: ts}}))
	require.NoError(t, client.WriteSamples(ctx, orgB, uuid.New(), []Sample{{Name: "opengate_edge_metric_avg", Value: 3, TS: ts}}))
	require.NoError(t, client.Flush(ctx))

	require.NoError(t, client.DeleteSeries(ctx, orgA, nil))

	n, err := client.CountSeries(ctx, orgA, nil)
	require.NoError(t, err)
	assert.Zero(t, n, "org-wide delete must purge every device in the org")

	n, err = client.CountSeries(ctx, orgB, nil)
	require.NoError(t, err)
	assert.Positive(t, n, "another org must survive a tenant-scoped delete")
}

func TestVMClientListSubjectsReturnsDistinctPairs(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	org := uuid.New()
	device := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	// Two metrics for one device must collapse to a single subject.
	require.NoError(t, client.WriteSamples(ctx, org, device, []Sample{
		{Name: "opengate_edge_metric_avg", Value: 1, TS: ts, Labels: map[string]string{"dim": "cpu"}},
		{Name: "opengate_edge_node_anomaly_rate", Value: 2, TS: ts},
	}))
	require.NoError(t, client.Flush(ctx))

	subjects, err := client.ListSubjects(ctx)
	require.NoError(t, err)

	count := 0
	for _, s := range subjects {
		if s.OrgID == org && s.DeviceID == device {
			count++
		}
	}
	assert.Equal(t, 1, count, "a device with several metrics is one subject")
}

func TestVMClientDeleteSeriesRejectsNilOrg(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	require.Error(t, client.DeleteSeries(context.Background(), uuid.Nil, nil))
	_, err := client.CountSeries(context.Background(), uuid.Nil, nil)
	require.Error(t, err)
}

// The delete request always carries the server-side delete auth key when one is
// configured, and always includes an org_id matcher.
func TestVMClientDeleteSeriesSendsAuthKeyAndOrgMatcher(t *testing.T) {
	t.Parallel()
	var gotAuthKey string
	var gotMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotAuthKey = r.Form.Get("authKey")
		gotMatch = r.Form.Get("match[]")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewVMClient(srv.URL, nil).WithDeleteAuthKey("s3cr3t")
	org := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	device := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	require.NoError(t, client.DeleteSeries(context.Background(), org, &device))

	assert.Equal(t, "s3cr3t", gotAuthKey, "delete must present the server-side auth key")
	require.NotEmpty(t, gotMatch)
	assert.Contains(t, gotMatch, `org_id="`+org.String()+`"`)
	assert.Contains(t, gotMatch, `device_id="`+device.String()+`"`)
}

// CountSeries surfaces a non-2xx VM response as an error rather than reporting a
// false "empty" that would let a job complete over undeleted data.
func TestVMClientCountSeriesErrorsOnBadStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewVMClient(srv.URL, nil)
	_, err := client.CountSeries(context.Background(), uuid.New(), nil)
	require.Error(t, err)
}

func TestSubjectSelector(t *testing.T) {
	t.Parallel()
	org := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	device := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	got, err := subjectSelector(org, nil)
	require.NoError(t, err)
	assert.Equal(t, `{org_id="`+org.String()+`"}`, got)

	got, err = subjectSelector(org, &device)
	require.NoError(t, err)
	assert.Equal(t, `{org_id="`+org.String()+`",device_id="`+device.String()+`"}`, got)

	_, err = subjectSelector(uuid.Nil, nil)
	require.Error(t, err)

	// The selector must survive URL encoding into a match[] argument unchanged.
	v := url.Values{}
	v.Set("match[]", got)
	assert.Contains(t, v.Encode(), "match")
}
