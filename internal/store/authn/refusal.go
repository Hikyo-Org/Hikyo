package authn

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// RefusalDeadlineError reports a server-side lock/statement timeout (55P03,
// 57014) as a deadline error that still wraps the PgError. The server bound is
// the remaining time rounded down to whole milliseconds (up to ~1 ms short) and
// the Go context timer can fire late on a loaded runner, so the server may
// report first; callers classify both the same way. An administrator's
// pg_cancel_backend also yields 57014 and is labelled a deadline too; harmless,
// because the production caller discards the error.
func RefusalDeadlineError(err error) error {
	var pgErr *pgconn.PgError
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &pgErr) || (pgErr.Code != "55P03" && pgErr.Code != "57014") {
		return err
	}
	return fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
}

// UpdateRequestRefusals is the #1034-only enumerated writer. A nil sample is
// the system sealer. The caller owns a write transaction and commits every
// event, staging reset and increment together, or none of them.
func (r *Resolver) UpdateRequestRefusals(ctx context.Context, sample *audit.RefusalSample) (n int64, err error) {
	defer func() { err = RefusalDeadlineError(err) }()
	var start, now int64
	var encoded string
	if r.sq != nil {
		// sqlite needs no server-side bound: BEGIN IMMEDIATE holds the single writer
		// and no separate server outlives the in-process call.
		row, err := r.sq.ReadAuthRefusalWindow(ctx)
		if err != nil {
			return 0, err
		}
		start, now, encoded = row.WindowStart, row.NowSeconds, row.State
	} else {
		// Cancelling the client does not stop a lock wait or statement the server already
		// has, so the remaining deadline also bounds each wait/statement server-side
		// (per statement, not per transaction; 0 would disable the timeout).
		if dl, ok := ctx.Deadline(); ok {
			if err := r.pg.BoundAuthRefusalTransaction(ctx, strconv.FormatInt(max(1, time.Until(dl).Milliseconds()), 10)); err != nil {
				return 0, err
			}
		}
		row, err := r.pg.ReadAuthRefusalWindow(ctx)
		if err != nil {
			return 0, err
		}
		start, now, encoded = row.WindowStart, row.NowSeconds, row.State
	}
	if len(encoded) > audit.RefusalStateBytes || start < 0 || start%60 != 0 || now < start {
		return 0, fmt.Errorf("authn: invalid refusal staging state")
	}
	var window audit.RefusalWindow
	d := jsontext.NewDecoder(strings.NewReader(encoded))
	if err := json.UnmarshalDecode(d, &window, json.RejectUnknownMembers(true)); err != nil {
		return 0, fmt.Errorf("authn: invalid refusal staging encoding")
	}
	if _, err := d.ReadToken(); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("authn: trailing refusal staging data")
	}
	if err := window.Validate(); err != nil {
		return 0, err
	}
	if start == 0 && !window.Empty() {
		return 0, fmt.Errorf("authn: refusal counts have no window")
	}
	changed := false
	if start != 0 && now-start >= 60 && !window.Empty() {
		for _, stage := range audit.RefusalStages() {
			payload := window.RefusalPayload(stage, start)
			if payload["count"] == int64(0) {
				continue
			}
			id, err := audit.NewEventID()
			if err != nil {
				return 0, err
			}
			event := audit.Event{ID: id, Type: audit.EventRequestRefused, SchemaVersion: 1, OccurredAt: time.Unix(start+60, 0).UTC(), Actor: audit.Actor{Class: audit.ActorUnauthenticated}, Outcome: audit.OutcomeFailure, Origin: audit.OriginSystem, Payload: payload}
			if err := r.WriteAuthEvent(ctx, event, audit.TrailInstance); err != nil {
				return 0, err
			}
		}
		window = audit.NewRefusalWindow()
		start = 0
		changed = true
	}
	if sample != nil {
		if start == 0 || window.Empty() {
			start = now / 60 * 60
		}
		if err := window.Add(*sample); err != nil {
			return 0, err
		}
		changed = true
	}
	if changed {
		state, err := json.Marshal(window, jsonwire.Stable)
		if err != nil {
			return 0, err
		}
		if len(state) > audit.RefusalStateBytes {
			return 0, fmt.Errorf("authn: refusal staging exceeds bound")
		}
		if r.sq != nil {
			err = r.sq.SaveAuthRefusalWindow(ctx, sqlitegen.SaveAuthRefusalWindowParams{WindowStart: start, State: string(state)})
		} else {
			err = r.pg.SaveAuthRefusalWindow(ctx, pggen.SaveAuthRefusalWindowParams{WindowStart: start, State: string(state)})
		}
		if err != nil {
			return 0, err
		}
	}
	if window.Empty() {
		return 0, nil
	}
	return start, nil
}
