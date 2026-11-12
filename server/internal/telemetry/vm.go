package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrTenantMatcherNotAllowed is returned when a caller supplies its own tenant_id matcher.
	ErrTenantMatcherNotAllowed = errors.New("tenant_id matcher is not allowed")
	// ErrReservedLabel is returned when a sample tries to override labels the
	// server owns for tenant/device scoping.
	ErrReservedLabel = errors.New("reserved telemetry label")
	// ErrInvalidValue is returned for a value the store refuses: not a number, or infinite.
	ErrInvalidValue = errors.New("invalid telemetry value")
)

// matchParam is VictoriaMetrics' series-selector query argument.
const matchParam = "match[]"

// VMClient writes and reads Edge Sentinel samples through VictoriaMetrics'
// Prometheus import/export APIs.
type VMClient struct {
	baseURL string
	client  *http.Client
	// deleteAuthKey guards VictoriaMetrics' delete-series admin API
	// (-deleteAuthKey). Empty when the endpoint is unguarded (e.g. tests).
	deleteAuthKey string
	// namespace is the environment the server runs in, stamped onto everything
	// it writes. Empty writes no stamp.
	namespace string
}

// environmentLabel names the environment a sample was written in, which keeps production
// and staging apart in one store. Reads group it away, so one device stays one series.
const environmentLabel = "namespace"

// WithNamespace returns a client that stamps everything it writes with the
// environment the server runs in. The receiver is left as it was.
func (v *VMClient) WithNamespace(namespace string) *VMClient {
	stamped := *v
	stamped.namespace = namespace
	return &stamped
}

// ExportedSeries is one newline-delimited object returned by VM's export API.
type ExportedSeries struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// NewVMClient returns a VictoriaMetrics client. Passing nil uses a bounded
// default HTTP client.
func NewVMClient(baseURL string, client *http.Client) *VMClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &VMClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

// WriteSamples imports samples as Prometheus exposition text. The tenant_id and
// device_id labels are always server-owned and cannot be overridden by samples.
func (v *VMClient) WriteSamples(ctx context.Context, tenantID uuid.UUID, deviceID uuid.UUID, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, sample := range samples {
		if err := writePrometheusSample(&body, tenantID, deviceID, sample); err != nil {
			return err
		}
	}
	return v.Import(ctx, body.Bytes())
}

// Import posts readings already in Prometheus exposition text, stamped with the environment.
func (v *VMClient) Import(ctx context.Context, body []byte) error {
	target := v.baseURL + "/api/v1/import/prometheus"
	if v.namespace != "" {
		target += "?" + url.Values{"extra_label": {environmentLabel + "=" + v.namespace}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build vm import request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; version=0.0.4")
	return v.sendChecked(req, "post", "import")
}

// Flush forces pending imports to become immediately queryable. It is intended
// for deterministic tests and ad-hoc verification.
func (v *VMClient) Flush(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/internal/force_flush", nil)
	if err != nil {
		return fmt.Errorf("build vm flush request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("post vm flush: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vm flush status %d", resp.StatusCode)
	}
	return nil
}

// Export queries series for tenantID; the caller's selector omits tenant_id, which this
// method injects.
func (v *VMClient) Export(ctx context.Context, tenantID uuid.UUID, selector string, start, end time.Time) ([]ExportedSeries, error) {
	scoped, err := ScopeSelector(selector, tenantID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set(matchParam, scoped)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	resp, err := v.getChecked(ctx, "/api/v1/export?"+q.Encode(), "export")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var out []ExportedSeries
	dec := json.NewDecoder(resp.Body)
	for {
		var series ExportedSeries
		if err := dec.Decode(&series); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, fmt.Errorf("decode vm export: %w", err)
		}
		out = append(out, series)
	}
}

// ScopeSelector injects a tenant_id label matcher into a single VM series
// selector and rejects any caller-supplied tenant_id matcher.
func ScopeSelector(selector string, tenantID uuid.UUID) (string, error) {
	if tenantID == uuid.Nil {
		return "", fmt.Errorf("tenant_id is required")
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("selector is required")
	}
	if strings.Contains(selector, "tenant_id") {
		return "", ErrTenantMatcherNotAllowed
	}
	if !strings.Contains(selector, "{") {
		return fmt.Sprintf(`%s{tenant_id=%q}`, selector, tenantID.String()), nil
	}
	open := strings.Index(selector, "{")
	if !strings.HasSuffix(selector, "}") || open == len(selector)-1 {
		return "", fmt.Errorf("unsupported selector shape: %q", selector)
	}
	inner := selector[open+1 : len(selector)-1]
	if strings.TrimSpace(inner) == "" {
		return selector[:open] + fmt.Sprintf(`{tenant_id=%q}`, tenantID.String()), nil
	}
	return selector[:open] + fmt.Sprintf(`{tenant_id=%q,`, tenantID.String()) + inner + "}", nil
}

// CheckSample reports whether the store accepts sample on its own: a valid metric name, a finite
// value, and label names that are valid and leave the server's own labels alone.
func CheckSample(sample Sample) error {
	if !validMetricName(sample.Name) {
		return fmt.Errorf("invalid metric name %q", sample.Name)
	}
	if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
		return fmt.Errorf("%w for %s", ErrInvalidValue, sample.Name)
	}
	for k := range sample.Labels {
		if k == tenantLabel || k == deviceLabel || k == environmentLabel {
			return fmt.Errorf("%w: %s", ErrReservedLabel, k)
		}
		if !validLabelName(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
	}
	return nil
}

// writePrometheusSample appends one reading's line to b under the machine's tenant and device.
func writePrometheusSample(b *bytes.Buffer, tenantID uuid.UUID, deviceID uuid.UUID, sample Sample) error {
	line, _, err := appendSample(b.AvailableBuffer(), nil, Scope{Tenant: tenantID, Device: deviceID}, sample)
	if err != nil {
		return err
	}
	b.Write(line)
	return nil
}

func escapeLabelValue(v string) string {
	return string(appendEscaped(nil, v))
}
