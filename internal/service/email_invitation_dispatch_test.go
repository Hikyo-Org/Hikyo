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
			var dials, failures atomic.Int64
			newSvc := func(mail func(context.Context) (*runtimeconfig.Bundle, error)) *EmailInvitations {
				return &EmailInvitations{DB: db, Mail: mail, MailFailed: func() { failures.Add(1) }}
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
			svc.WaitForMail()
			if got := last(); !strings.HasPrefix(got, "failure ") || !strings.Contains(got, `"mail_sent":false`) || dials.Load() != 1 || failures.Load() != 1 {
				t.Fatalf("failed send: %s dials=%d failures=%d", got, dials.Load(), failures.Load())
			}

			// A full concurrency bound records a failure without dialling.
			svc = newSvc(failing)
			svc.semOnce.Do(func() { svc.sem = make(chan struct{}, emailInvitationMailConcurrency) })
			for range emailInvitationMailConcurrency {
				svc.sem <- struct{}{}
			}
			dials.Store(0)
			failures.Store(0)
			svc.dispatch(t.Context(), receipt, "intent-full", "Org", "tok", true)
			svc.WaitForMail()
			if got := last(); !strings.HasPrefix(got, "failure ") || !strings.Contains(got, "intent-full") || dials.Load() != 0 || failures.Load() != 1 {
				t.Fatalf("full semaphore: %s dials=%d failures=%d", got, dials.Load(), failures.Load())
			}

			// An unmatched send never dials and states that no mail was sent.
			svc = newSvc(failing)
			dials.Store(0)
			failures.Store(0)
			svc.dispatch(t.Context(), receipt, "intent-unmatched", "Org", "tok", false)
			svc.WaitForMail()
			if got := last(); !strings.HasPrefix(got, "success ") || !strings.Contains(got, `"mail_sent":false`) || !strings.Contains(got, "intent-unmatched") || dials.Load() != 0 || failures.Load() != 0 {
				t.Fatalf("unmatched send: %s dials=%d failures=%d", got, dials.Load(), failures.Load())
			}

			// WaitForMail (the generation's stop join) waits for an in-flight send.
			entered, release := make(chan struct{}), make(chan struct{})
			svc = newSvc(func(context.Context) (*runtimeconfig.Bundle, error) {
				close(entered)
				<-release
				return nil, errors.New("late failure")
			})
			before := len(dispatchOutcomes(t, db))
			svc.dispatch(t.Context(), receipt, "intent-inflight", "Org", "tok", true)
			<-entered
			joined := make(chan struct{})
			go func() { svc.WaitForMail(); close(joined) }()
			select {
			case <-joined:
				t.Fatal("WaitForMail returned while a send was in flight")
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			select {
			case <-joined:
			case <-time.After(10 * time.Second):
				t.Fatal("WaitForMail never returned")
			}
			if len(dispatchOutcomes(t, db)) != before+1 {
				t.Fatal("the in-flight send did not record its outcome before the join returned")
			}
		})
	}
}
