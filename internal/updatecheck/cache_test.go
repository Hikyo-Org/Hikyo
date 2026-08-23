package updatecheck

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sourceFunc func(context.Context) ([]Release, error)

func (fn sourceFunc) Releases(ctx context.Context) ([]Release, error) { return fn(ctx) }

func TestCachedSourceRefreshesOncePerTTLAndFallsBackToLastSuccess(t *testing.T) {
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	calls := 0
	fail := false
	source := sourceFunc(func(context.Context) ([]Release, error) {
		calls++
		if fail {
			return nil, errors.New("offline")
		}
		return []Release{{Version: "1.0.1"}}, nil
	})
	cached, err := NewCachedSource(source, 6*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := cached.Releases(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("source calls = %d, want one inside TTL", calls)
	}

	now = now.Add(7 * time.Hour)
	fail = true
	releases, err := cached.Releases(t.Context())
	if err != nil {
		t.Fatalf("stale fallback failed: %v", err)
	}
	if calls != 2 || len(releases) != 1 || releases[0].Version != "1.0.1" {
		t.Fatalf("fallback = %+v after %d calls", releases, calls)
	}
}

func TestCachedSourceRequiresSourceAndPositiveTTL(t *testing.T) {
	if _, err := NewCachedSource(nil, time.Hour, time.Now); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := NewCachedSource(sourceFunc(func(context.Context) ([]Release, error) { return nil, nil }), 0, time.Now); err == nil {
		t.Fatal("zero TTL accepted")
	}
}

func TestCachedSourceRefreshesAfterClockRollback(t *testing.T) {
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	calls := 0
	cached, err := NewCachedSource(sourceFunc(func(context.Context) ([]Release, error) {
		calls++
		return []Release{{Version: "1.0.1"}}, nil
	}), time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cached.Releases(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(-time.Hour)
	if _, err := cached.Releases(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("source calls = %d, want refresh after clock rollback", calls)
	}
}
