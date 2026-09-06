package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSelfConfigNextRootSelectorNeedsExactApply(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, _ := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, session := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			initial := status.Generation
			node := map[string]string{"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272", config.ManagedNewRootSourceKey: "root-next"}
			raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{s.NodeID: node})
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			draft, err := (&Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).Set(t.Context(), local, scope, config.ManagedNodeOverridesKey, raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (&Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}).PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			active, err := s.Capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if active.HasNodeValues() {
				t.Fatal("publishing changed active node selector")
			}
			status, err = s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			req := installerRequest(status, "select-next-root")
			req.PrepareOnly = true
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := awaitInstallerApply(t, done)
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.status.Job.PlanDigest != "" {
				t.Fatal("candidate-only selection requested bootstrap rollout")
			}
			req.PrepareOnly = false
			target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration}
			wrong := target
			wrong.Revision++
			selfConfigReauthenticate(t, s, session, wrong)
			if _, err := s.Apply(t.Context(), actor, req); err == nil {
				t.Fatal("wrong revision MFA applied root selector")
			}
			status, err = s.Status(t.Context(), local)
			if err != nil || status.Generation != initial {
				t.Fatal("refused Apply changed generation", err)
			}
			selfConfigReauthenticate(t, s, session, target)
			if _, err := s.Apply(t.Context(), actor, req); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			active, err = s.Capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			applied, err := active.NodeValues(s.NodeID)
			if err != nil || applied[config.ManagedNewRootSourceKey] != "root-next" {
				t.Fatal("authorized target not captured", err)
			}
			// A fresh coordinator reconstructs from the encrypted saved revision.
			restarted := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: s.NodeID, Installer: s.Installer, Seed: func() (map[string]string, error) {
				return map[string]string{"HIKYO_UPDATE_CHANNEL": "off"}, nil
			}}
			t.Cleanup(func() { _ = restarted.CloseRuntime() })
			if err := restarted.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			active, err = restarted.Capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			applied, err = active.NodeValues(s.NodeID)
			if err != nil || applied[config.ManagedNewRootSourceKey] != "root-next" {
				t.Fatal("restart lost managed candidate selector", err)
			}
		})
	}
}
