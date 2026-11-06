package securitymaintenance

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
)

func TestPublicStatusFollowsEverySavedStatusWithoutRecoveryEvidence(t *testing.T) {
	e, _, d := fixtureEngine(t)
	path := filepath.Join(t.TempDir(), "public", "status.json")
	e.Store.PublicStatusFile = path

	state, err := e.RunOnce(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := securitystatus.Load(path); !got.Reported || got.Status.Enrolled || got.Status.Phase != state.Phase || got.Status.Reason != "explicit_enrollment_required" {
		t.Fatalf("unenrolled run not published: %+v", got)
	}

	enrollFixture(t, e)
	d.applyFailure = true
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil {
		t.Fatalf("expected a pinned failure: %+v %v", state, err)
	}
	got := securitystatus.Load(path)
	if !got.Reported || !got.Status.Enrolled || got.Status.Phase != state.Phase || got.Status.Action != state.Action || got.Status.Detail == "" ||
		got.Status.TargetVersion != "1.3.1" || got.Status.Mode != "automatic" || got.Status.Channel != "stable" || len(got.Status.Applicable) != 1 {
		t.Fatalf("failed run not published: %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"operation", "incarnation", "manifest_sha256", "route_sha256", "retry", "catalog_sha256", "instance"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("public status leaks %q:\n%s", private, raw)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("public status mode = %v, err = %v", info.Mode(), err)
		}
	}
}

func TestPublicStatusWriteFailureNeverFailsMaintenance(t *testing.T) {
	e, _, _ := fixtureEngine(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A regular file as the parent makes both directory creation and the write fail.
	e.Store.PublicStatusFile = filepath.Join(blocker, "status.json")
	enrollFixture(t, e)
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" {
		t.Fatalf("public status failure changed maintenance: %+v %v", state, err)
	}
	if saved, err := e.Store.Load(); err != nil || saved.Phase != "healthy" {
		t.Fatalf("private state not saved: %+v %v", saved, err)
	}
}

// Detail echoes whatever a failed tool printed, so it is checked by value. Action
// keeps its confirmation value on purpose, and nothing else may carry one.
func TestPublicStatusScrubsAndBoundsDetailByValue(t *testing.T) {
	incarnation := strings.Repeat("c", 64)
	pinned := fixtureIdentity("1.3.1", 8)
	state := Status{
		Phase: "blocked", Reason: "preparation_failed", Action: reconcileAction(incarnation),
		Operation: &Operation{Target: Target{Identity: pinned}, RouteSHA256: releaseidentity.Hash([]byte("route"))},
		Detail: `registry login "hunter2-user" failed: Authorization: Bearer abc.def.ghi password=hunter2 ` +
			`dsn: postgres://admin:s3cr3t@db.internal/hikyo HIKYO_ROOT_KEY=0123456789abcdef ` + strings.Repeat("word ", 600),
	}
	raw, err := json.Marshal(state.Public())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "abc.def.ghi", "s3cr3t", "admin", "0123456789abcdef", string(pinned.ManifestSHA256), string(releaseidentity.Hash([]byte("route")))} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("public status carries %q:\n%s", secret, raw)
		}
	}
	public := state.Public()
	if !strings.Contains(public.Detail, "registry login") || !strings.Contains(public.Detail, "[redacted]") || len(public.Detail) > maxDetailBytes {
		t.Fatalf("detail lost its reason or exceeded its bound: %d %q", len(public.Detail), public.Detail)
	}
	if public.Action != "hikyo security-maintenance reconcile --confirm "+incarnation || strings.Count(string(raw), incarnation) != 1 {
		t.Fatalf("the exact command must be public, and only there: %s", raw)
	}
}

// The public subset replacing the private state would lose the enrollment and any pinned operation.
func TestPublicStatusNeverReplacesThePrivateState(t *testing.T) {
	e, _, _ := fixtureEngine(t)
	enrollFixture(t, e)
	e.Store.PublicStatusFile = filepath.Join(e.Store.directory, "status.json")
	if _, err := e.RunOnce(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if saved, err := e.Store.Load(); err != nil || saved.Enrollment == nil {
		t.Fatalf("public status replaced the private state: %+v %v", saved, err)
	}
}

// A refused public path must not leave a world-readable directory inside the private state.
func TestPublicStatusCreatesNothingInsideThePrivateState(t *testing.T) {
	e, _, _ := fixtureEngine(t)
	enrollFixture(t, e)
	pub := filepath.Join(e.Store.directory, "pub", "nested")
	e.Store.PublicStatusFile = filepath.Join(pub, "status.json")
	if _, err := e.RunOnce(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(e.Store.directory, "pub")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused public path created a directory inside the private state: %v", err)
	}
}
