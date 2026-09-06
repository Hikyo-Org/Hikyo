package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func repairSession(t *testing.T, s *SelfConfig, principal domain.PrincipalID, method, factors string) Actor {
	t.Helper()
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID("ses")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, principal)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{ID: id, PrincipalID: principal, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: method, Factors: factors, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "repair-test"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return Bearer(artifact)
}

func repairOrgAdmin(t *testing.T, s *SelfConfig, scope domain.Scope) Actor {
	t.Helper()
	id, err := newID("usr")
	if err != nil {
		t.Fatal(err)
	}
	principal := domain.PrincipalID(id)
	caps, err := domain.ExpandTemplate(domain.TemplateAdmin, domain.LevelOrg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.CreateHumanPrincipal(ctx, principal, now); err != nil {
			return err
		}
		for i, cap := range caps {
			if err := az.CreateGrant(ctx, fmt.Sprintf("grant_%s_%d", id, i), principal, domain.Grant{Capability: cap, Scope: domain.Scope{Org: scope.Org}}, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return repairSession(t, s, principal, "local-passkey", `["webauthn","mfa"]`)
}

func repairMachine(t *testing.T, s *SelfConfig, local Actor, scope domain.Scope) Actor {
	t.Helper()
	// Create a real machine aggregate and credential through fixture authority.
	// The protected runtime API never permits an admin to provision this machine.
	principal, err := newID("mch")
	if err != nil {
		t.Fatal(err)
	}
	account, err := newID("sa")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := newID("cred")
	if err != nil {
		t.Fatal(err)
	}
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactAutomation)
	if err != nil {
		t.Fatal(err)
	}
	hint, err := prefixHint(artifact)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.CreateServiceAccountAggregate(ctx, authz.NewServiceAccount{ID: account, PrincipalID: domain.PrincipalID(principal), Org: scope.Org, Project: scope.Project, Name: "repair-machine", Kind: domain.ClassAutomation, CreatedAt: now, CreatedBy: local.principal})
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.CreateMachineCredential(ctx, authz.NewCredential{ID: credential, ServiceAccountID: account, Kind: domain.CredentialHikyoToken, Verifier: verifier, PrefixHint: hint, Lifetime: domain.LifetimeFinite, ExpiresAt: now.Add(time.Hour), CredentialEpoch: epoch, CreatedAt: now, CreatedBy: local.principal})
	})
	if err != nil {
		t.Fatal(err)
	}
	return Bearer(artifact)
}

func TestSelfConfigRepairScopeRequiresMFAInstanceAdminAndExactHierarchy(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, _ := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			passkey, _ := selfConfigSession(t, s, local)
			totp := repairSession(t, s, local.principal, "local-password", `["password","totp","mfa"]`)
			weak := repairSession(t, s, local.principal, "local-password", `["password"]`)
			orgAdmin := repairOrgAdmin(t, s, scope)
			machine := repairMachine(t, s, local, scope)
			allowed := []domain.Scope{{Org: scope.Org}, {Org: scope.Org, Project: scope.Project}, scope}
			refusedScopes := []domain.Scope{
				{}, {Project: scope.Project, Env: scope.Env},
				{Org: "other-org", Project: scope.Project, Env: scope.Env},
				{Org: scope.Org, Project: "other-project", Env: scope.Env},
				{Org: scope.Org, Project: scope.Project, Env: "other-environment"},
				{Org: scope.Org, Env: scope.Env},
			}
			verify := func(t *testing.T) {
				t.Helper()
				// The middleware exception is itself a network operation. It carries no
				// local authority and must not widen access beyond the bound hierarchy.
				contract, err := operation.NewContract("test:configuration-repair", "key.list", []string{"read@environment"}, []string{operation.ArtifactHumanSession, operation.ArtifactMachineCredential})
				if err != nil {
					t.Fatal(err)
				}
				ctx := operation.WithContract(t.Context(), contract)
				for _, actor := range []Actor{passkey, totp} {
					for _, target := range allowed {
						if err := s.AuthorizeRepairScope(ctx, actor, target); err != nil {
							t.Fatalf("MFA instance admin denied protected hierarchy: %v", err)
						}
					}
					for _, target := range refusedScopes {
						if err := s.AuthorizeRepairScope(ctx, actor, target); err == nil {
							t.Fatalf("repair scope widened to %+v", target)
						}
					}
				}
				for name, actor := range map[string]Actor{"password-only instance admin": weak, "organization admin": orgAdmin, "machine": machine} {
					for _, target := range allowed {
						if err := s.AuthorizeRepairScope(ctx, actor, target); err == nil {
							t.Fatalf("%s entered protected recovery scope", name)
						}
					}
				}
			}
			t.Run("active", verify)
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				return r.SelfConfig().FenceRestored(ctx, proof, "repair-restored-incarnation", time.Now())
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatalf("restore did not fence ordinary application: %v", err)
			}
			t.Run("suspended", verify)
		})
	}
}
