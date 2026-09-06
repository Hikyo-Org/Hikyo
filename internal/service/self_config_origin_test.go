package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestSelfConfigOriginRecovery(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			for _, test := range []struct {
				name, factor, target                               string
				password, totp, unconfirmed, removed, wrongAccount bool
				passwordStale, totpStale, staleReview, unknownOld  bool
				age                                                time.Duration
				want                                               error
			}{
				{name: "passkey_cannot_retire_own_rp", factor: "webauthn", password: true, totp: true, want: domain.ErrInvalid},
				{name: "totp_and_password", factor: "totp", password: true, totp: true},
				{name: "same_hostname_port_change", factor: "webauthn", target: "https://old.example:8443"},
				{name: "same_hostname_case", factor: "webauthn", target: "https://OLD.example"},
				{name: "password_missing", factor: "totp", totp: true, want: domain.ErrInvalid},
				{name: "totp_missing", factor: "totp", password: true, want: domain.ErrInvalid},
				{name: "totp_unconfirmed", factor: "totp", password: true, totp: true, unconfirmed: true, want: domain.ErrInvalid},
				{name: "factor_removed_after_proof", factor: "totp", password: true, totp: true, removed: true, want: domain.ErrInvalid},
				{name: "another_accounts_credentials", factor: "totp", password: true, totp: true, wrongAccount: true, want: domain.ErrInvalid},
				{name: "password_stale_epoch", factor: "totp", password: true, totp: true, passwordStale: true, want: domain.ErrInvalid},
				{name: "totp_stale_epoch", factor: "totp", password: true, totp: true, totpStale: true, want: domain.ErrInvalid},
				{name: "stale_exact_proof", factor: "totp", password: true, totp: true, age: 5 * time.Minute, want: ErrReauthWindowExpired},
				{name: "unknown_old_origin", factor: "totp", password: true, totp: true, unknownOld: true, want: domain.ErrInvalid},
				{name: "stale_worker_review", factor: "totp", password: true, totp: true, staleReview: true, want: domain.ErrConflict},
			} {
				t.Run(test.name, func(t *testing.T) {
					cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "origin.db")}
					if engine == store.EnginePostgres {
						cfg = selfConfigPostgres(t)
					}
					s, local := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "off"})
					actor, session := selfConfigSession(t, s, local)
					binding, err := s.bindingForActor(t.Context(), actor, authz.OpSelfConfigApply)
					if err != nil {
						t.Fatal(err)
					}
					job := store.SelfConfigJob{SnapshotID: binding.DesiredSnapshotID, Revision: binding.DesiredRevision}
					review := &selfConfigOriginReview{owner: binding.OwnerInstanceID, incarnation: binding.Incarnation, oldSnapshot: binding.DesiredSnapshotID, generation: binding.Generation, candidateSnapshot: job.SnapshotID, candidateRevision: job.Revision, oldOrigin: "https://old.example", candidateOrigin: "https://new.example"}
					if test.target != "" {
						review.candidateOrigin = test.target
					}
					if test.unknownOld {
						review.oldOrigin = ""
					}
					if test.staleReview {
						review.generation++
					}
					s.originReview.Store(review)
					intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: binding.OwnerInstanceID, Revision: job.Revision, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: binding.Generation})
					if err != nil {
						t.Fatal(err)
					}
					exact, err := intent.bindingFor("")
					if err != nil {
						t.Fatal(err)
					}
					now := time.Now().UTC().Truncate(time.Second)
					err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
						account, err := az.AccountByPrincipal(ctx, local.principal)
						if err != nil {
							return err
						}
						if test.wrongAccount {
							if err := az.CreateHumanPrincipal(ctx, "usr_other", now); err != nil {
								return err
							}
							account = authz.Account{ID: "acc_other", PrincipalID: "usr_other", Username: "other", DisplayName: "Other", CreatedAt: now}
							if err := az.CreateAccount(ctx, account); err != nil {
								return err
							}
						}
						epoch, err := az.CredentialEpoch(ctx)
						if err != nil {
							return err
						}
						if test.password {
							credentialEpoch := epoch
							if test.passwordStale {
								credentialEpoch--
							}
							// Only credential presence/epoch is under test; cryptographic
							// verification belongs to the existing reauth ceremony.
							if err := az.WritePasswordCredential(ctx, authz.PasswordCredential{AccountID: account.ID, Verifier: []byte("fixture-verifier"), KDF: authz.KDFParams{MemoryKiB: 65536, Time: 3, Parallelism: 1}, DEKVersion: 1, CredentialEpoch: credentialEpoch}, now); err != nil {
								return err
							}
						}
						if test.totp {
							credentialEpoch := epoch
							if test.totpStale {
								credentialEpoch--
							}
							if err := az.CreateTOTP(ctx, authz.NewTOTPCredential{ID: "tot_origin", AccountID: account.ID, Seed: []byte("fixture-sealed-seed"), DEKVersion: 1, CredentialEpoch: credentialEpoch, CreatedStep: 1, CreatedAt: now}); err != nil {
								return err
							}
							if !test.unconfirmed {
								ok, err := az.ConfirmTOTP(ctx, "tot_origin", 1, 1, now)
								if err != nil {
									return err
								}
								if !ok {
									return domain.ErrConflict
								}
							}
						}
						if err := az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: "raw_origin", SessionID: session, EnvironmentID: intent.environmentID, CeremonyID: "origin-ceremony", FactorClass: test.factor, SingleDecision: true, AuthenticatedAt: now.Add(-test.age), WindowExpiresAt: now.Add(time.Hour), HardExpiresAt: now.Add(time.Hour), CredentialEpoch: epoch, CreatedAt: now, BoundPurpose: string(exact.purpose), BoundOperation: string(exact.operation), BoundKeySet: exact.keySet}); err != nil {
							return err
						}
						if test.removed {
							return az.RemoveTOTPForAccount(ctx, account.ID)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					consume := func() error {
						return tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
							caller, err := az.Authenticate(ctx, actor.bearer, now)
							if err != nil {
								return err
							}
							if err := s.requireOriginRecovery(ctx, az, caller, binding, job, intent, now); err != nil {
								return err
							}
							return s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, now)
						})
					}
					if err := consume(); !errors.Is(err, test.want) {
						t.Fatalf("origin decision = %v, want %v", err, test.want)
					}
					if test.want == nil {
						if err := consume(); !errors.Is(err, ErrReauthWindowSpent) {
							t.Fatalf("proof replay = %v", err)
						}
					}
				})
			}
		})
	}
}

