package updater

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnixControlSubmitsAndPersistsOneJob(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hikyo-updater-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "updater.sock")
	journal := &Journal{Path: filepath.Join(dir, "state.json")}
	runner := &recordingRunner{}
	control := &ControlServer{
		Executor: Executor{Config: fixtureConfig(t, BackendFlux), Runner: runner},
		Journal:  journal,
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: control.Handler()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})

	client := NewClient(socket)
	capability, err := client.Capability(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if capability.Backend != BackendFlux {
		t.Fatalf("backend = %q", capability.Backend)
	}
	jobID := "upd_0198aa00-0000-7000-8000-000000000001"
	job, err := client.Submit(t.Context(), Request{
		ID: jobID, Version: "1.2.3", ReleaseURL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.2.3", RequestedBy: "usr_admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != jobID || (job.State != StateQueued && job.State != StateRunning) {
		t.Fatalf("submitted job = %#v", job)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err = client.Job(t.Context(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish: %#v", job)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.State != StateSucceeded {
		t.Fatalf("terminal job = %#v", job)
	}
	pending, err := client.PendingOutcomes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != jobID {
		t.Fatalf("pending outcomes = %#v, want job %s", pending, jobID)
	}
	if err := client.AcknowledgeOutcome(t.Context(), jobID); err != nil {
		t.Fatal(err)
	}
	job, err = journal.Get(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !job.OutcomeReported {
		t.Fatal("outcome acknowledgement was not durable")
	}
	if _, err := client.Submit(t.Context(), Request{
		ID: "../../unsafe", Version: "1.2.4",
		ReleaseURL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.2.4", RequestedBy: "usr_admin",
	}); err == nil {
		t.Fatal("helper accepted a non-canonical job id")
	}
	if _, err := client.Submit(t.Context(), Request{
		ID: "upd_0198aa00-0000-7000-8000-000000000002", Version: "1.2.4",
		ReleaseURL: "https://attacker.example/v1.2.4", RequestedBy: "usr_admin",
	}); !errors.Is(err, ErrReleaseAuthority) {
		t.Fatalf("foreign release authority error = %v, want ErrReleaseAuthority", err)
	}
}
