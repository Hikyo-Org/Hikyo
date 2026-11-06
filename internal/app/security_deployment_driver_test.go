package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
)

type reconcilingSecurityExecutor struct {
	securityDeploymentExecutor
	observed securitydeployment.Observation
}

func (e *reconcilingSecurityExecutor) Reconcile(_ context.Context, observed securitydeployment.Observation) error {
	e.observed = observed
	return nil
}

func TestSecurityDeploymentDriverReconcilePreservesObservation(t *testing.T) {
	executor := &reconcilingSecurityExecutor{}
	source := securitydeployment.Observation{Platform: "linux/arm64", Engine: releaseidentity.SQLite, Identity: nativeSecurityIdentity(releaseidentity.StableV1), Image: "ghcr.io/hikyo-org/hikyo@sha256:aa", RuntimeImageSHA256: "bb",
		Instance: "installation", Generation: 7, RestoreEpoch: 2, Incarnation: "incarnation", Phase: "healthy", Ready: true, RouteSHA256: "cc", SchemaChanges: true, RequiresMaintenance: true}
	driver := &securityDeploymentDriver{executor: executor}
	if err := driver.Reconcile(t.Context(), maintenanceObservation(source)); err != nil {
		t.Fatal(err)
	}
	if executor.observed != source {
		t.Fatalf("observation changed crossing the driver: %+v", executor.observed)
	}
	withoutJournal := &securityDeploymentDriver{}
	if err := withoutJournal.Reconcile(t.Context(), securitymaintenance.Observation{}); err == nil {
		t.Fatal("executor without a journal reported reconciliation")
	}
}

type publishingSecurityExecutor struct {
	securityDeploymentExecutor
	pinned   int
	got      []byte
	failWith error
}

func (e *publishingSecurityExecutor) PinIdentity(context.Context) error { e.pinned++; return nil }

func (e *publishingSecurityExecutor) PublishStatus(_ context.Context, status []byte) error {
	e.got = status
	return e.failWith
}

// The copy is a courtesy to the serving Pod: whatever goes wrong, the run it follows
// has nothing to report and the process does not stop.
func TestSecurityDeploymentDriverStatusCopyNeverFailsTheRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(file, []byte(`{"phase":"current"}`), 0644); err != nil {
		t.Fatal(err)
	}
	executor := &publishingSecurityExecutor{failWith: errors.New("configmaps \"status\" is forbidden")}
	driver := &securityDeploymentDriver{executor: executor}
	driver.PublishStatus(t.Context(), file)
	if string(executor.got) != `{"phase":"current"}` {
		t.Fatalf("published %q", executor.got)
	}
	executor.got = nil
	driver.PublishStatus(t.Context(), filepath.Join(t.TempDir(), "absent.json"))
	driver.PublishStatus(t.Context(), "")
	var none *securityDeploymentDriver
	none.PublishStatus(t.Context(), file)
	(&securityDeploymentDriver{executor: securityDeploymentExecutor(nil)}).PublishStatus(t.Context(), file)
	oversized := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(oversized, make([]byte, securitystatus.MaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	driver.PublishStatus(t.Context(), oversized)
	if executor.got != nil {
		t.Fatalf("copied a missing, empty or oversized status: %q", executor.got)
	}
}

func TestSecurityDeploymentDriverIsFoundUnderThePolicyGuard(t *testing.T) {
	driver := &securityDeploymentDriver{executor: &publishingSecurityExecutor{}}
	for name, wrapped := range map[string]securitymaintenance.Driver{"bare": driver, "bound": policyBoundSecurityDriver{Driver: driver}} {
		if deploymentDriverOf(wrapped) != driver {
			t.Fatalf("%s driver not found", name)
		}
	}
	if deploymentDriverOf(&policyTestSecurityDriver{}) != nil {
		t.Fatal("a native driver was taken for a deployment driver")
	}
	if err := driver.PinIdentity(t.Context()); err != nil || driver.executor.(*publishingSecurityExecutor).pinned != 1 {
		t.Fatalf("pin not forwarded: %v", err)
	}
	if err := (&securityDeploymentDriver{executor: &reconcilingSecurityExecutor{}}).PinIdentity(t.Context()); err != nil {
		t.Fatalf("an executor with nothing to pin failed enrollment: %v", err)
	}
}

type removedPinExecutor struct {
	publishingSecurityExecutor
	present bool
}

func (e *removedPinExecutor) IdentityPinned() (bool, error) { return e.present, nil }

// The runbook recovers a replaced Deployment by removing the pin and running enroll.
func TestSecurityEnrollRecordsAPinTheOperatorRemoved(t *testing.T) {
	store, err := securitymaintenance.NewStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(securitymaintenance.Status{Format: securitymaintenance.StatusFormat, Phase: "enrolled", Enrollment: &securitymaintenance.Enrollment{Instance: "installation", Incarnation: string(releaseidentity.Hash([]byte("incarnation"))), ConsentedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	executor := &removedPinExecutor{}
	engine := &securitymaintenance.Engine{Store: store, Driver: &securityDeploymentDriver{executor: executor}}
	if err := enrollSecurityMaintenance(t.Context(), engine, "", io.Discard); err != nil || executor.pinned != 1 {
		t.Fatalf("the pin was not recorded again: %v pinned=%d", err, executor.pinned)
	}
	executor.present = true
	if err := enrollSecurityMaintenance(t.Context(), engine, "", io.Discard); err != nil || executor.pinned != 1 {
		t.Fatalf("a present pin reached the cluster: %v pinned=%d", err, executor.pinned)
	}
}

type conflictingPinExecutor struct {
	publishingSecurityExecutor
}

func (e *conflictingPinExecutor) PinIdentity(context.Context) error {
	e.pinned++
	return securitydeployment.ErrConflict
}

// The chart's hook runs on every upgrade, including the one that recovers a failed update
// while the coordinator holds its lock. Once enrolled it must not need either.
func TestSecurityEnrollHookSucceedsForAnEnrolledInstallationWithoutPinningOrTheLock(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	store, err := securitymaintenance.NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	executor := &conflictingPinExecutor{}
	engine := &securitymaintenance.Engine{Store: store, Driver: &securityDeploymentDriver{executor: executor}}
	var out bytes.Buffer
	if err := enrollSecurityMaintenance(t.Context(), engine, "", &out); !errors.Is(err, securitydeployment.ErrConflict) {
		t.Fatal("first enrollment skipped the pin", err)
	}
	if executor.pinned != 1 {
		t.Fatal("first enrollment did not pin first", executor.pinned)
	}
	if err := store.Save(securitymaintenance.Status{Format: securitymaintenance.StatusFormat, Phase: "enrolled", Enrollment: &securitymaintenance.Enrollment{Instance: "installation", Incarnation: string(releaseidentity.Hash([]byte("incarnation"))), ConsentedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(directory, "coordinator.lock"))
	if locked, err := held.TryLock(); err != nil || !locked {
		t.Fatal(locked, err)
	}
	defer held.Unlock()
	out.Reset()
	if err := enrollSecurityMaintenance(t.Context(), engine, "", &out); err != nil || executor.pinned != 1 || !strings.Contains(out.String(), `"phase":"enrolled"`) {
		t.Fatalf("an enrolled installation needed the pin or the lock: %v pinned=%d %s", err, executor.pinned, out.String())
	}
}