func TestSelfConfigOriginReviewLoadsRetainedOriginWithoutActiveGraph(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "retained-origin.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, actor := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "off", "HIKYO_EXTERNAL_ORIGIN": "https://old.example"})
			binding, err := s.bindingForActor(t.Context(), actor, authz.OpSelfConfigApply)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := runtimeconfig.Prepare(map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://new.example"})
			if err != nil {
				t.Fatal(err)
			}
			if s.active.Load() != nil {
				t.Fatal("fixture already has active graph")
			}
			job := store.SelfConfigJob{SnapshotID: binding.DesiredSnapshotID, Revision: binding.DesiredRevision}
			if err := s.prepareOriginReview(t.Context(), binding, job, candidate); err != nil {
				t.Fatal(err)
			}
			review := s.originReview.Load()
			if review == nil || review.oldOrigin != "https://old.example" || review.candidateOrigin != "https://new.example" {
				t.Fatal("retained snapshot origin was not reviewed")
			}
		})
	}
}

func TestSelfConfigOriginReviewUsesStillActiveRPAfterInstallationFailure(t *testing.T) {
	old, err := runtimeconfig.Prepare(map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://old.example"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := runtimeconfig.Prepare(map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://new.example"})
	if err != nil {
		t.Fatal(err)
	}
	s := &SelfConfig{}
	s.installed.Store(&selfConfigActive{owner: "owner", incarnation: "incarnation", generation: 1, snapshotID: "old", bundle: old})
	binding := store.SelfConfigBinding{OwnerInstanceID: "owner", Incarnation: "incarnation", Generation: 2, DesiredSnapshotID: "new", DesiredRevision: 2}
	job := store.SelfConfigJob{SnapshotID: "new", Revision: 2}
	if err := s.prepareOriginReview(t.Context(), binding, job, target); err != nil {
		t.Fatal(err)
	}
	review := s.originReview.Load()
	if review == nil || review.oldOrigin != "https://old.example" || review.candidateOrigin != "https://new.example" || review.generation != 2 {
		t.Fatal("retry forgot the RP hostname still serving the applying administrator")
	}
}

func TestSelfConfigOriginReviewCannotCrossDecisionBoundary(t *testing.T) {
	binding := store.SelfConfigBinding{OwnerInstanceID: "owner", Incarnation: "incarnation", Generation: 2, DesiredSnapshotID: "old"}
	job := store.SelfConfigJob{SnapshotID: "candidate", Revision: 3}
	for _, change := range []struct {
		name   string
		mutate func(*selfConfigOriginReview)
	}{
		{"owner", func(r *selfConfigOriginReview) { r.owner = "another" }},
		{"incarnation", func(r *selfConfigOriginReview) { r.incarnation = "restored" }},
		{"old_snapshot", func(r *selfConfigOriginReview) { r.oldSnapshot = "another" }},
		{"generation", func(r *selfConfigOriginReview) { r.generation++ }},
		{"candidate_snapshot", func(r *selfConfigOriginReview) { r.candidateSnapshot = "another" }},
		{"candidate_revision", func(r *selfConfigOriginReview) { r.candidateRevision++ }},
	} {
		t.Run(change.name, func(t *testing.T) {
			s := &SelfConfig{}
			review := &selfConfigOriginReview{owner: "owner", incarnation: "incarnation", oldSnapshot: "old", generation: 2, candidateSnapshot: "candidate", candidateRevision: 3}
			change.mutate(review)
			s.originReview.Store(review)
			if err := s.requireOriginRecovery(t.Context(), nil, authz.Identity{}, binding, job, ReauthIntent{}, time.Now()); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("changed %s reused review: %v", change.name, err)
			}
		})
	}
}

