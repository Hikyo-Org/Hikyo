package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestEnvironmentParametersDeliveryAndHistory(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_param_reveal', '`+string(identAdmin)+`', 'reveal', 'org_a', 'prj_a1', NULL, `+ts+`)`)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "PR_NUMBER", "^[0-9]{1,6}$", false); err != nil {
			t.Fatal(err)
		}
		// Declaration editing alone must never activate unpublished configuration.
		del := deliverySvc(t, db)
		if _, err := del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{}); err != nil {
			t.Fatalf("declaration activated before publication: %v", err)
		}
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "https://pr-${PR_NUMBER}.preview.example.com", "DATABASE_PASSWORD": "literal-${PR_NUMBER}"})
		_, _, cloneErr := envs.Clone(t.Context(), actor, domain.Scope{Org: orgA, Project: prjA1}, "unresolved-parameter-clone", string(envA1), nil)
		if !errors.Is(cloneErr, domain.ErrInvalid) {
			t.Fatalf("clone of template source did not refuse: %v", cloneErr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM environments WHERE name='unresolved-parameter-clone'`); n != 0 {
			t.Fatal("refused template clone left a partial environment")
		}
		contractLength := "OCTET_LENGTH(parameter_contract)"
		if db.Engine() == store.EngineSQLite {
			contractLength = "LENGTH(CAST(parameter_contract AS BLOB))"
		}
		wantBytes := queryInt(t, db, `SELECT (SELECT COALESCE(SUM(LENGTH(ciphertext)),0) FROM snapshot_entries WHERE org_id='org_a' AND project_id='prj_a1') + (SELECT COALESCE(SUM(`+contractLength+`),0) FROM snapshots WHERE org_id='org_a' AND project_id='prj_a1' AND parameter_contract <> '{}')`)
		if err := tx.Read(t.Context(), db, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: identAdmin}, authz.OpValuePublish, scope)
			if err != nil {
				return err
			}
			got, err := r.Snapshots().PayloadBytesForProject(ctx, p)
			if err == nil && got != wantBytes {
				t.Errorf("snapshot quota bytes = %d, want ciphertext plus contracts %d", got, wantBytes)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, supplied := range []map[string]string{nil, {"PR_NUMBER": "oops"}, {"PR_NUMBER": "123", "UNKNOWN": "x"}} {
			out, err := del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: supplied})
			if !errors.Is(err, domain.ErrInvalid) || len(out.Keys) != 0 {
				t.Fatalf("invalid parameters delivered: %+v, %v", out, err)
			}
		}
		first, err := del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "123"}})
		if err != nil {
			t.Fatal(err)
		}
		found, secretLiteral := false, false
		for _, key := range first.Keys {
			if key.Name == "DATABASE_URL" {
				found = key.Value != nil && *key.Value == "https://pr-123.preview.example.com"
			}
			if key.Name == "DATABASE_PASSWORD" {
				secretLiteral = key.Value != nil && *key.Value == "literal-${PR_NUMBER}"
			}
		}
		if !found {
			t.Fatalf("wrong resolved config: %+v", first.Keys)
		}
		if !secretLiteral {
			t.Fatal("secret reference was substituted or not disclosed to authorized caller")
		}
		current, err := del.FetchAs(t.Context(), actor, scope, first.Cursor, service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "123"}})
		if err != nil || !current.Current || len(current.Keys) != 0 {
			t.Fatalf("conditional fetch: %+v, %v", current, err)
		}
		other, err := del.FetchAs(t.Context(), actor, scope, first.Cursor, service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "124"}})
		if err != nil || other.Current || first.ChangeToken == other.ChangeToken {
			t.Fatalf("parameters did not invalidate cursor/token: %+v, %v", other, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='identity.delivery_fetched' AND payload LIKE '%"PR_NUMBER":"123"%'`); n < 1 {
			t.Fatal("parameter disclosure lacks audit inputs")
		}
		if err := envs.SetParameter(t.Context(), actor, scope, "PR_NUMBER", "", true); err != nil {
			t.Fatal(err)
		}
		if err := envs.SetParameter(t.Context(), actor, scope, "PR_NUMBER", "^[a-z]+$", false); err != nil {
			t.Fatal(err)
		}
		values, _, err := revisionSvc(t, db).ExportWithParameters(t.Context(), actor, scope, 2, false, map[string]string{"PR_NUMBER": "123"})
		if err != nil {
			t.Fatalf("history used live parameter pattern: %v", err)
		}
		if len(values) == 0 {
			t.Fatal("historical export empty")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='disclosure.values_exported' AND payload LIKE '%"revision":2%' AND payload LIKE '%"PR_NUMBER":"123"%'`); n != 1 {
			t.Fatal("parameter export lacks disclosure audit inputs")
		}
		staged, err := valueSvc(t, db).Set(t.Context(), actor, scope, "DATABASE_URL", "${UNKNOWN}", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = revisionSvc(t, db).PublishPlanned(t.Context(), actor, scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "UNKNOWN") {
			t.Fatalf("undeclared reference published: %v", err)
		}
	})
}

func TestUnicodeParameterContractChargesBytes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "LABEL", "^é+$", false); err != nil {
			t.Fatal(err)
		}
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "${LABEL}"})
		raw := queryString(t, db, `SELECT parameter_contract FROM snapshots WHERE environment_id='env_a1' ORDER BY revision DESC LIMIT 1`)
		if !strings.Contains(raw, "é") {
			t.Fatal("fixture did not persist multibyte contract text")
		}
		ciphertextBytes := queryInt(t, db, `SELECT COALESCE(SUM(LENGTH(ciphertext)),0) FROM snapshot_entries WHERE org_id='org_a' AND project_id='prj_a1'`)
		want := ciphertextBytes + int64(len(raw))
		if err := tx.Read(t.Context(), db, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: identAdmin}, authz.OpValuePublish, scope)
			if err != nil {
				return err
			}
			got, err := r.Snapshots().PayloadBytesForProject(ctx, p)
			if err != nil {
				return err
			}
			if got != want {
				t.Errorf("quota counts %d bytes, want %d actual ciphertext and UTF-8 bytes", got, want)
			}
			metricsProof, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, scope)
			if err != nil {
				return err
			}
			rows, err := r.Snapshots().InstancePayloadByProject(ctx, metricsProof)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.OrgID == "org_a" && row.ProjectID == "prj_a1" {
					if row.Bytes != want {
						t.Errorf("instance metric counts %d bytes, want %d actual ciphertext and UTF-8 bytes", row.Bytes, want)
					}
					return nil
				}
			}
			t.Error("instance payload metric omitted parameterized project")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestParameterContractPublicationBound(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "VALUE", ".*", false); err != nil {
			t.Fatal(err)
		}
		declaration, err := json.Marshal(map[string]any{"rule": map[string]any{"type": "string", "enum": []string{strings.Repeat("a", 15000), "x"}}})
		if err != nil {
			t.Fatal(err)
		}
		versions := []string{}
		for index := range 20 {
			name := fmt.Sprintf("LARGE_%02d", index)
			execRaw(t, db, fmt.Sprintf(`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at) VALUES ('key_large_%d','org_a','prj_a1','%s','','config','',FALSE,'','%s','none','none',NULL,%s)`, index, name, declaration, ts))
			staged, err := valueSvc(t, db).Set(t.Context(), actor, scope, name, "${VALUE}", nil)
			if err != nil {
				t.Fatal(err)
			}
			versions = append(versions, staged.VersionID)
		}
		_, err = revisionSvc(t, db).PublishPlanned(t.Context(), actor, scope, service.PublishRequest{VersionIDs: versions})
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "parameter contract exceeds") {
			t.Fatalf("unbounded contract published: %v", err)
		}
		if n := queryInt(t, db, `SELECT MAX(revision) FROM snapshots WHERE environment_id='env_a1'`); n != 1 {
			t.Fatalf("refused contract advanced revision to %d", n)
		}
	})
}

func TestParameterRenderLimitIgnoresUnrevealedSecretBytes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "PAD", "^x{256}$", false); err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for index := range 16 {
			name := fmt.Sprintf("CONFIG_%02d", index)
			execRaw(t, db, fmt.Sprintf(`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at) VALUES ('key_pad_%d','org_a','prj_a1','%s','','config','',FALSE,'','{"rule":{"type":"string"}}','none','none',NULL,%s)`, index, name, ts))
			values[name] = strings.Repeat("${PAD}", 256)
		}
		// Exactly 1 MiB of authorized config including the fixture DATABASE_URL.
		values["CONFIG_15"] = strings.Repeat("${PAD}", 255) + strings.Repeat("x", 256-len("postgres://dev"))
		publishDeliveryValues(t, db, envA1, values)
		check := func() {
			t.Helper()
			out, err := deliverySvc(t, db).FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: map[string]string{"PAD": strings.Repeat("x", 256)}})
			if err != nil {
				t.Fatalf("hidden secret affected config render limit: %v", err)
			}
			for _, key := range out.Keys {
				if key.Name == "DATABASE_PASSWORD" && key.Value != nil {
					t.Fatal("secret unexpectedly revealed")
				}
			}
		}
		check()
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_PASSWORD": strings.Repeat("s", 65536)})
		check()
	})
}

func TestParametersValidateResolvedSchemaAtomically(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		actor := service.LocalPrincipal(identAdmin)
		scope := scopeEnv(orgA, prjA1, envA1)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		if err := envs.SetParameter(t.Context(), actor, scope, "PR_NUMBER", "^[0-9]{1,6}$", false); err != nil {
			t.Fatal(err)
		}
		// A schema may impose a stricter final-value condition than the public
		// parameter pattern. Publication captures it without guessing a caller.
		execRaw(t, db, `UPDATE keys SET declaration='{"rule":{"type":"string","pattern":"^https://pr-123[.]preview[.]example[.]com$"}}' WHERE id='key_fed_url'`)
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "https://pr-${PR_NUMBER}.preview.example.com"})
		del := deliverySvc(t, db)
		if _, err := del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "123"}}); err != nil {
			t.Fatal(err)
		}
		bad, err := del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "124"}})
		if !errors.Is(err, domain.ErrInvalid) || len(bad.Keys) != 0 {
			t.Fatalf("schema-invalid delivery escaped: %+v %v", bad, err)
		}
		values, _, err := revisionSvc(t, db).ExportWithParameters(t.Context(), actor, scope, 0, false, map[string]string{"PR_NUMBER": "124"})
		if !errors.Is(err, domain.ErrInvalid) || len(values) != 0 {
			t.Fatalf("schema-invalid export escaped: %+v %v", values, err)
		}
		// Later live schema edits must not reinterpret the committed contract.
		execRaw(t, db, `UPDATE keys SET declaration='{"rule":{"type":"string"}}' WHERE id='key_fed_url'`)
		_, err = del.FetchAs(t.Context(), actor, scope, "", service.FetchOptions{Parameters: map[string]string{"PR_NUMBER": "124"}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("delivery used live schema instead of snapshot schema: %v", err)
		}
	})
}
