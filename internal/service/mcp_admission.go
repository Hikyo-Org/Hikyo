package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// MCPAdmission performs the shared phase-1 rate and concurrency admission.
// It first resolves and authorizes inside a read transaction, so guessed scope
// ids cannot occupy tenant capacity. The actual page service repeats both
// checks in its own transaction after admission; the principal id returned by
// the first transaction is only a limiter key and never authorization state.
type MCPAdmission struct {
	DB *store.DB
}

const (
	mcpAdmissionLeaseTTL       = 35 * time.Second
	mcpAdmissionCleanupTimeout = time.Second
)

func mcpCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), mcpAdmissionCleanupTimeout)
}

// Acquire authorizes one closed MCP read operation, then atomically charges
// its datastore-coordinated token bucket and claims 4/principal, 8/org, and
// 64/instance capacity. Release is synchronous and safe to call once.
func (s *MCPAdmission) Acquire(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope) (func() error, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("service: MCP admission unavailable")
	}
	switch op {
	case authz.OpKeyList, authz.OpEnvList, authz.OpValueList, authz.OpValuePendingList, authz.OpRevisionList:
	default:
		return nil, fmt.Errorf("service: unsupported MCP admission operation %q", op)
	}

	var principal domain.PrincipalID
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, op, scope, time.Now().UTC())
		if err == nil {
			principal = caller.Principal
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	callID, err := newID("mcp_call")
	if err != nil {
		return nil, err
	}
	if err := s.DB.Coordination().AcquireMCP(ctx, callID, string(principal), string(scope.Org), mcpAdmissionLeaseTTL); err != nil {
		if errors.Is(err, store.ErrMCPAdmissionLimited) {
			return nil, fmt.Errorf("%w: service: MCP shared admission exhausted", admission.ErrOverloaded)
		}
		return nil, err
	}
	return func() error {
		cleanupCtx, cancel := mcpCleanupContext(ctx)
		defer cancel()
		return s.DB.Coordination().ReleaseMCP(cleanupCtx, callID)
	}, nil
}
