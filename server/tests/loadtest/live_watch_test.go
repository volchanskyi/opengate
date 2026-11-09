package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportStore answers the store's export API with the readings it holds, one JSON line a series,
// and records each selector it was asked.
type exportStore struct {
	mu    sync.Mutex
	held  map[string][]int64
	asked []string
}

func (s *exportStore) hold(device string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held[device] = append(s.held[device], ts)
}

func (s *exportStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path != "/api/v1/export" {
		http.NotFound(w, r)
		return
	}
	selector := r.URL.Query().Get("match[]")
	s.asked = append(s.asked, selector)
	for device, stamps := range s.held {
		if !strings.Contains(selector, device) {
			continue
		}
		millis := make([]string, len(stamps))
		values := make([]string, len(stamps))
		for i, ts := range stamps {
			millis[i] = fmt.Sprint(ts * 1000)
			values[i] = "1"
		}
		_, _ = fmt.Fprintf(w, `{"metric":{"__name__":"opengate_edge_metric_avg","device_id":%q,"dim":"cpu.total"},"values":[%s],"timestamps":[%s]}`+"\n",
			device, strings.Join(values, ","), strings.Join(millis, ","))
	}
}

func TestTheWatcherTimesEachWatchedMinuteFromSentToVisible(t *testing.T) {
	store := &exportStore{held: map[string][]int64{}}
	server := httptest.NewServer(store)
	defer server.Close()
	watch := newLiveWatch(server.URL, server.Client())

	sent := time.Unix(1_700_000_000, 0)
	watch.sentAt("dev-a", 1_699_999_940, sent)
	watch.sentAt("dev-b", 1_699_999_940, sent)

	store.hold("dev-a", 1_699_999_940)
	require.NoError(t, watch.poll(context.Background(), sent.Add(2*time.Second)))
	store.hold("dev-b", 1_699_999_940)
	require.NoError(t, watch.poll(context.Background(), sent.Add(7*time.Second)))
	require.NoError(t, watch.poll(context.Background(), sent.Add(9*time.Second)))

	delay := watch.result()
	require.NotNil(t, delay)
	assert.Equal(t, 2, delay.Sent)
	assert.Equal(t, 2, delay.Seen)
	assert.Equal(t, 1, delay.WithinFiveSeconds)
	assert.InDelta(t, 7000, delay.MaxMs, 0.001)
	store.mu.Lock()
	defer store.mu.Unlock()
	assert.Contains(t, store.asked[0], `dim="cpu.total"`)
	assert.Contains(t, store.asked[0], "dev-a")
	assert.Len(t, store.asked, 2, "nothing is asked once every watched minute was seen")
}

func TestAMinuteNeverSeenCountsAsSentAndNotSeen(t *testing.T) {
	store := &exportStore{held: map[string][]int64{}}
	server := httptest.NewServer(store)
	defer server.Close()
	watch := newLiveWatch(server.URL, server.Client())

	watch.sentAt("dev-c", 1_699_999_940, time.Unix(1_700_000_000, 0))
	require.NoError(t, watch.poll(context.Background(), time.Unix(1_700_000_030, 0)))

	delay := watch.result()
	assert.Equal(t, 1, delay.Sent)
	assert.Zero(t, delay.Seen)
	assert.Zero(t, delay.WithinFiveSeconds, "a minute that never appeared is a miss")
}

func TestAStoreThatRefusesTheReadIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	watch := newLiveWatch(server.URL, server.Client())
	watch.sentAt("dev-d", 1_699_999_940, time.Unix(1_700_000_000, 0))

	assert.Error(t, watch.poll(context.Background(), time.Unix(1_700_000_001, 0)))
}

func TestAWatchThatSawNothingSentReportsNone(t *testing.T) {
	assert.Nil(t, newLiveWatch("http://store.invalid", http.DefaultClient).result())
	var none *liveWatch
	assert.Nil(t, none.result())
	none.sentAt("dev", 1, time.Now())
}

func TestTheWatchFollowsOnlyTheMachinesItIsAskedTo(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	device := "6f1c2c8e-3b52-4d0a-9a43-1f2d3c4b5a69"
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: device},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: device}}, &key.PublicKey, key)
	require.NoError(t, err)
	config := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}}}}
	opts := loadOptions{liveWatch: newLiveWatch("http://store.invalid", http.DefaultClient), watchMachines: 2}

	assert.Equal(t, device, watchedDevice(config, tenantAgent{agentIndex: 1}, opts), "a watched machine is named by its certificate")
	assert.Empty(t, watchedDevice(config, tenantAgent{agentIndex: 2}, opts), "a machine past the count is not followed")
	assert.Empty(t, watchedDevice(config, tenantAgent{agentIndex: 0}, loadOptions{}), "no watch follows nobody")
}

func TestStartingAWatchWithNoStoreWatchesNothing(t *testing.T) {
	opts := loadOptions{}
	stop := startLiveWatch(&opts, "", 50)
	assert.Nil(t, opts.liveWatch)
	assert.Nil(t, stop())

	store := &exportStore{held: map[string][]int64{}}
	server := httptest.NewServer(store)
	defer server.Close()
	stop = startLiveWatch(&opts, server.URL, 3)
	require.NotNil(t, opts.liveWatch)
	assert.Equal(t, 3, opts.watchMachines)
	opts.liveWatch.sentAt("dev-e", 60, time.Now())
	assert.Equal(t, &LiveDelay{Sent: 1}, stop(), "stopping reads what the watch saw")
}

func TestTheRunReportSaysHowLateTheLiveMinutesWere(t *testing.T) {
	var out bytes.Buffer
	writeLiveDelay(&out, &LiveDelay{Sent: 600, Seen: 598, WithinFiveSeconds: 12, P50Ms: 31000, P99Ms: 59000, MaxMs: 61000})
	assert.Contains(t, out.String(), "598 of 600 watched minutes seen, 12 within 5s; p50 31000 ms, p99 59000 ms, max 61000 ms")
	out.Reset()
	writeLiveDelay(&out, nil)
	assert.Empty(t, out.String())
}

func TestTheTallyDatesTheLastStoredBatchFromTheRunsStart(t *testing.T) {
	tally := &catchUpTally{started: time.Now().Add(-90 * time.Second)}
	tally.record(batchStored)
	assert.InDelta(t, 90, tally.count().LastStoredSeconds, 2)
}

func TestTheBundleCarriesTheLiveDelay(t *testing.T) {
	in := measuredRun()
	in.LiveDelay = &LiveDelay{Sent: 10, Seen: 10, WithinFiveSeconds: 10}
	assert.Equal(t, in.LiveDelay, buildRunBundle(in).LiveDelay)
}