func TestSelfConfigOriginApplyRefusesRetiringPasskeyHostname(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "origin-apply.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, local := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "off", "HIKYO_EXTERNAL_ORIGIN": "https://old.example", "HIKYO_ARGON2_TIME": "3"})
			s.Installer = &installerProbe{activateFailures: make(map[string]int)}
			t.Cleanup(func() {
				if err := s.CloseRuntime(); err != nil {
					t.Error(err)
				}
			})
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, session := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
			draft, err := values.Set(t.Context(), actor, scope, "HIKYO_EXTERNAL_ORIGIN", "https://new.example", nil)
			if err != nil {
				t.Fatal(err)
			}
			revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
			if _, err := revisions.PublishPlanned(t.Context(), actor, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
				t.Fatal(err)
			}
			status, err = s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			req := installerRequest(status, "origin-passkey-refusal")
			intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: req.Revision, ExpectedGeneration: req.ExpectedGeneration, SchemaVersion: req.SchemaVersion})
			if err != nil {
				t.Fatal(err)
			}
			exact, err := intent.bindingFor("")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: "raw_origin_apply", SessionID: session, EnvironmentID: intent.environmentID, CeremonyID: "origin-apply-ceremony", FactorClass: "webauthn", SingleDecision: true, AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute), CredentialEpoch: epoch, CreatedAt: now, BoundPurpose: string(exact.purpose), BoundOperation: string(exact.operation), BoundKeySet: exact.keySet})
			})
			if err != nil {
				t.Fatal(err)
			}
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := awaitInstallerApply(t, done)
			if !errors.Is(result.err, domain.ErrInvalid) {
				t.Fatalf("Apply retired passkey hostname: %v", result.err)
			}
			after, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			if after.Generation != status.Generation {
				t.Fatal("refused origin change committed a generation")
			}
			bundle, err := s.Capture(t.Context())
			if err != nil || bundle.OwnerValues()["HIKYO_EXTERNAL_ORIGIN"] != "https://old.example" {
				t.Fatalf("old usable origin lost after refusal: %v", err)
			}
		})
	}
}
