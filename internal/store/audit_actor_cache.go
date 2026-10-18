package store

import (
	"context"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/store/auditrow"
)

// auditActorCache retains at most one authoritative kind, owned by one SQL
// transaction attempt. Ordinary statements invalidate it conservatively, even
// reads: a statement may mutate principals through RETURNING or a future trigger.
// Only the two append-only audit inserts use the owner's native transaction.
// As with actor resolution followed by insertion, the transaction caller owns
// statement ordering. No state is shared between transactions or retries.
type auditActorCache struct {
	mu       sync.Mutex
	id, kind string
	disabled bool
}

func (c *auditActorCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id, c.kind = "", ""
}

func (c *auditActorCache) disable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disabled = true
	c.id, c.kind = "", ""
}

func (c *auditActorCache) lookup(ctx context.Context, kindOf auditrow.KindLookup, id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !c.disabled && c.id == id && c.kind != "" {
		return c.kind, nil
	}
	kind, err := kindOf(ctx, id)
	// Absence, failures and unknown vocabulary must always be resolved afresh.
	if !c.disabled && err == nil && (kind == "human" || kind == "machine") {
		c.id, c.kind = id, kind
	}
	return kind, err
}
