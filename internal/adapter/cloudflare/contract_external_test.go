package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// TestCloudflareRealLifecycle is destructive and opt-in: it writes to a
// disposable Workers script and Pages project. No emulator exists, so this
// gated run is the end-to-end proof; the fake-API module tests carry the logic.
//
// Required: HIKYO_TEST_CLOUDFLARE_ACCOUNT, _TOKEN, _SCRIPT, _PAGES_PROJECT.
// The token must be scoped to that one account with Workers Scripts: Edit and
// Cloudflare Pages: Edit. HIKYO_TEST_CLOUDFLARE_REQUIRED=1 turns a missing
// credential into a failure for jobs that are meant to run it.
func TestCloudflareRealLifecycle(t *testing.T) {
	account := os.Getenv("HIKYO_TEST_CLOUDFLARE_ACCOUNT")
	token := os.Getenv("HIKYO_TEST_CLOUDFLARE_TOKEN")
	script := os.Getenv("HIKYO_TEST_CLOUDFLARE_SCRIPT")
	project := os.Getenv("HIKYO_TEST_CLOUDFLARE_PAGES_PROJECT")
	if account == "" || token == "" || script == "" || project == "" {
		if os.Getenv("HIKYO_TEST_CLOUDFLARE_REQUIRED") == "1" {
			t.Fatal("HIKYO_TEST_CLOUDFLARE_REQUIRED=1 but ACCOUNT/TOKEN/SCRIPT/PAGES_PROJECT are not all set")
		}
		t.Skip("set HIKYO_TEST_CLOUDFLARE_ACCOUNT/TOKEN/SCRIPT/PAGES_PROJECT for the real Cloudflare lifecycle")
	}
	client, err := NewClient(ClientConfig{Credential: token, Deadline: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Forget()
	module := &Module{API: client}
	run := fmt.Sprintf("HIKYO_E2E_%d_", time.Now().UnixNano()%1_000_000)
	manifest := []adapter.ManifestEntry{
		{KeyID: "key_1", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "s3cret-" + run},
		{KeyID: "key_2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "config-" + run},
	}

	connect := func(d adapter.Destination) adapter.Target {
		t.Helper()
		connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: d, Access: adapter.Access{Credential: token}, Gate: allow})
		if err != nil {
			t.Fatalf("TestConnection(%s): %v", d.Kind, err)
		}
		d.NumericID = connection.DestinationID
		return adapter.Target{ID: "tgt_" + string(d.Kind) + d.Environment, Destination: d, NamePrefix: run, Generation: 1}
	}
	lifecycle := func(t *testing.T, target adapter.Target, other *adapter.Target) {
		journal := newFakeJournal()
		cleanup := func() {
			if _, err := module.Sync(context.WithoutCancel(t.Context()), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
				t.Errorf("teardown: %v", err)
			}
		}
		defer cleanup()

		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
			t.Fatalf("first sync: %v", err)
		}
		_, names, err := module.resolve(t.Context(), target.Destination)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{run + adapter.SentinelName, run + "TOKEN", run + "MODE"} {
			if !names[strings.ToUpper(name)] {
				t.Fatalf("%s missing after sync", name)
			}
		}
		if target.Destination.Kind == adapter.PagesProject {
			shape, err := client.ResolveProject(t.Context(), target.Destination)
			if err != nil {
				t.Fatal(err)
			}
			for name, kind := range shape.Names {
				if strings.HasPrefix(name, run) && kind != SecretType {
					t.Fatalf("%s written as %q, want %s", name, kind, SecretType)
				}
			}
			if other != nil {
				otherShape, err := client.ResolveProject(t.Context(), other.Destination)
				if err != nil {
					t.Fatal(err)
				}
				for name := range otherShape.Names {
					if strings.HasPrefix(name, run) {
						t.Fatalf("%s leaked into %s", name, other.Destination.Environment)
					}
				}
			}
		}
		// Replay converges as updates with no new ownership.
		if result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest, Ledger: journal.ledger()}, journal); err != nil || len(result.Conflicts) != 0 {
			t.Fatalf("replay = %+v, %v", result, err)
		}
		// An unowned name in the way is refused, not overwritten.
		foreign := run + "FOREIGN"
		value := "foreign-" + run
		if target.Destination.Kind == adapter.PagesProject {
			err = client.PatchPagesSecret(t.Context(), target.Destination, foreign, &value)
		} else {
			err = client.PutSecret(t.Context(), target.Destination, foreign, value)
		}
		if err != nil {
			t.Fatalf("seed foreign name: %v", err)
		}
		defer func() {
			if target.Destination.Kind == adapter.PagesProject {
				_ = client.PatchPagesSecret(context.WithoutCancel(t.Context()), target.Destination, foreign, nil)
			} else {
				_ = client.DeleteSecret(context.WithoutCancel(t.Context()), target.Destination, foreign)
			}
		}()
		withForeign := append(append([]adapter.ManifestEntry(nil), manifest...), adapter.ManifestEntry{KeyID: "key_3", CanonicalName: "FOREIGN", Classification: adapter.SecretClassification, Value: "hikyo"})
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: withForeign, Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrConflict) {
			t.Fatalf("foreign name sync = %v, want conflict", err)
		}
	}

	t.Run("workers", func(t *testing.T) {
		lifecycle(t, connect(adapter.Destination{Kind: adapter.WorkersScript, Owner: account, Name: script}), nil)
	})
	t.Run("pages", func(t *testing.T) {
		preview := connect(adapter.Destination{Kind: adapter.PagesProject, Owner: account, Name: project, Environment: "preview"})
		production := connect(adapter.Destination{Kind: adapter.PagesProject, Owner: account, Name: project, Environment: "production"})
		if preview.Destination.NumericID == production.Destination.NumericID {
			t.Fatal("preview and production share a destination id")
		}
		lifecycle(t, preview, &production)
	})
}
