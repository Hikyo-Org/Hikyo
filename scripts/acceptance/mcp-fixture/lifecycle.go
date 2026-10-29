//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Locks are permitted only on this disposable fixture. They simulate admitted
// work stalled in the real store, not a timer or mocked service. Never use
// this function against an existing installation or an unapproved database.
func (p *probe) lifecycle(ctx context.Context, pool *pgxpool.Pool, processes []*serverProcess, sequence *atomic.Uint64) error {
	for i := 0; i < 2; i++ {
		lock, err := pool.Begin(ctx)
		if err != nil {
			return errors.New("fixture store lock unavailable")
		}
		if _, err := lock.Exec(ctx, "LOCK TABLE keys IN ACCESS EXCLUSIVE MODE"); err != nil {
			lock.Rollback(ctx)
			return errors.New("fixture catalogue lock unavailable")
		}
		callCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		index := int(sequence.Load()) % 2
		go func() {
			_, err := p.call(callCtx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
			done <- err
		}()
		admitted := func() bool {
			var n int
			err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight WHERE principal_id=$1", p.fixture.Credentials["lifecycle"].Principal).Scan(&n)
			if err != nil || n != 1 {
				return false
			}
			err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'", fmt.Sprintf("mcp651-%d", index)).Scan(&n)
			return err == nil && n > 0
		}
		if err := wait(ctx, 4*time.Second, admitted); err != nil {
			cancel()
			lock.Rollback(ctx)
			<-done
			return errors.New("actual admitted blocked query not observed")
		}
		start := time.Now()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			lock.Rollback(ctx)
			return errors.New("proxy cancellation did not return")
		}
		// Keep the table lock held until BOTH the shared claim and the blocked
		// backend query have disappeared. A client error alone cannot pass.
		if err := wait(ctx, 3*time.Second, func() bool {
			var claims, waiting int
			if pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight WHERE principal_id=$1", p.fixture.Credentials["lifecycle"].Principal).Scan(&claims) != nil {
				return false
			}
			if pool.QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'", fmt.Sprintf("mcp651-%d", index)).Scan(&waiting) != nil {
				return false
			}
			return claims == 0 && waiting == 0
		}); err != nil {
			lock.Rollback(ctx)
			return errors.New("cancelled store work or shared claim persisted")
		}
		p.receipt.Measurements[fmt.Sprintf("local_proxy_cancel_process_%d", index)] = time.Since(start).Milliseconds()
		lock.Rollback(ctx)
	}
	p.receipt.Checks["local_proxy_cancellation_store_context_claims_both"] = "PASS"
	// Drain one process with an actual admitted query. The unchanged server
	// grace is five seconds; this probe neither alters it nor waits 30 seconds.
	lock, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("drain lock unavailable")
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, "LOCK TABLE keys IN ACCESS EXCLUSIVE MODE"); err != nil {
		return errors.New("drain catalogue lock unavailable")
	}
	index := int(sequence.Load()) % 2
	done := make(chan error, 1)
	go func() {
		r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
		if err == nil && !successful(r) {
			err = errors.New("draining admitted read failed")
		}
		done <- err
	}()
	if err := wait(ctx, 4*time.Second, func() bool {
		var n int
		return pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight WHERE principal_id=$1", p.fixture.Credentials["lifecycle"].Principal).Scan(&n) == nil && n == 1
	}); err != nil {
		return errors.New("drain admission not observed")
	}
	start := time.Now()
	if err := processes[index].cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return errors.New("SIGTERM failed")
	}
	// Releasing the owned store lock permits the already-admitted read to
	// finish while shutdown refuses new work. No application state is edited.
	if err := lock.Rollback(ctx); err != nil {
		return errors.New("drain lock release failed")
	}
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(5 * time.Second):
		return errors.New("inflight request exceeded drain budget")
	}
	select {
	case err := <-processes[index].done:
		if err != nil {
			return errors.New("drained process exited unsuccessfully")
		}
	case <-time.After(time.Until(start.Add(5 * time.Second))):
		return errors.New("server exceeded unchanged five-second drain budget")
	}
	var claims int
	if pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight").Scan(&claims) != nil || claims != 0 {
		return errors.New("drain left shared claims")
	}
	r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
	if err != nil || !successful(r) || r.Process != processes[1-index].identity {
		return errors.New("peer did not survive drain")
	}
	p.receipt.Measurements["SIGTERM_drain"] = time.Since(start).Milliseconds()
	p.receipt.Checks["SIGTERM_inflight_drain_claim_release_peer_survives"] = "PASS"
	return nil
}
