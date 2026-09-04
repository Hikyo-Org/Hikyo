package mcpserver

import (
	"context"
	"errors"
	"time"
)

// PageSpec is one bounded page request the transport resolves on a tool's
// behalf. Row is the service result element; Elem is the whitelisted output
// element the tool emits. The tool supplies the fetch, the row-to-element
// mapping, and the stable keyset accessor; the transport owns cursor
// authentication, chain bounds, and the output byte fit.
type PageSpec[Row, Elem any] struct {
	Scope    CursorScope
	Cursor   string
	PageSize int
	// Fetch returns up to limit rows strictly past the keyset position after
	// ("" for the first page), in the tool's stable order.
	Fetch func(ctx context.Context, after string, limit int) ([]Row, error)
	// Map projects one service row into its whitelisted output element.
	Map func(Row) (Elem, error)
	// Key returns one row's stable keyset position (the cursor advances on it).
	Key func(Row) string
	// StructuredSize returns the exact encoded structuredContent byte count for
	// these elements and cursor, including addressed ids and other tool fields.
	StructuredSize func([]Elem, string) (int, error)
}

// Paginate resolves one page under the phase-1 cursor, chain, and output-byte
// bounds. It authenticates the presented cursor against the scope, refuses when
// a chain bound is reached, fetches one row past the page to detect a
// continuation, and includes only the elements that fit the structured-content
// bound. It returns the page's elements and, when a continuation exists, an
// authenticated next cursor; the caller wraps them in the tool's output.
func Paginate[Row, Elem any](ctx context.Context, spec PageSpec[Row, Elem]) ([]Elem, string, error) {
	sealer, ok := cursorSealerFrom(ctx)
	if !ok {
		return nil, "", errors.New("mcpserver: cursor sealer unavailable")
	}
	if spec.PageSize < PageSizeMin || spec.PageSize > PageSizeMax || spec.Fetch == nil || spec.Map == nil || spec.Key == nil || spec.StructuredSize == nil {
		return nil, "", ErrInvalidArgument
	}
	spec.Scope.PageSize = spec.PageSize
	now := time.Now().UTC()

	var state cursorState
	if spec.Cursor == "" {
		state = cursorState{
			Version: cursorVersion, Tool: spec.Scope.Tool,
			Org: spec.Scope.Org, Project: spec.Scope.Project, Env: spec.Scope.Env, PageSize: spec.PageSize,
			Expiry: now.Add(CursorTTL).UnixMilli(),
		}
	} else {
		decoded, err := decodeCursor(ctx, sealer, spec.Scope, spec.Cursor, now)
		if err != nil {
			return nil, "", err
		}
		state = decoded
	}

	// Chain bounds are checked before the next page is fetched, so a probe
	// cannot spend tenant work past the cap.
	if state.Pages >= MaxChainPages || state.Items >= MaxChainItems || state.Bytes >= MaxChainBytes {
		return nil, "", ErrTraversalLimitReached
	}

	remainingItems := MaxChainItems - state.Items
	fetchLimit := min(spec.PageSize, remainingItems) + 1
	rows, err := spec.Fetch(ctx, state.Pos, fetchLimit)
	if err != nil {
		return nil, "", err
	}
	candidateCount := min(spec.PageSize, remainingItems)
	morePeeked := len(rows) > candidateCount
	if morePeeked {
		rows = rows[:candidateCount]
	}

	elements := make([]Elem, 0, len(rows))
	for _, row := range rows {
		elem, err := spec.Map(row)
		if err != nil {
			return nil, "", err
		}
		elements = append(elements, elem)
	}

	if len(elements) == 0 {
		size, err := spec.StructuredSize(elements, "")
		if err != nil {
			return nil, "", err
		}
		if size > MaxStructuredContentBytes {
			return nil, "", ErrResultItemTooLarge
		}
		if state.Bytes+size > MaxChainBytes {
			return nil, "", ErrTraversalLimitReached
		}
		return elements, "", nil
	}

	for count := len(elements); count > 0; count-- {
		hasMore := morePeeked || count < len(elements)
		if !hasMore {
			size, err := spec.StructuredSize(elements[:count], "")
			if err != nil {
				return nil, "", err
			}
			if size <= MaxStructuredContentBytes && state.Bytes+size <= MaxChainBytes {
				return elements[:count], "", nil
			}
			if count == 1 {
				if size > MaxStructuredContentBytes {
					return nil, "", ErrResultItemTooLarge
				}
				return nil, "", ErrTraversalLimitReached
			}
			continue
		}

		next := cursorState{
			Version: cursorVersion, Tool: spec.Scope.Tool,
			Org: spec.Scope.Org, Project: spec.Scope.Project, Env: spec.Scope.Env, PageSize: spec.PageSize,
			Pos: spec.Key(rows[count-1]), Pages: state.Pages + 1,
			Items: state.Items + count, Bytes: state.Bytes, Expiry: state.Expiry,
		}
		cursor, size, err := cursorWithExactSize(ctx, sealer, next, elements[:count], spec.StructuredSize)
		if err != nil {
			return nil, "", err
		}
		// A cursor that already carries a terminal counter is unusable. The
		// mcp-server ADR requires the current call to refuse with no cursor when
		// more data exists and this page would reach a chain bound.
		if next.Pages >= MaxChainPages || next.Items >= MaxChainItems || state.Bytes+size >= MaxChainBytes {
			return nil, "", ErrTraversalLimitReached
		}
		if size <= MaxStructuredContentBytes && state.Bytes+size <= MaxChainBytes {
			return elements[:count], cursor, nil
		}
		if count == 1 {
			if size > MaxStructuredContentBytes {
				return nil, "", ErrResultItemTooLarge
			}
			return nil, "", ErrTraversalLimitReached
		}
	}
	return nil, "", ErrTraversalLimitReached
}

func cursorWithExactSize[Elem any](ctx context.Context, sealer CursorSealer, state cursorState, elements []Elem, sizeOf func([]Elem, string) (int, error)) (string, int, error) {
	previousBytes := state.Bytes
	for range 8 {
		cursor, err := encodeCursor(ctx, sealer, state)
		if err != nil {
			return "", 0, err
		}
		size, err := sizeOf(elements, cursor)
		if err != nil {
			return "", 0, err
		}
		total := previousBytes + size
		if total == state.Bytes {
			return cursor, size, nil
		}
		state.Bytes = total
	}
	return "", 0, errors.New("mcpserver: cursor byte count did not stabilize")
}
