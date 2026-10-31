package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func refusalStorageSample(t *testing.T) audit.RefusalSample {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return audit.RefusalSample{Stage: audit.RefusalArtifact, SourceIP: "192.0.2.103", RequestID: id.String(), Operation: "whoami"}
}

func expireRefusalWindow(t *testing.T, db *store.DB) {
	t.Helper()
	start := queryInt(t, db, "SELECT window_start FROM auth_refusal_window WHERE id=1")
	if start == 0 {
		t.Fatal("no pending window")
	}
	execRaw(t, db, fmt.Sprintf("UPDATE auth_refusal_window SET window_start=%d WHERE id=1", start-60))
}

// runRefusalAuditLifecycle stages a durable fixture sample and runs the actual
// recovery sealer so registry closure requires a committed refusal event.
func runRefusalAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	before := sealedRefusalCount(t, db)
	sample := refusalStorageSample(t)
	// Registry closure needs one durable fixture sample. Runtime Record has an
	// approved 100 ms accounting budget and may return an unconfirmed result;
	// it is not a fixture-construction guarantee under race instrumentation.
	// Use its canonical transactional writer, then exercise the real sealer.
	if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.UpdateRequestRefusals(ctx, &sample)
		return err
	}); err != nil {
		t.Fatalf("stage refusal emitter fixture: %v", err)
	}
	expireRefusalWindow(t, db)
	waitForRefusalSeal(t, db, service.NewRefusalSignals(db), before+1)
}

func assertRetainedRefusalRequest(t *testing.T, db *store.DB, requestID, source string) {
	t.Helper()
	if requestID == "" {
		t.Fatal("no server correlation")
	}
	page, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root, store.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Type != audit.EventRequestRefused {
			continue
		}
		var p struct {
			Samples []struct {
				SourceIP  string `json:"source_ip"`
				RequestID string `json:"request_id"`
			} `json:"samples"`
		}
		if err := json.Unmarshal([]byte(event.RawPayload), &p); err != nil {
			t.Fatal(err)
		}
		for _, sample := range p.Samples {
			if sample.RequestID == requestID && sample.SourceIP == source {
				return
			}
		}
	}
	t.Fatal("request missing from supported instance reader")
}

func TestRequestRefusalRevokedReplayIsRetainedAndCorrelated(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		handler := server.New(&server.API{Auth: auth, Refusals: signals}, nil)
		token := refusalSession(t, db, crypto.ArtifactCLISession, "revoked")
		r := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		r.RemoteAddr = "192.0.2.103:12345"
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Forwarded-For", "198.51.100.100")
		r.Header.Set(server.RefusalRequestIDHeader, "spoofed-request")
		r.Header.Set("X-Request-ID", "spoofed-request")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("replay: %d", w.Code)
		}
		requestID := w.Header().Get(server.RefusalRequestIDHeader)
		if _, err := uuid.Parse(requestID); err != nil || requestID == "spoofed-request" {
			t.Fatal("untrusted correlation")
		}
		expireRefusalWindow(t, db)
		// A fresh service object recovers durable pending data without a replay.
		restarted := service.NewRefusalSignals(db)
		if err := restarted.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		page, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root, store.AuditFilter{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range page.Events {
			if event.Type != audit.EventRequestRefused {
				continue
			}
			var p struct {
				Count   int64 `json:"count"`
				Samples []struct {
					SourceIP  string `json:"source_ip"`
					RequestID string `json:"request_id"`
				} `json:"samples"`
			}
			if err := json.Unmarshal([]byte(event.RawPayload), &p); err != nil {
				t.Fatal(err)
			}
			for _, sample := range p.Samples {
				if sample.RequestID == requestID && sample.SourceIP == "192.0.2.103" && p.Count == 1 {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("revoked replay missing from supported instance reader")
		}
	})
}

func TestRequestRefusalFailedDedicatedCommitDoesNotSuppressFallback(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		h := server.New(&server.API{Auth: auth, Settings: &service.ProjectSettings{DB: db, Auth: auth}, Refusals: signals}, nil)
		token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloper)
		if err != nil {
			t.Fatal(err)
		}
		fail := authn.SetMutationFailureObserver(func(q string) error {
			if strings.Contains(q, "InsertInstanceAuditEvent") {
				return errors.New("synthetic dedicated commit failure")
			}
			return nil
		})
		path := api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/settings"
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "192.0.2.103:1234"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		fail()
		// The dedicated owner still fails closed. A tentative capture cannot
		// suppress the independently durable generic fallback on that response.
		if w.Code != http.StatusInternalServerError || signals.RefusalHealth().PendingSince == 0 {
			t.Fatalf("failed dedicated write suppressed fallback: %d %+v", w.Code, signals.RefusalHealth())
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'"); got != 0 {
			t.Fatalf("failed dedicated event appeared committed: %d", got)
		}
		expireRefusalWindow(t, db)
		if err := signals.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertRetainedRefusalRequest(t, db, w.Header().Get(server.RefusalRequestIDHeader), "192.0.2.103")
	})
}

