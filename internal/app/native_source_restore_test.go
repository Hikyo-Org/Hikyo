//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type restoreProbeHost struct {
	started, completed int
	fenced             int
	completeErr        error
	digest             string
	startErr           error
	afterStart         func()
}

func (h *restoreProbeHost) StartCandidate(_ context.Context, digest string, ready bool, _ time.Duration) error {
	h.started++
	h.digest = digest
	if !ready {
		return errors.New("the restarted source must prove readiness")
	}
	if h.afterStart != nil {
		h.afterStart()
	}
	return h.startErr
}
func (h *restoreProbeHost) Complete(context.Context) error {
	h.completed++
	return h.completeErr
}
func (h *restoreProbeHost) FenceAndStop(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	h.fenced++
	return nil
}

func TestNativeSourceIsRestartedOnlyAfterAProvenPreWriteFailure(t *testing.T) {
	source, target := preparationIdentity("1.0.0", 1), preparationIdentity("1.0.1", 2)
	manifest := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	schema, route := releaseidentity.Hash([]byte("schema")), releaseidentity.Hash([]byte("route"))
	journal := func(phase string) *automaticJournal {
		return &automaticJournal{Phase: phase, Target: target, Route: route, Instance: "installation", Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: source}, Migrations: manifest, SchemaSHA256: schema}}
	}
	healthy := func() upgrade.State {
		return upgrade.State{InstanceID: "installation", Applied: releaseidentity.Source{Release: source}, SchemaDigest: schema, MigrationDigest: digest, Generation: 4, Pending: &upgrade.Operation{Phase: upgrade.Healthy}}
	}
	cause := errors.New("scratch proof refused")
	binary := releaseidentity.Hash([]byte("source binary"))
	for _, test := range []struct {
		name     string
		phase    string
		change   func(*upgrade.State)
		host     restoreProbeHost
		absent   bool
		restored bool
		started  int
		fenced   int
	}{
		{name: "untouched source before write intent", phase: "preparing", restored: true, started: 1},
		{name: "journal at write-intent stays stopped", phase: "write-intent"},
		{name: "journal at schema-applied stays stopped", phase: "schema-applied"},
		{name: "journal at restore-required stays stopped", phase: "restore-required"},
		{name: "control row shows a started operation", phase: "preparing", change: func(s *upgrade.State) { s.Pending.Phase, s.Maintenance = upgrade.SchemaApplied, true }},
		{name: "control row names another source", phase: "preparing", change: func(s *upgrade.State) { s.Applied = releaseidentity.Source{Release: target} }},
		{name: "control row unreadable", phase: "preparing", absent: true},
		{name: "source does not come back healthy", phase: "preparing", host: restoreProbeHost{startErr: errors.New("source unhealthy")}, started: 1},
		{name: "source moved after it restarted", phase: "preparing", host: restoreProbeHost{afterStart: func() {}}, started: 1, fenced: 1},
		{name: "control row unreadable after it restarted", phase: "preparing", host: restoreProbeHost{afterStart: func() {}}, started: 1, fenced: 1},
		{name: "fence lift fails after it restarted", phase: "preparing", host: restoreProbeHost{completeErr: errors.New("complete failed")}, started: 1, fenced: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := healthy()
			if test.change != nil {
				test.change(&state)
			}
			inspection := &automaticTestInspection{state: state, absent: test.absent}
			host := &test.host
			switch test.name {
			case "source moved after it restarted":
				host.afterStart = func() { inspection.state.Generation++ }
			case "control row unreadable after it restarted":
				host.afterStart = func() { inspection.absent = true }
			}
			directory := t.TempDir()
			restored, err := restoreNativeSource(t.Context(), host, inspection, journal(test.phase), directory, binary, cause)
			if restored != test.restored || !errors.Is(err, cause) || host.started != test.started {
				t.Fatalf("restored=%t err=%v started=%d", restored, err, host.started)
			}
			// A source that was started and then doubted must be stopped, not left writing.
			if host.fenced != test.fenced {
				t.Fatalf("fenced %d times, want %d", host.fenced, test.fenced)
			}
			if test.restored {
				if host.completed != 1 || host.digest != string(binary) {
					t.Fatalf("the fence was not lifted around the exact source binary: %+v", host)
				}
				raw, readErr := os.ReadFile(filepath.Join(directory, "preparation-abandoned.json"))
				if readErr != nil || !strings.Contains(string(raw), "restarted after its preparation failed") {
					t.Fatalf("the reason was not recorded: %v", readErr)
				}
			} else if host.completed != 0 && host.completeErr == nil {
				t.Fatal("maintenance was lifted without a proven untouched source")
			}
		})
	}
}

// A host enrolled from nothing has no completed journal, so the restore must retain the
// bundle runtime.env names or the source cannot boot again.
func TestNativeSourceEvidenceRetainsTheBundleRuntimeEnvironmentNames(t *testing.T) {
	completed := &automaticJournal{}
	completed.Runtime.BundleDirectory = "/public/bundle-completed"
	for name, test := range map[string]struct {
		previous *automaticJournal
		bundle   string
		want     string
	}{
		"no journal": {bundle: "/public/bundle-source", want: "/public/bundle-source"},
		"completed":  {previous: completed, bundle: "/public/bundle-source", want: "/public/bundle-completed"},
	} {
		t.Run(name, func(t *testing.T) {
			evidence, err := nativeSourceEvidence(bundleHost{test.bundle}, test.previous)
			if err != nil || evidence.BundleDirectory != test.want {
				t.Fatal(evidence, err)
			}
		})
	}
	if _, err := nativeSourceEvidence(bundleHost{}, nil); err == nil {
		t.Fatal("an unreadable source bundle was ignored")
	}
}

type bundleHost struct{ bundle string }

func (h bundleHost) LocalBundleDirectory() (string, error) {
	if h.bundle == "" {
		return "", errors.New("no local bundle")
	}
	return h.bundle, nil
}
