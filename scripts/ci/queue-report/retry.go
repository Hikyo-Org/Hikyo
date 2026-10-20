package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	maxReadAttempts = 3
	maxRetryAfter   = 10 * time.Second
)

// Package-level so tests can run retries instantly and observe them.
var (
	readBackoff           = [maxReadAttempts - 1]time.Duration{time.Second, 3 * time.Second}
	retryLog    io.Writer = os.Stderr
	retryWait             = func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
)

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfterSeconds reads only the delta-seconds form, and only where the
// server is explicitly asking us to back off. Zero means no usable request;
// retryDelay decides whether a long one can be honoured.
func retryAfterSeconds(resp *http.Response) time.Duration {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return 0
	}
	seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return 0
	}
	// Clamp so an absurd value stays absurd (and declined) rather than overflowing.
	return time.Duration(min(seconds, math.MaxInt32)) * time.Second
}

// transientTransportError matches dropped or timed-out connections. It does not
// match everything that satisfies net.Error: *url.Error always does, which would
// also catch the CheckRedirect refusal.
func transientTransportError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var op *net.OpError
	var timeout interface{ Timeout() bool }
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &op) || (errors.As(err, &timeout) && timeout.Timeout())
}

// retryDelay picks the wait before the next attempt, or false when waiting
// would use up the remaining context time. A Retry-After beyond the cap or the
// remaining time ends the retries: retrying sooner than asked would ignore it.
func retryDelay(ctx context.Context, attempt int, retryAfter time.Duration) (time.Duration, bool) {
	remaining := time.Duration(math.MaxInt64)
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	wait := readBackoff[attempt-1]
	if retryAfter > 0 {
		if retryAfter > maxRetryAfter {
			return 0, false
		}
		wait = retryAfter
	}
	return wait, wait < remaining
}

func logRetry(attempt int, wait time.Duration, err error) {
	fmt.Fprintf(retryLog, "queue-report: attempt %d/%d failed, retrying in %s: %v\n", attempt, maxReadAttempts, wait, err)
}