func TestRequestRefusalFailedSealRetainsPendingAndSinkFailurePreservesWire(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := service.NewRefusalSignals(db)
		if err := s.Record(t.Context(), refusalStorageSample(t)); err != nil {
			t.Fatal(err)
		}
		expireRefusalWindow(t, db)
		before := queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1")
		start := queryInt(t, db, "SELECT window_start FROM auth_refusal_window WHERE id=1")
		fail := authn.SetMutationFailureObserver(func(q string) error {
			if strings.Contains(q, "InsertInstanceAuditEvent") {
				return errors.New("synthetic seal failure")
			}
			return nil
		})
		err := s.Record(t.Context(), refusalStorageSample(t))
		fail()
		if err == nil || s.RefusalHealth().Healthy || s.RefusalHealth().Failures != 1 {
			t.Fatal("failed seal claimed healthy accounting")
		}
		if before != queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1") || start != queryInt(t, db, "SELECT window_start FROM auth_refusal_window WHERE id=1") {
			t.Fatal("failed seal overwrote pending evidence")
		}
		if err := s.Seal(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.request_refused'"); got != 1 {
			t.Fatalf("seal lost or duplicated: %d", got)
		}
		auth := authService(t, db)
		handler := server.New(&server.API{Auth: auth, Refusals: s}, nil)
		fail = authn.SetMutationFailureObserver(func(q string) error {
			if strings.Contains(q, "SaveAuthRefusalWindow") {
				return errors.New("synthetic sink failure")
			}
			return nil
		})
		r := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		r.RemoteAddr = "192.0.2.103:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		fail()
		if w.Code != http.StatusUnauthorized || s.RefusalHealth().Healthy || s.RefusalHealth().Failures != 2 {
			t.Fatalf("sink failure changed refusal or hid failure: %d %+v", w.Code, s.RefusalHealth())
		}
	})
}

func TestRequestRefusalSharedNodesCountEveryCommittedUpdate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		nodes := []*service.RefusalSignals{service.NewRefusalSignals(db), service.NewRefusalSignals(db)}
		var wg sync.WaitGroup
		results := make(chan error, 64)
		for i := 0; i < 64; i++ {
			sample := refusalStorageSample(t)
			wg.Add(1)
			go func() { defer wg.Done(); results <- nodes[i%2].Record(context.Background(), sample) }()
		}
		wg.Wait()
		close(results)
		var accepted int64
		for err := range results {
			if err == nil {
				accepted++
			}
		}
		if accepted == 0 {
			t.Fatal("no progress under bounded contention")
		}
		committed := assertRefusalCommittedAccounting(t, db, accepted, 64)
		if uint64(64-accepted) != nodes[0].RefusalHealth().Failures+nodes[1].RefusalHealth().Failures {
			t.Fatal("unaccounted failures")
		}
		expireRefusalWindow(t, db)
		seals := make(chan error, 2)
		wg.Add(2)
		for _, node := range nodes {
			go func() { defer wg.Done(); seals <- node.Seal(t.Context()) }()
		}
		wg.Wait()
		close(seals)
		var failedSeals uint64
		for err := range seals {
			if err != nil {
				failedSeals++
			}
		}
		// Contending transactions can exhaust the approved 100 ms budget.
		// Those calls must be visible as unconfirmed accounting, while a quiet
		// worker recovers the same pending window without losing or resealing it.
		if got := nodes[0].RefusalHealth().Failures + nodes[1].RefusalHealth().Failures; got != uint64(64-accepted)+failedSeals {
			t.Fatalf("concurrent seal failure accounting: %d want %d", got, uint64(64-accepted)+failedSeals)
		}
		waitForRefusalSeal(t, db, service.NewRefusalSignals(db), committed)
		if got := sealedRefusalCount(t, db); got != committed {
			t.Fatalf("shared seal lost or duplicated committed counts: %d want %d", got, committed)
		}
	})
}

func assertRefusalCommittedAccounting(t *testing.T, db *store.DB, accepted, attempts int64) int64 {
	t.Helper()
	window, sealed := refusalAccountingSnapshot(t, db)
	committed := window.Counts[0] + sealed
	// A failed COMMIT acknowledgement is unconfirmed accounting, not proof
	// of rollback. Aggregate committed counts must cover acknowledged updates
	// without exceeding the attempted count. Sealing preserves that exact total.
	if committed < accepted || committed > attempts || len(window.Samples) > 32 {
		t.Fatalf("shared count %d accepted %d attempts %d samples %d", committed, accepted, attempts, len(window.Samples))
	}
	return committed
}

