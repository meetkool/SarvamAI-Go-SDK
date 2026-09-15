package sarvam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func metricClient(t *testing.T, handler http.HandlerFunc) (*Client, func() []Metric) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var seen []Metric

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(srv.URL),
		WithRetryDelays(time.Millisecond, 5*time.Millisecond),
		WithMetrics(func(m Metric) {
			mu.Lock()
			seen = append(seen, m)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	return client, func() []Metric {
		mu.Lock()
		defer mu.Unlock()
		return append([]Metric(nil), seen...)
	}
}

func TestMetricsReportRequestsAndRetries(t *testing.T) {
	var calls atomic.Int32
	client, metrics := metricClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{}`))
	})

	if err := client.doJSON(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatal(err)
	}

	var requests, retries int
	for _, m := range metrics() {
		switch m.Name {
		case MetricRequest:
			requests++
			if m.Endpoint != "/x" {
				t.Errorf("endpoint = %q, want /x", m.Endpoint)
			}
			if m.Status != http.StatusServiceUnavailable && m.Status != http.StatusOK {
				t.Errorf("status = %d, want 503 or 200", m.Status)
			}
		case MetricRetry:
			retries++
		}
	}
	if requests != 3 {
		t.Errorf("%s count = %d, want 3", MetricRequest, requests)
	}
	if retries != 2 {
		t.Errorf("%s count = %d, want 2", MetricRetry, retries)
	}
}

func TestMetricsReportFirstToken(t *testing.T) {
	client, metrics := metricClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	})

	stream, err := client.Chat.Stream(context.Background(), &models.ChatRequest{
		Model:    models.ChatSarvam105B,
		Messages: []models.Message{models.UserMessage("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for stream.Next() {
	}
	stream.Close()

	for _, m := range metrics() {
		if m.Name == MetricFirstToken {
			if m.Endpoint != chatPath {
				t.Errorf("endpoint = %q, want %q", m.Endpoint, chatPath)
			}
			return
		}
	}
	t.Fatalf("no %s metric was emitted", MetricFirstToken)
}
