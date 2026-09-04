package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Phase-1 pagination and traversal bounds (mcp-server ADR, Pagination and output bounds).
const (
	// PageSizeDefault, PageSizeMin, and PageSizeMax bound one page's item count.
	PageSizeDefault = 25
	PageSizeMin     = 1
	PageSizeMax     = 100
	// CursorMaxBytes bounds an accepted cursor's encoded size.
	CursorMaxBytes = 2 << 10
	// CursorTTL is the one chain expiry; continuation never renews it.
	CursorTTL = 15 * time.Minute
	// Chain bounds: one cursor chain returns at most MaxChainPages pages,
	// MaxChainItems items, and MaxChainBytes encoded structured content.
	MaxChainPages = 10
	MaxChainItems = 1000
	MaxChainBytes = 1 << 20
)

// Named, tenant-safe tool errors. They describe the cursor, a bound, or an
// argument, never a tenant fact, so the transport surfaces their exact token
// while every other failure collapses to SafeOperationError.
var (
	// ErrInvalidCursor is the single safe response to a missing, tampered,
	// expired, or scope-mismatched cursor. It never distinguishes which.
	ErrInvalidCursor = errors.New("invalid_cursor")
	// ErrTraversalLimitReached is returned when a chain bound is reached; no
	// continuation cursor accompanies it.
	ErrTraversalLimitReached = errors.New("traversal_limit_reached")
	// ErrResultItemTooLarge is returned when the next single valid item cannot
	// fit within the structured-content bound.
	ErrResultItemTooLarge = errors.New("result_item_too_large")
	// ErrInvalidArgument is the safe response to an out-of-range argument.
	ErrInvalidArgument = errors.New("invalid_argument")
)

// CursorSealer encrypts and authenticates opaque cursor payloads. It is
// injected because the crypto chokepoint confines the AEAD to internal/crypto;
// the transport holds only this narrow verb. Open returns a non-nil error for
// any tampered, truncated, or forged token, without distinguishing which.
type CursorSealer interface {
	Seal(context.Context, []byte) (string, error)
	Open(context.Context, string) ([]byte, error)
}

// publicErrorMessage returns the tenant-safe token a named error surfaces, or
// "" when the error must collapse to SafeOperationError.
func publicErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrInvalidCursor):
		return ErrInvalidCursor.Error()
	case errors.Is(err, ErrTraversalLimitReached):
		return ErrTraversalLimitReached.Error()
	case errors.Is(err, ErrResultItemTooLarge):
		return ErrResultItemTooLarge.Error()
	case errors.Is(err, ErrInvalidArgument):
		return ErrInvalidArgument.Error()
	default:
		return ""
	}
}

type cursorSealerContextKey struct{}

// withCursorSealer attaches the cursor sealer to a request context. The sealer
// derives from shared authoritative key material, never request data, so
// cursors are portable across replicas.
func withCursorSealer(ctx context.Context, sealer CursorSealer) context.Context {
	return context.WithValue(ctx, cursorSealerContextKey{}, sealer)
}

func cursorSealerFrom(ctx context.Context) (CursorSealer, bool) {
	sealer, ok := ctx.Value(cursorSealerContextKey{}).(CursorSealer)
	return sealer, ok && sealer != nil
}

// cursorState is the authenticated, opaque continuation. It binds the tool, the
// exact scope ids, the stable keyset position, the chain counters, and the one
// chain expiry. It carries no bearer, principal id, tenant name, secret
// material, or authorization claim.
type cursorState struct {
	Version  int    `json:"v"`
	Tool     string `json:"t"`
	Org      string `json:"o"`
	Project  string `json:"p"`
	Env      string `json:"e"`
	PageSize int    `json:"s"`
	// Pos is the keyset position: the last returned item's stable sort key. The
	// store query fetches strictly past it.
	Pos string `json:"k"`
	// Pages, Items, and Bytes are the cumulative chain counters checked against
	// the chain bounds before the next page is fetched.
	Pages  int   `json:"n"`
	Items  int   `json:"i"`
	Bytes  int   `json:"b"`
	Expiry int64 `json:"x"`
}

const cursorVersion = 1

// CursorScope is the tool and immutable id chain a cursor is bound to. A
// continuation whose scope does not match the presented request is rejected as
// one safe invalid-cursor error.
type CursorScope struct {
	Tool     string
	Org      string
	Project  string
	Env      string
	PageSize int
}

func (s CursorScope) matches(c cursorState) bool {
	return c.Version == cursorVersion && c.Tool == s.Tool &&
		c.Org == s.Org && c.Project == s.Project && c.Env == s.Env && c.PageSize == s.PageSize
}

// encodeCursor authenticates and encodes a continuation.
func encodeCursor(ctx context.Context, sealer CursorSealer, state cursorState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return sealer.Seal(ctx, payload)
}

// decodeCursor opens a presented continuation, checking its authentication
// (via the sealer), scope binding, and chain expiry. Every failure
// returns the one ErrInvalidCursor; none distinguishes which check failed.
func decodeCursor(ctx context.Context, sealer CursorSealer, scope CursorScope, raw string, now time.Time) (cursorState, error) {
	if len(raw) > CursorMaxBytes {
		return cursorState{}, ErrInvalidCursor
	}
	payload, err := sealer.Open(ctx, raw)
	if err != nil {
		return cursorState{}, ErrInvalidCursor
	}
	var state cursorState
	if err := json.Unmarshal(payload, &state); err != nil {
		return cursorState{}, ErrInvalidCursor
	}
	if !scope.matches(state) || now.UnixMilli() >= state.Expiry {
		return cursorState{}, ErrInvalidCursor
	}
	return state, nil
}
