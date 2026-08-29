package sarvam

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	p := retryPolicy{maxRetries: 3, baseDelay: 100 * time.Millisecond, maxDelay: 5 * time.Second}

	want := map[int]time.Duration{
		0:  0,
		1:  200 * time.Millisecond,
		2:  400 * time.Millisecond,
		3:  800 * time.Millisecond,
		10: 5 * time.Second,
	}
	for attempt, d := range want {
		if got := p.delay(attempt); got != d {
			t.Errorf("delay(%d) = %v, want %v", attempt, got, d)
		}
	}
}
