package sarvam

import (
	"context"
	"errors"
	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(srv.URL),
		WithRetryDelays(time.Millisecond, 5*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("got %v, want ErrMissingAPIKey", err)
	}
}

func TestNewFromEnvReadsTheEnvironment(t *testing.T) {
	t.Setenv(envAPIKey, "env-key")
	t.Setenv(envBaseURL, "https://example.test")

	client, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if client.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", client.apiKey)
	}
	if client.BaseURL() != "https://example.test" {
		t.Errorf("BaseURL = %q", client.BaseURL())
	}
}

func TestSendsAuthAndUserAgent(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("api-subscription-key"); got != "test-key" {
			t.Errorf("api-subscription-key = %q", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "sarvam-go-sdk/") {
			t.Errorf("User-Agent = %q", got)
		}
		w.Write([]byte(`{}`))
	})

	if err := client.doJSON(context.Background(), http.MethodGet, "/ping", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestErrorsMapToSentinels(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"bad key", 403, `{"error":{"message":"bad key","code":"invalid_api_key_error"}}`, ErrUnauthorized},
		{"bad request", 400, `{"error":{"message":"bad field","code":"invalid_request_error"}}`, ErrInvalidRequest},
		{"unprocessable", 422, `{"error":{"message":"nope","code":"unprocessable_entity_error"}}`, ErrInvalidRequest},
		{"no credits", 429, `{"error":{"message":"no credits","code":"insufficient_quota_error"}}`, ErrQuotaExceeded},
		{"rate limited", 429, `{"error":{"message":"slow down","code":"rate_limit_exceeded_error"}}`, ErrRateLimited},
		{"server", 500, `{"error":{"message":"boom","code":"internal_server_error"}}`, ErrServer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			client.retry.maxRetries = 0

			err := client.doJSON(context.Background(), http.MethodGet, "/x", nil, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("got %T, want an *APIError in the chain", err)
			}
			if apiErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
			}
			if apiErr.Message == "" {
				t.Error("Message is empty")
			}
		})
	}
}

func TestRateLimitErrorCarriesRetryAfter(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"slow down","code":"rate_limit_exceeded_error"}}`))
	})
	client.retry.maxRetries = 0

	err := client.doJSON(context.Background(), http.MethodGet, "/x", nil, nil)
	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("got %v, want *RateLimitError", err)
	}
	if rateErr.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", rateErr.RetryAfter)
	}
}

func TestRetriesUntilItWorks(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{}`))
	})

	if err := client.doJSON(context.Background(), http.MethodGet, "/flaky", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := client.doJSON(context.Background(), http.MethodGet, "/down", nil, nil)
	var retryErr *MaxRetriesError
	if !errors.As(err, &retryErr) {
		t.Fatalf("got %v, want *MaxRetriesError", err)
	}
	if retryErr.Attempts != DefaultMaxRetries+1 {
		t.Errorf("Attempts = %d, want %d", retryErr.Attempts, DefaultMaxRetries+1)
	}
	if !errors.Is(err, ErrMaxRetriesExceeded) || !errors.Is(err, ErrServer) {
		t.Errorf("error chain lost a sentinel: %v", err)
	}
}

func TestRunningOutOfCreditsIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"no credits","code":"insufficient_quota_error"}}`))
	})

	err := client.doJSON(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("got %v, want ErrQuotaExceeded", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1: retrying will not refill credits", got)
	}
}

func testWAV(sampleRate int, d time.Duration) []byte {
	samples := int(float64(sampleRate) * d.Seconds())
	return append(wav.Header(sampleRate, 1, 16, samples*2), make([]byte, samples*2)...)
}
