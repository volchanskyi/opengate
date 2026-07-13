package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// WithDeleteAuthKey configures the server-side key VictoriaMetrics requires on
// its delete-series admin API (-deleteAuthKey). It is never exposed to the edge.
// It returns the receiver for chaining.
func (v *VMClient) WithDeleteAuthKey(key string) *VMClient {
	v.deleteAuthKey = key
	return v
}

// DeleteSeries issues a scoped delete-series covering every metric for the
// subject: the whole org when deviceID is nil, otherwise one device within the
// org. The selector always includes org_id so a purge can never span tenants.
// VictoriaMetrics processes the delete asynchronously and frees disk on a later
// merge, so callers verify completion with CountSeries.
func (v *VMClient) DeleteSeries(ctx context.Context, orgID uuid.UUID, deviceID *uuid.UUID) error {
	selector, err := subjectSelector(orgID, deviceID)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set(matchParam, selector)
	if v.deleteAuthKey != "" {
		q.Set("authKey", v.deleteAuthKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		v.baseURL+"/api/v1/admin/tsdb/delete_series", strings.NewReader(q.Encode()))
	if err != nil {
		return fmt.Errorf("build vm delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("post vm delete: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("vm delete status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// CountSeries returns how many series still match the subject selector. A purge
// job may complete only once this reaches zero.
func (v *VMClient) CountSeries(ctx context.Context, orgID uuid.UUID, deviceID *uuid.UUID) (int, error) {
	selector, err := subjectSelector(orgID, deviceID)
	if err != nil {
		return 0, err
	}
	q := url.Values{}
	q.Set(matchParam, selector)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/api/v1/series?"+q.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("build vm series request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get vm series: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, fmt.Errorf("vm series status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var body struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode vm series: %w", err)
	}
	return len(body.Data), nil
}

// SeriesSubject is one (org, device) pair that owns series in VictoriaMetrics.
// The reconciliation sweep compares these against Postgres to find orphans.
type SeriesSubject struct {
	OrgID    uuid.UUID
	DeviceID uuid.UUID
}

// ListSubjects returns the distinct (org_id, device_id) pairs present in
// VictoriaMetrics. Series lacking either label, or carrying an unparseable one,
// are skipped: a reconciliation sweep must never delete what it cannot scope.
func (v *VMClient) ListSubjects(ctx context.Context) ([]SeriesSubject, error) {
	q := url.Values{}
	q.Set(matchParam, `{device_id=~".+"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/api/v1/series?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build vm subjects request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get vm subjects: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("vm subjects status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var body struct {
		Data []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode vm subjects: %w", err)
	}
	seen := make(map[SeriesSubject]struct{})
	var out []SeriesSubject
	for _, labels := range body.Data {
		org, err := uuid.Parse(labels["org_id"])
		if err != nil {
			continue
		}
		device, err := uuid.Parse(labels["device_id"])
		if err != nil {
			continue
		}
		subject := SeriesSubject{OrgID: org, DeviceID: device}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out, nil
}

// subjectSelector builds a bare label-matcher selector for a purge subject. It
// always pins org_id and, for a device purge, device_id — never a metric name,
// so it matches every metric belonging to the subject.
func subjectSelector(orgID uuid.UUID, deviceID *uuid.UUID) (string, error) {
	if orgID == uuid.Nil {
		return "", fmt.Errorf("org_id is required")
	}
	if deviceID == nil {
		return fmt.Sprintf(`{org_id=%q}`, orgID.String()), nil
	}
	return fmt.Sprintf(`{org_id=%q,device_id=%q}`, orgID.String(), deviceID.String()), nil
}
