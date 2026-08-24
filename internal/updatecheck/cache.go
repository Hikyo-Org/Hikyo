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

// Releases refreshes at most once per TTL. Refresh failures are returned
// loudly and do not advance freshness; callers may retain their last rendered
// state, but they cannot mistake stale release data for a successful check.
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
		return nil, err
	}
	c.checkedAt = now
	c.releases = slices.Clone(releases)
	return slices.Clone(c.releases), nil
}
