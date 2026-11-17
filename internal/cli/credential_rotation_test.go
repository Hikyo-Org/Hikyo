package cli_test

import (
	"io"
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
		line := r.Method + " " + r.URL.RequestURI()
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			line += " " + string(body)
		}
		w.mu.Lock()
		w.seen = append(w.seen, line)
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == rotationBase+"/runs":
			revision, converged, code := int64(7), int64(2), "provider_unreachable"
			_ = jsonwire.Encode(rw, apigen.RotationRunList{
				Items: []apigen.RotationRun{{
					Id: "rrn_70", Trigger: apigen.RotationRunTriggerManual, Phase: apigen.RotationRunPhaseDone,
					PublishedRevision: &revision, NotConvergedAdapterTargets: &converged, ErrorCode: &code, RevocationDelaySeconds: 3600,
					StartedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), PhaseAt: time.Date(2030, 1, 2, 4, 4, 5, 0, time.UTC),
				}},
				NextAfter: &[]string{"rrn_69"}[0],
			})
		case r.Method == http.MethodPost && r.URL.Path == rotationBase+"/run-now":
			rw.WriteHeader(http.StatusAccepted)
			_ = jsonwire.Encode(rw, apigen.RotationRunRequested{RotationId: "rot_70", RunId: "rrn_71", SkipOverlap: true})
		case r.Method == http.MethodGet && r.URL.Path == rotationBase:
			lapses := time.Date(2030, 1, 3, 0, 0, 0, 0, time.UTC)
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active", RestoreHold: true, CredentialExpiresAt: &lapses})
		case r.Method == http.MethodPost && r.URL.Path == rotationBase+"/release-hold":
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active"})
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

// run-now publishes a new credential off the schedule, so the CLI refuses every
// run-now without the explicit confirmation, before any request that could
// consume a ceremony; the server enforces the same field.
func TestRotationRunNowNeedsConfirmation(t *testing.T) {
	for _, extra := range [][]string{nil, {"--skip-overlap"}} {
		var wire rotationWire
		ios, _, stderr := definitionsTestIO(t, wire.handler(t))
		args := append(append([]string{"rotation", "run-now", "rot_70"}, extra...), rotationScope...)
		if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
			t.Fatalf("%v: exit %d, want usage; stderr=%s", extra, code, stderr)
		}
		if !strings.Contains(stderr.String(), "--confirm-destructive") {
			t.Fatalf("%v: stderr = %q", extra, stderr)
		}
		if seen := wire.take(); len(seen) != 0 {
			t.Fatalf("%v: requests before the refusal: %v", extra, seen)
		}
	}
}

// A second rotation id is refused before any login or request, on every verb:
// run-now would otherwise publish for the first id and silently drop the rest,
// and list and create take no positional at all.
func TestRotationRefusesSurplusPositionals(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		message string
	}{
		{"run-now", []string{"run-now", "rot_70", "rot_71", "--confirm-destructive", "--skip-overlap"}, "rotation run-now takes one rotation id, got 2"},
		{"pause", []string{"pause", "rot_70", "rot_71"}, "rotation pause takes one rotation id, got 2"},
		{"resume", []string{"resume", "rot_70", "rot_71"}, "rotation resume takes one rotation id, got 2"},
		{"release-hold", []string{"release-hold", "rot_70", "rot_71"}, "rotation release-hold takes one rotation id, got 2"},
		{"show", []string{"show", "rot_70", "rot_71"}, "rotation show takes one rotation id, got 2"},
		{"runs", []string{"runs", "rot_70", "rot_71"}, "rotation runs takes one rotation id, got 2"},
		{"delete", []string{"delete", "rot_70", "rot_71", "--abandon"}, "rotation delete takes one rotation id, got 2"},
		{"list", []string{"list", "rot_70"}, "rotation list takes no positional arguments"},
		{"create", []string{"create", "rot_70", "--provider", "p", "--key", "k", "--username-key", "u", "--interval", "30d", "--revocation-delay", "1d"}, "rotation create takes no positional arguments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wire rotationWire
			ios, _, stderr := definitionsTestIO(t, wire.handler(t))
			args := append(append([]string{"rotation"}, tc.args...), rotationScope...)
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
				t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
			}
			if !strings.Contains(stderr.String(), tc.message) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.message)
			}
			if seen := wire.take(); len(seen) != 0 {
				t.Fatalf("requests before the refusal: %v", seen)
			}
		})
	}
}

// The confirmation travels in the body as confirm_destructive, next to skip_overlap.
func TestRotationRunNowSendsTheConfirmation(t *testing.T) {
	var wire rotationWire
	ios, _, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "run-now", "rot_70", "--confirm-destructive", "--skip-overlap"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	seen := wire.take()
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "POST "+rotationBase+"/run-now ") ||
		!strings.Contains(seen[0], `"confirm_destructive":true`) || !strings.Contains(seen[0], `"skip_overlap":true`) {
		t.Fatalf("requests = %v", seen)
	}
}

func TestRotationRunsListsMetadataOnly(t *testing.T) {
	var wire rotationWire
	ios, stdout, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "runs", "rot_70", "--limit", "1", "--after", "rrn_72"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if seen := wire.take(); len(seen) != 2 || seen[0] != "GET "+rotationBase+"/runs?after=rrn_72&limit=1" || seen[1] != "GET "+rotationBase {
		t.Fatalf("requests = %v", seen)
	}
	// A held rotation is said loudly before the runs, with the lapse time and the way out.
	for _, want := range []string{"RESTORE HOLD", "2030-01-03T00:00:00Z", "hikyo rotation release-hold rot_70"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("no hold notice %q: %q", want, stderr)
		}
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

// release-hold posts to its own route with the optional generation, and says
// nothing about the hold once the server reports it gone.
func TestRotationReleaseHoldPostsToItsRoute(t *testing.T) {
	var wire rotationWire
	ios, _, stderr := definitionsTestIO(t, wire.handler(t))
	args := append([]string{"rotation", "release-hold", "rot_70", "--expected-generation", "3"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	seen := wire.take()
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "POST "+rotationBase+"/release-hold ") || !strings.Contains(seen[0], `"expected_generation":3`) {
		t.Fatalf("requests = %v", seen)
	}
	if strings.Contains(stderr.String(), "RESTORE HOLD") {
		t.Errorf("hold notice after release: %q", stderr)
	}
	if !strings.Contains(stderr.String(), "may now drop lingering unpublished candidate roles") {
		t.Errorf("release says nothing of the candidate drops: %q", stderr)
	}
}
