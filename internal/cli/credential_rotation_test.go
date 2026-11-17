package cli_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

const rotationBase = "/api/v1/orgs/org_70/projects/prj_70/environments/env_70/rotations/rot_70"

var rotationScope = []string{"--org", "org_70", "--project", "prj_70", "--env", "env_70", "--instance", "local"}

// rotationWire serves the rotation routes the run-now, runs and delete verbs
// touch and records every request line with its query.
type rotationWire struct {
	mu   sync.Mutex
	seen []string
}

func (w *rotationWire) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		w.seen = append(w.seen, r.Method+" "+r.URL.RequestURI())
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == rotationBase+"/runs":
			revision, converged, code := int64(7), int64(2), "provider_unreachable"
			_ = jsonwire.Encode(rw, apigen.RotationRunList{
				Items: []apigen.RotationRun{{
					Id: "rrn_70", Trigger: apigen.RotationRunTriggerManual, Phase: apigen.RotationRunPhaseDone,
					PublishedRevision: &revision, NotConvergedConsumers: &converged, ErrorCode: &code, RevocationDelaySeconds: 3600,
					StartedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), PhaseAt: time.Date(2030, 1, 2, 4, 4, 5, 0, time.UTC),
				}},
				NextAfter: &[]string{"rrn_69"}[0],
			})
		case r.Method == http.MethodPost && r.URL.Path == rotationBase+"/run-now":
			rw.WriteHeader(http.StatusAccepted)
			_ = jsonwire.Encode(rw, apigen.RotationRunRequested{RotationId: "rot_70", RunId: "rrn_71", SkipOverlap: true})
		case r.Method == http.MethodDelete && r.URL.Path == rotationBase:
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "disabled"})
		default:
			http.NotFound(rw, r)
		}
	})
}

func (w *rotationWire) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.seen
	w.seen = nil
	return out
}

// run-now --skip-overlap revokes at once, so the CLI refuses it without the
// explicit confirmation, before any request that could consume a ceremony.
func TestRotationRunNowSkipOverlapNeedsConfirmation(t *testing.T) {
	var wire rotationWire
	ios, _, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "run-now", "rot_70", "--skip-overlap"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
		t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr.String(), "--confirm-destructive") {
		t.Fatalf("stderr = %q", stderr)
	}
	if seen := wire.take(); len(seen) != 0 {
		t.Fatalf("requests before the refusal: %v", seen)
	}
}

func TestRotationRunsListsMetadataOnly(t *testing.T) {
	var wire rotationWire
	ios, stdout, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "runs", "rot_70", "--limit", "1", "--after", "rrn_72"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if seen := wire.take(); len(seen) != 1 || seen[0] != "GET "+rotationBase+"/runs?after=rrn_72&limit=1" {
		t.Fatalf("requests = %v", seen)
	}
	for _, want := range []string{"rrn_70", "manual", "done", "provider_unreachable", "2", "1h0m0s"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("runs table lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr.String(), "--after rrn_69") {
		t.Errorf("no continuation hint: %q", stderr)
	}
}

func TestRotationDeleteAbandonRidesTheQuery(t *testing.T) {
	var wire rotationWire
	ios, _, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "delete", "rot_70", "--abandon", "--expected-generation", "3"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if seen := wire.take(); len(seen) != 1 || seen[0] != "DELETE "+rotationBase+"?abandon=true&expected_generation=3" {
		t.Fatalf("requests = %v", seen)
	}
}
