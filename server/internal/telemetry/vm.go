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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrOrgMatcherNotAllowed is returned when a caller tries to provide its
	// own org_id matcher instead of letting the scoped client inject it.
	ErrOrgMatcherNotAllowed = errors.New("org_id matcher is not allowed")
	// ErrReservedLabel is returned when a sample tries to override labels the
	// server owns for tenant/device scoping.
	ErrReservedLabel = errors.New("reserved telemetry label")

	metricNameRE = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameRE  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// VMClient writes and reads Edge Sentinel samples through VictoriaMetrics'
// Prometheus import/export APIs.
type VMClient struct {
	baseURL string
	client  *http.Client
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

// WriteSamples imports samples as Prometheus exposition text. The org_id and
// device_id labels are always server-owned and cannot be overridden by samples.
func (v *VMClient) WriteSamples(ctx context.Context, orgID uuid.UUID, deviceID uuid.UUID, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, sample := range samples {
		if err := writePrometheusSample(&body, orgID, deviceID, sample); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/api/v1/import/prometheus", &body)
	if err != nil {
		return fmt.Errorf("build vm import request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; version=0.0.4")
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("post vm import: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("vm import status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
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

// Export queries series through the scoped selector path used by future
// telemetry reads. The caller supplies a selector without org_id; this method
// injects the authoritative tenant matcher.
func (v *VMClient) Export(ctx context.Context, orgID uuid.UUID, selector string, start, end time.Time) ([]ExportedSeries, error) {
	scoped, err := ScopeSelector(selector, orgID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("match[]", scoped)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/api/v1/export?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build vm export request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get vm export: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("vm export status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

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

// ScopeSelector injects an org_id label matcher into a single VM series
// selector and rejects any caller-supplied org_id matcher.
func ScopeSelector(selector string, orgID uuid.UUID) (string, error) {
	if orgID == uuid.Nil {
		return "", fmt.Errorf("org_id is required")
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("selector is required")
	}
	if strings.Contains(selector, "org_id") {
		return "", ErrOrgMatcherNotAllowed
	}
	if !strings.Contains(selector, "{") {
		return fmt.Sprintf(`%s{org_id=%q}`, selector, orgID.String()), nil
	}
	open := strings.Index(selector, "{")
	if !strings.HasSuffix(selector, "}") || open == len(selector)-1 {
		return "", fmt.Errorf("unsupported selector shape: %q", selector)
	}
	inner := selector[open+1 : len(selector)-1]
	if strings.TrimSpace(inner) == "" {
		return selector[:open] + fmt.Sprintf(`{org_id=%q}`, orgID.String()), nil
	}
	return selector[:open] + fmt.Sprintf(`{org_id=%q,`, orgID.String()) + inner + "}", nil
}

func writePrometheusSample(b *bytes.Buffer, orgID uuid.UUID, deviceID uuid.UUID, sample Sample) error {
	if !metricNameRE.MatchString(sample.Name) {
		return fmt.Errorf("invalid metric name %q", sample.Name)
	}
	if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
		return fmt.Errorf("invalid metric value for %s", sample.Name)
	}
	if sample.TS.IsZero() {
		sample.TS = time.Now()
	}

	labels := make(map[string]string, len(sample.Labels)+2)
	labels["org_id"] = orgID.String()
	labels["device_id"] = deviceID.String()
	for k, v := range sample.Labels {
		if k == "org_id" || k == "device_id" {
			return fmt.Errorf("%w: %s", ErrReservedLabel, k)
		}
		if !labelNameRE.MatchString(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
		labels[k] = v
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString(sample.Name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(b, `%s="%s"`, k, escapeLabelValue(labels[k]))
	}
	fmt.Fprintf(b, "} %s %d\n",
		strconv.FormatFloat(sample.Value, 'g', -1, 64),
		sample.TS.UTC().UnixMilli())
	return nil
}

func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return strings.ReplaceAll(v, `"`, `\"`)
}
