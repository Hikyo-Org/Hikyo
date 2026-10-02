package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestHumanRequestAdmissionPrecedesProtectedWork(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		token := seedSessionFactors(t, db, alice, `["password","totp"]`)
		sessionID := queryString(t, db, `SELECT id FROM sessions WHERE principal_id='usr_alice'`)
		contract, err := operation.NewContract("createProjectGrant", "grant.create-project", []string{"manage-members@project"}, []string{operation.ArtifactHumanSession})
		if err != nil {
			t.Fatal(err)
		}
		charges := 0
		ctx := operation.WithContract(t.Context(), contract)
		ctx = operation.WithRequestAdmission(ctx, func(id string) error {
			charges++
			if id != sessionID {
				t.Fatalf("charge used untrusted session key %q", id)
			}
			return &admission.RateLimitedError{Cause: admission.ErrOverloaded, Wait: time.Second}
		})
		before := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events`)
		_, err = (&service.Grants{DB: db}).Create(ctx, service.Bearer(token), service.GrantSpec{
			Target: alice, Capability: domain.CapRead, Scope: domain.Scope{Org: orgA, Project: prjA1},
		})
		if !errors.Is(err, admission.ErrOverloaded) || charges != 1 {
			t.Fatalf("forbidden mutation bypassed request admission: %v charges=%d", err, charges)
		}
		if after := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events`); after != before {
			t.Fatal("rate refusal wrote an authorization-denial audit")
		}
		// Ordinary read-only account/session surfaces also invoke the same
		// admission after authentication; repeated reads share one receipt.
		whoami, err := operation.NewArtifactContract("whoami", []string{operation.ArtifactHumanSession})
		if err != nil {
			t.Fatal(err)
		}
		readCharges := 0
		readCtx := operation.WithRequestAdmission(operation.WithContract(t.Context(), whoami), func(id string) error {
			readCharges++
			if id != sessionID {
				t.Fatal("read charged different session")
			}
			return nil
		})
		auth := authService(t, db)
		for range 2 {
			if _, err := auth.Identity(readCtx, token); err != nil {
				t.Fatal(err)
			}
		}
		if readCharges != 1 {
			t.Fatalf("read retries charged %d times", readCharges)
		}
		invalidCharges := 0
		invalidCtx := operation.WithRequestAdmission(operation.WithContract(t.Context(), whoami), func(string) error { invalidCharges++; return nil })
		if _, err := auth.Identity(invalidCtx, "guessed-session"); !errors.Is(err, domain.ErrUnauthenticated) || invalidCharges != 0 {
			t.Fatal("unresolved caller allocated a session budget")
		}
	})
}

func TestDocumentRemoteOriginsRequireLiveDirectoryProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_doc_directory','usr_root','instance-directory',NULL,NULL,NULL,`+ts+`)`)
		credential := "X'01'"
		if db.Engine() == store.EnginePostgres {
			credential = "decode('01','hex')"
		}
		execRaw(t, db, `INSERT INTO remotes (id,name,url,spki_pin,credential_sealed,created_at,created_by) VALUES ('rmt_doc','private','https://10.0.0.12:8443','pin',`+credential+`,`+ts+`,'usr_root')`)
		svc := &service.Remotes{DB: db}
		privileged := seedSessionFactors(t, db, root, `["password","totp"]`)
		unprivileged := seedSessionFactors(t, db, alice, `["password","totp"]`)
		for _, actor := range []service.Actor{service.Bearer(""), service.Bearer("guessed-session"), service.Bearer(unprivileged)} {
			if origins, err := svc.RemoteOrigins(ctx, actor); err == nil || len(origins) != 0 {
				t.Fatalf("unprivileged document disclosed origins=%v error=%v", origins, err)
			}
		}
		origins, err := svc.RemoteOrigins(ctx, service.Bearer(privileged))
		if err != nil || len(origins) != 1 || origins[0] != "https://10.0.0.12:8443" {
			t.Fatalf("authorized workspace policy: %v %v", origins, err)
		}
		if err := authService(t, db).Logout(context.Background(), privileged); err != nil {
			t.Fatal(err)
		}
		if origins, err := svc.RemoteOrigins(ctx, service.Bearer(privileged)); !errors.Is(err, domain.ErrUnauthenticated) || len(origins) != 0 {
			t.Fatalf("stale document session disclosed origins=%v error=%v", origins, err)
		}
	})
}
