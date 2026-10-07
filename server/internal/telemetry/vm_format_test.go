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
)

func TestScopeSelector(t *testing.T) {
	t.Parallel()
	const tenant = "11111111-1111-1111-1111-111111111111"
	tenantID := uuid.MustParse(tenant)
	tests := []struct {
		name     string
		selector string
		tenantID uuid.UUID
		want     string
		errIs    error
		wantErr  bool
	}{
		{name: "injects into existing label set", selector: `m{device_id="d1"}`, tenantID: tenantID, want: `m{tenant_id="` + tenant + `",device_id="d1"}`},
		{name: "injects into bare metric", selector: "m", tenantID: tenantID, want: `m{tenant_id="` + tenant + `"}`},
		{name: "injects into empty brace set", selector: "m{}", tenantID: tenantID, want: `m{tenant_id="` + tenant + `"}`},
		{name: "rejects caller-supplied tenant matcher", selector: `m{tenant_id="other"}`, tenantID: tenantID, errIs: ErrTenantMatcherNotAllowed},
		{name: "rejects nil tenant", selector: "m", tenantID: uuid.Nil, wantErr: true},
		{name: "rejects empty selector", selector: "   ", tenantID: tenantID, wantErr: true},
		{name: "rejects unterminated brace set", selector: "m{foo=", tenantID: tenantID, wantErr: true},
		{name: "rejects trailing open brace", selector: "m{", tenantID: tenantID, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScopeSelector(tt.selector, tt.tenantID)
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
	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
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
			want:   `opengate_edge_metric_avg{device_id="33333333-3333-3333-3333-333333333333",dim="cpu",tenant_id="22222222-2222-2222-2222-222222222222"} 1.5 1700000000000` + "\n",
		},
		{name: "rejects invalid metric name", sample: Sample{Name: "1bad name", Value: 1, TS: ts}, wantErr: true},
		{name: "rejects NaN", sample: Sample{Name: "m", Value: math.NaN(), TS: ts}, wantErr: true},
		{name: "rejects Inf", sample: Sample{Name: "m", Value: math.Inf(1), TS: ts}, wantErr: true},
		{name: "rejects reserved label", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"tenant_id": "x"}}, errIs: ErrReservedLabel},
		{name: "rejects invalid label name", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"bad-label": "x"}}, wantErr: true},
		{name: "defaults zero timestamp", sample: Sample{Name: "m", Value: 1}, contains: "m{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			err := writePrometheusSample(&b, tenantID, deviceID, tt.sample)
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
