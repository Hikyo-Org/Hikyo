package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// outcomes returns "<outcome> <payload>" for every dispatch mail outcome.
func dispatchOutcomes(t *testing.T, db *store.DB) []string {
	t.Helper()
	const q = `SELECT outcome || ' ' || payload FROM audit_instance_events WHERE type = 'member.email_invitation_mail_outcome' ORDER BY seq`
	var out []string
	if db.Engine() == store.EnginePostgres {
		rows, err := db.PG().Query(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	rows, err := db.SQLiteRead().QueryContext(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestEmailInvitationDispatchOutcomes(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			db, err := openServiceFixture(t, preparationDatabase(t, engine))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			receipt := EmailInvitationReceipt{ID: "ein_test", Email: "a@example.test", ExpiresAt: time.Now().Add(time.Hour)}
			var dials atomic.Int64
			newSvc := func(mail func(context.Context) (*runtimeconfig.Bundle, error)) *EmailInvitations {
				return &EmailInvitations{DB: db, Mail: mail, Sends: NewMailSends()}
			}
			failing := func(context.Context) (*runtimeconfig.Bundle, error) {
				dials.Add(1)
				return nil, errors.New("smtp down")
			}
			last := func() string {
				all := dispatchOutcomes(t, db)
				if len(all) == 0 {
					t.Fatal("no outcome recorded")
				}
				return all[len(all)-1]
			}

			// A failed send records a failure and counts it.
			svc := newSvc(failing)
			svc.dispatch(t.Context(), receipt, "intent-failed", "Org", "tok", true)
			svc.Sends.Wait()
			if got := last(); !strings.HasPrefix(got, "failure ") || !strings.Contains(got, `"mail_sent":false`) || dials.Load() != 1 || svc.Sends.Failures() != 1 {
				t.Fatalf("failed send: %s dials=%d failures=%v", got, dials.Load(), svc.Sends.Failures())
			}

			// A matched send in flight does not change whether, or how many,
			// other sends are attempted.
			const others = 8
			blocked, unblock := make(chan struct{}), make(chan struct{})
			dials.Store(0)
			svc = newSvc(func(context.Context) (*runtimeconfig.Bundle, error) {
				if dials.Add(1) == 1 {
					close(blocked)
					<-unblock
				}
				return nil, errors.New("smtp down")
			})
			svc.dispatch(t.Context(), receipt, "intent-held", "Org", "tok", true)
			<-blocked
			for i := range others {
				svc.dispatch(t.Context(), receipt, "intent-other-"+string(rune('a'+i)), "Org", "tok", true)
			}
			deadline := time.Now().Add(10 * time.Second)
			for dials.Load() != 1+others && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if got := dials.Load(); got != 1+others {
				t.Fatalf("dials with one send in flight = %d, want %d", got, 1+others)
			}
			close(unblock)
			svc.Sends.Wait()
			if got := svc.Sends.Failures(); got != 1+others {
				t.Fatalf("failures = %v, want %d", got, 1+others)
			}

			// Stopping cancels a blocked send and records its failure outcome.
			svc = newSvc(func(ctx context.Context) (*runtimeconfig.Bundle, error) {
				dials.Add(1)
				<-ctx.Done()
				return nil, ctx.Err()
			})
			dials.Store(0)
			before := len(dispatchOutcomes(t, db))
			svc.dispatch(t.Context(), receipt, "intent-stopped", "Org", "tok", true)
			for deadline := time.Now().Add(10 * time.Second); dials.Load() != 1; time.Sleep(5 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the blocked send never dialled")
				}
			}
			stopped := make(chan struct{})
			go func() { svc.Sends.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("Stop did not cancel the blocked send")
			}
			if got := last(); !strings.HasPrefix(got, "failure ") || !strings.Contains(got, "intent-stopped") || len(dispatchOutcomes(t, db)) != before+1 || svc.Sends.Failures() != 1 {
				t.Fatalf("stopped send: %s", got)
			}

			// An unmatched send never dials and states that no mail was sent.
			svc = newSvc(failing)
			dials.Store(0)
			svc.dispatch(t.Context(), receipt, "intent-unmatched", "Org", "tok", false)
			svc.Sends.Wait()
			if got := last(); !strings.HasPrefix(got, "success ") || !strings.Contains(got, `"mail_sent":false`) || !strings.Contains(got, "intent-unmatched") || dials.Load() != 0 || svc.Sends.Failures() != 0 {
				t.Fatalf("unmatched send: %s dials=%d failures=%v", got, dials.Load(), svc.Sends.Failures())
			}

			// Wait waits for an in-flight send.
			entered, release := make(chan struct{}), make(chan struct{})
			svc = newSvc(func(context.Context) (*runtimeconfig.Bundle, error) {
				close(entered)
				<-release
				return nil, errors.New("late failure")
			})
			before = len(dispatchOutcomes(t, db))
			svc.dispatch(t.Context(), receipt, "intent-inflight", "Org", "tok", true)
			<-entered
			joined := make(chan struct{})
			go func() { svc.Sends.Wait(); close(joined) }()
			select {
			case <-joined:
				t.Fatal("Wait returned while a send was in flight")
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			select {
			case <-joined:
			case <-time.After(10 * time.Second):
				t.Fatal("Wait never returned")
			}
			if len(dispatchOutcomes(t, db)) != before+1 {
				t.Fatal("the in-flight send did not record its outcome before the join returned")
			}
		})
	}
}
