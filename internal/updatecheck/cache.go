package updatecheck

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// CachedSource bounds release-source requests across all callers in a process.
type CachedSource struct {
	mu        sync.Mutex
	source    Source
	ttl       time.Duration
	now       func() time.Time
	checkedAt time.Time
	releases  []Release
}

// NewCachedSource validates the cache seam before first use.
func NewCachedSource(source Source, ttl time.Duration, now func() time.Time) (*CachedSource, error) {
	if source == nil {
		return nil, errors.New("updatecheck: cached source requires a source")
	}
	if ttl <= 0 {
		return nil, errors.New("updatecheck: cached source requires a positive TTL")
	}
	if now == nil {
		now = time.Now
	}
	return &CachedSource{source: source, ttl: ttl, now: now}, nil
}

// Releases refreshes at most once per TTL. A last successful immutable
// release list remains usable during a source outage.
func (c *CachedSource) Releases(ctx context.Context) ([]Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	age := now.Sub(c.checkedAt)
	if !c.checkedAt.IsZero() && age >= 0 && age < c.ttl {
		return slices.Clone(c.releases), nil
	}
	releases, err := c.source.Releases(ctx)
	if err != nil {
		if c.checkedAt.IsZero() {
			return nil, err
		}
		c.checkedAt = now
		return slices.Clone(c.releases), nil
	}
	c.checkedAt = now
	c.releases = slices.Clone(releases)
	return slices.Clone(c.releases), nil
}
