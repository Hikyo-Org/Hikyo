package authn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// UpdateRequestRefusals is the #1034-only enumerated writer. A nil sample is
// the system sealer. The caller owns a write transaction and commits every
// event, staging reset and increment together, or none of them.
func (r *Resolver) UpdateRequestRefusals(ctx context.Context, sample *audit.RefusalSample) (int64, error) {
	var start, now int64
	var encoded string
	if r.sq != nil {
		row, err := r.sq.ReadAuthRefusalWindow(ctx)
		if err != nil {
			return 0, err
		}
		start, now, encoded = row.WindowStart, row.NowSeconds, row.State
	} else {
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
	d := json.NewDecoder(strings.NewReader(encoded))
	d.DisallowUnknownFields()
	if err := d.Decode(&window); err != nil {
		return 0, fmt.Errorf("authn: invalid refusal staging encoding")
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
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
		state, err := json.Marshal(window)
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
