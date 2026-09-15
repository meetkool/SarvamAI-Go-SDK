package sarvam

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"time"
)

type backoff interface {
	next(attempt int) time.Duration
}

type exponentialBackoff struct {
	base   time.Duration
	max    time.Duration
	jitter float64
}

func (b exponentialBackoff) next(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := b.base << min(attempt, 10)
	if d > b.max {
		d = b.max
	}
	if n := int64(float64(d) * b.jitter); n > 0 {
		d += time.Duration(rand.Int64N(n))
	}
	return d
}

type constantBackoff struct {
	every time.Duration
}

func (b constantBackoff) next(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return b.every
}

type retryPolicy struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	jitter     float64
}

func (p retryPolicy) strategy() backoff {
	return exponentialBackoff{base: p.baseDelay, max: p.maxDelay, jitter: p.jitter}
}

func (p retryPolicy) delay(attempt int) time.Duration { return p.strategy().next(attempt) }

type retryer struct {
	policy  retryPolicy
	backoff backoff
	sleep   func(context.Context, time.Duration) error
}

func newRetryer(p retryPolicy) *retryer {
	return &retryer{policy: p, backoff: p.strategy(), sleep: sleep}
}

func withRetry(ctx context.Context, p retryPolicy, op func() error) error {
	return newRetryer(p).run(ctx, op)
}

func (r *retryer) run(ctx context.Context, op func() error) error {
	var err error

	for attempt := 0; ; attempt++ {
		wait := r.backoff.next(attempt)

		var rateErr *RateLimitError
		if errors.As(err, &rateErr) && rateErr.RetryAfter > wait {
			wait = min(rateErr.RetryAfter, r.policy.maxDelay)
		}
		if sleepErr := r.sleep(ctx, wait); sleepErr != nil {
			return sleepErr
		}

		err = op()
		if err == nil || !isRetryable(ctx, err) {
			return err
		}
		if attempt >= r.policy.maxRetries {
			if attempt == 0 {
				return err
			}
			return &MaxRetriesError{Attempts: attempt + 1, Err: err}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isRetryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.retryable()
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
