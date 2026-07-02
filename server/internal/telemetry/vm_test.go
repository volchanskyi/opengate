package telemetry

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

func TestVMClientWritesOrgScopedSamples(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	orgA := uuid.New()
	orgB := uuid.New()
	deviceID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, client.WriteSamples(ctx, orgA, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  41,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.WriteSamples(ctx, orgB, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  82,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, orgA, `opengate_test_ws4_metric{device_id="`+deviceID.String()+`"}`, ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, orgA.String(), series[0].Metric["org_id"])
	assert.Equal(t, []float64{41}, series[0].Values)
}

func TestScopeSelector(t *testing.T) {
	t.Parallel()
	const org = "11111111-1111-1111-1111-111111111111"
	orgID := uuid.MustParse(org)
	tests := []struct {
		name     string
		selector string
		orgID    uuid.UUID
		want     string
		errIs    error
		wantErr  bool
	}{
		{name: "injects into existing label set", selector: `m{device_id="d1"}`, orgID: orgID, want: `m{org_id="` + org + `",device_id="d1"}`},
		{name: "injects into bare metric", selector: "m", orgID: orgID, want: `m{org_id="` + org + `"}`},
		{name: "injects into empty brace set", selector: "m{}", orgID: orgID, want: `m{org_id="` + org + `"}`},
		{name: "rejects caller-supplied org matcher", selector: `m{org_id="other"}`, orgID: orgID, errIs: ErrOrgMatcherNotAllowed},
		{name: "rejects nil org", selector: "m", orgID: uuid.Nil, wantErr: true},
		{name: "rejects empty selector", selector: "   ", orgID: orgID, wantErr: true},
		{name: "rejects unterminated brace set", selector: "m{foo=", orgID: orgID, wantErr: true},
		{name: "rejects trailing open brace", selector: "m{", orgID: orgID, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScopeSelector(tt.selector, tt.orgID)
			switch {
			case tt.errIs != nil:
				assert.ErrorIs(t, err, tt.errIs)
			case tt.wantErr:
				require.Error(t, err)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestWritePrometheusSample(t *testing.T) {
	t.Parallel()
	orgID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	deviceID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ts := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		name     string
		sample   Sample
		want     string
		contains string
		errIs    error
		wantErr  bool
	}{
		{
			name:   "writes sorted label line",
			sample: Sample{Name: "opengate_edge_metric_avg", Value: 1.5, TS: ts, Labels: map[string]string{"dim": "cpu"}},
			want:   `opengate_edge_metric_avg{device_id="33333333-3333-3333-3333-333333333333",dim="cpu",org_id="22222222-2222-2222-2222-222222222222"} 1.5 1700000000000` + "\n",
		},
		{name: "rejects invalid metric name", sample: Sample{Name: "1bad name", Value: 1, TS: ts}, wantErr: true},
		{name: "rejects NaN", sample: Sample{Name: "m", Value: math.NaN(), TS: ts}, wantErr: true},
		{name: "rejects Inf", sample: Sample{Name: "m", Value: math.Inf(1), TS: ts}, wantErr: true},
		{name: "rejects reserved label", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"org_id": "x"}}, errIs: ErrReservedLabel},
		{name: "rejects invalid label name", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"bad-label": "x"}}, wantErr: true},
		{name: "defaults zero timestamp", sample: Sample{Name: "m", Value: 1}, contains: "m{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			err := writePrometheusSample(&b, orgID, deviceID, tt.sample)
			switch {
			case tt.errIs != nil:
				assert.ErrorIs(t, err, tt.errIs)
			case tt.wantErr:
				require.Error(t, err)
			case tt.contains != "":
				require.NoError(t, err)
				assert.Contains(t, b.String(), tt.contains)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, b.String())
			}
		})
	}
}

func TestEscapeLabelValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `a\\b\nc\"d`, escapeLabelValue("a\\b\nc\"d"))
	assert.Equal(t, "plain", escapeLabelValue("plain"))
}

func TestWriteSamplesEmptyIsNoop(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	require.NoError(t, client.WriteSamples(context.Background(), uuid.New(), uuid.New(), nil))
}