// refusalAccountingSnapshot owns engine dispatch, commit-settlement fencing
// and validation; each caller applies its own stage or aggregate bounds.
func refusalAccountingSnapshot(t *testing.T, db *store.DB) (audit.RefusalWindow, int64) {
	t.Helper()
	var window audit.RefusalWindow
	state := ""
	var sealed int64
	if db.Engine() == store.EnginePostgres {
		var err error
		state, sealed, err = readPostgresRefusalAccounting(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		state = queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1")
		sealed = sealedRefusalCount(t, db)
	}
	if err := json.Unmarshal([]byte(state), &window); err != nil {
		t.Fatal(err)
	}
	if err := window.Validate(); err != nil {
		t.Fatal(err)
	}
	return window, sealed
}

func readPostgresRefusalAccounting(ctx context.Context, db *store.DB) (string, int64, error) {
	// Caller completion does not join an unacknowledged server-side COMMIT.
	// The producer's singleton writer lock fences that settlement, and keeps
	// staging and sealed counts coherent while the baseline is captured.
	tx, err := db.PG().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)
	var state string
	if err := tx.QueryRow(ctx, "SELECT state FROM auth_refusal_window WHERE id=1 FOR UPDATE").Scan(&state); err != nil {
		return "", 0, err
	}
	var sealed int64
	if err := tx.QueryRow(ctx, "SELECT COALESCE(SUM((payload::jsonb->>'count')::bigint),0) FROM audit_instance_events WHERE type='auth.request_refused'").Scan(&sealed); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return state, sealed, nil
}

func TestRequestRefusalUnavailableDatabaseKeepsEarlyRefusal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := service.NewRefusalSignals(db)
		h := server.New(&server.API{Refusals: s}, nil)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		r.RemoteAddr = "192.0.2.103:1234"
		r.Header.Set("Authorization", "Bearer CANARY-BEARER")
		r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: "CANARY-COOKIE"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || s.RefusalHealth().Healthy || s.RefusalHealth().Failures != 1 {
			t.Fatalf("sink outage changed refusal or hid accounting uncertainty: %d %+v", w.Code, s.RefusalHealth())
		}
	})
}

// holdRefusalWriter holds the real engine writer until explicit release or
// test cleanup; both release paths share exactly one rollback owner.
func holdRefusalWriter(t *testing.T, db *store.DB) func() {
	t.Helper()
	var rollback func()
	if db.Engine() == store.EngineSQLite {
		held, err := db.BeginSQLite(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		rollback = func() { _ = held.Rollback() }
	} else {
		held, err := db.BeginPostgres(t.Context(), pgx.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := held.Exec(t.Context(), "SELECT id FROM auth_refusal_window WHERE id=1 FOR UPDATE"); err != nil {
			_ = held.Rollback(t.Context())
			t.Fatal(err)
		}
		rollback = func() { _ = held.Rollback(t.Context()) }
	}
	release := sync.OnceFunc(rollback)
	t.Cleanup(release)
	return release
}

func TestRequestRefusalBlockedDatastoreHonorsDeadline(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		release := holdRefusalWriter(t, db)
		signals := service.NewRefusalSignals(db)
		start := time.Now()
		err := signals.Record(t.Context(), refusalStorageSample(t))
		elapsed := time.Since(start)
		release()
		if err == nil || elapsed > time.Second || signals.RefusalHealth().Failures != 1 || signals.RefusalHealth().InFlight != 0 {
			t.Fatalf("blocked sink escaped deadline/ownership: elapsed=%s err=%v health=%+v", elapsed, err, signals.RefusalHealth())
		}
		if state := queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1"); state != `{"counts":[0,0,0,0,0],"samples":[]}` {
			t.Fatal("timed-out blocked update committed after returning")
		}
	})
}

func sealedRefusalCount(t *testing.T, db *store.DB) int64 {
	t.Helper()
	if db.Engine() == store.EnginePostgres {
		return queryInt(t, db, "SELECT COALESCE(SUM((payload::jsonb->>'count')::bigint),0) FROM audit_instance_events WHERE type='auth.request_refused'")
	}
	return queryInt(t, db, "SELECT COALESCE(SUM(json_extract(payload,'$.count')),0) FROM audit_instance_events WHERE type='auth.request_refused'")
}

func TestRequestRefusalWorkerRecoversQuietPendingWindow(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := service.NewRefusalSignals(db)
		if err := s.Record(t.Context(), refusalStorageSample(t)); err != nil {
			t.Fatal(err)
		}
		expireRefusalWindow(t, db)
		waitForRefusalSeal(t, db, service.NewRefusalSignals(db), 1)
	})
}

// waitForRefusalSeal exercises startup and timer recovery through the real worker.
// Every individual attempt retains its production deadline; the worker is joined.
func waitForRefusalSeal(t *testing.T, db *store.DB, restarted *service.RefusalSignals, want int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); restarted.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		health := restarted.RefusalHealth()
		if health.Healthy && health.PendingSince == 0 {
			if got := sealedRefusalCount(t, db); got != want {
				t.Fatalf("quiet worker lost or duplicated evidence: %d want %d", got, want)
			}
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("startup sealer did not recover pending evidence: %+v", health)
		}
	}
}
