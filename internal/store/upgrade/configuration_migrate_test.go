package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

func migrationCatalogue(t *testing.T) *CandidateConfiguration {
	t.Helper()
	p := &CandidateConfiguration{SchemaVersion: runtimeconfig.SchemaVersion}
	for _, key := range runtimeconfig.Catalogue() {
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := compiled.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		p.Catalogue = append(p.Catalogue, CandidateConfigurationKey{Name: key.Name, Classification: string(key.Classification), Declaration: string(declaration), RequiredMode: "none", ForbiddenMode: "none"})
	}
	return p
}

func TestConfigurationMigrationPreview(t *testing.T) {
	for _, scenario := range []string{"old-catalogue", "missing-value", "true", "false", "zero-and-empty", "unexpected", "incompatible", "invalid", "missing-unregistered"} {
		t.Run(scenario, func(t *testing.T) {
			p := migrationCatalogue(t)
			values := map[string]string{"HIKYO_REAUTH_WINDOW_SECONDS": "0", "HIKYO_MAIL_PASSWORD": ""}
			switch scenario {
			case "old-catalogue":
				for i, k := range p.Catalogue {
					if k.Name == "HIKYO_MCP_WRITE_ENABLED" {
						p.Catalogue = append(p.Catalogue[:i], p.Catalogue[i+1:]...)
						break
					}
				}
			case "true", "false":
				values["HIKYO_MCP_WRITE_ENABLED"] = scenario
			case "unexpected":
				p.Catalogue = append(p.Catalogue, CandidateConfigurationKey{Name: "UNEXPECTED"})
			case "incompatible":
				p.Catalogue[0].Declaration = "{}"
			case "invalid":
				values["HIKYO_MCP_WRITE_ENABLED"] = "bad"
			case "missing-unregistered":
				p.Catalogue = p.Catalogue[1:]
			}
			before := maps.Clone(values)
			beforeCatalogue := append([]CandidateConfigurationKey(nil), p.Catalogue...)
			got, next, err := PreviewConfigurationMigrations(p, values)
			wantError := scenario == "unexpected" || scenario == "incompatible" || scenario == "invalid" || scenario == "missing-unregistered"
			if (err != nil) != wantError {
				t.Fatalf("preview error: %v", err)
			}
			if !reflect.DeepEqual(before, values) || !reflect.DeepEqual(beforeCatalogue, p.Catalogue) {
				t.Fatal("preview mutated source")
			}
			if wantError {
				return
			}
			want := "false"
			if scenario == "true" {
				want = "true"
			}
			if next["HIKYO_MCP_WRITE_ENABLED"] != want || next["HIKYO_REAUTH_WINDOW_SECONDS"] != "0" || next["HIKYO_MAIL_PASSWORD"] != "" {
				t.Fatal("operator bytes or default changed")
			}
			if len(got.Catalogue) != len(runtimeconfig.Catalogue()) {
				t.Fatal("declaration not migrated")
			}
		})
	}
}

type migrationKeyStore struct {
	*freshKeyStore
	reader *candidateKeys
}

func (f *migrationKeyStore) ActiveMasterWrappers(ctx context.Context) ([]crypto.WrappedKey, error) {
	return f.reader.ActiveMasterWrappers(ctx)
}
func (f *migrationKeyStore) AllOpenableTier3(ctx context.Context) ([]crypto.WrappedKey, error) {
	return f.reader.AllOpenableTier3(ctx)
}
func (f *migrationKeyStore) Tier3Versions(ctx context.Context, p crypto.Purpose, org, project string) ([]crypto.WrappedKey, error) {
	rows, err := f.reader.AllOpenableTier3(ctx)
	if err != nil {
		return nil, err
	}
	var out []crypto.WrappedKey
	for _, k := range rows {
		if k.Purpose == p && k.OrgID == org && k.ProjectID == project {
			out = append(out, k)
		}
	}
	return out, nil
}
func (f *migrationKeyStore) ActiveTier3(ctx context.Context, p crypto.Purpose, org, project string) (crypto.WrappedKey, error) {
	rows, err := f.Tier3Versions(ctx, p, org, project)
	if err != nil {
		return crypto.WrappedKey{}, err
	}
	if len(rows) == 0 {
		return crypto.WrappedKey{}, crypto.ErrNoKey
	}
	return rows[len(rows)-1], nil
}
func (f *migrationKeyStore) CreateTier3(ctx context.Context, k crypto.WrappedKey) error {
	_, err := f.session.conn.ExecContext(ctx, "INSERT INTO tier3_keys(id,purpose,org_id,project_id,version,master_key_version,state,blob,created_at) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8)", k.ID, string(k.Purpose), k.OrgID, k.ProjectID, k.Version, k.MasterKeyVersion, k.Blob, "2026-09-19T00:00:00Z")
	return err
}

func seedConfigurationMigration(t *testing.T, s *Session, scenario string) (State, []byte, map[string]string) {
	t.Helper()
	ctx := t.Context()
	state := prepareSessionMigration(t, s, "testdata/session-key-inventory")
	var err error
	state, err = s.Advance(ctx, state, SchemaWriteStarted)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.applyEmbedded(ctx, os.DirFS("../migrations/"+string(s.engine))); err != nil {
		t.Fatal(err)
	}
	state, err = s.Advance(ctx, state, SchemaApplied)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.CandidateKeys(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	root := bytes.Repeat([]byte{7}, crypto.KeySize)
	fresh := &freshKeyStore{session: s}
	if err := crypto.InitializeFreshHierarchy(ctx, fresh, bytes.Clone(root)); err != nil {
		t.Fatal(err)
	}
	ks := &migrationKeyStore{freshKeyStore: fresh, reader: reader}
	kr, err := crypto.LoadKeyring(ctx, ks, bytes.Clone(root))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, sealer, err := kr.PrepareNewProject("org", "project")
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.CreateTier3(ctx, wrapped); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org','owner',TRUE,'{}','2026-09-19T00:00:00Z'),('remote','remote',TRUE,'{}','2026-09-19T00:00:00Z')",
		"INSERT INTO projects(id,org_id,name,created_at) VALUES('project','org','self','2026-09-19T00:00:00Z'),('remote','remote','remote','2026-09-19T00:00:00Z')",
		"INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES('env','org','project','runtime','','2026-09-19T00:00:00Z',0)",
		"INSERT INTO project_schema_revisions VALUES('org','project',27),('remote','remote',0)",
		"INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES('snapshot','org','project','env',1,27,'operator','2026-09-19T00:00:00Z')",
	} {
		if _, err := s.conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.conn.ExecContext(ctx, "INSERT INTO self_config_binding(id,owner_instance_id,adoption_key,adopted_by,org_id,project_id,environment_id,schema_version,generation,desired_revision,desired_snapshot_id,incarnation,created_at,updated_at) VALUES(1,$1,'adoption','operator','org','project','env',1,1,1,'snapshot','incarnation','2026-09-19T00:00:00Z','2026-09-19T00:00:00Z')", state.InstanceID); err != nil {
		t.Fatal(err)
	}
	incarnation, err := state.RecoveryIncarnation.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET incarnation=$1", string(incarnation)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.conn.ExecContext(ctx, "INSERT INTO self_config_retention VALUES('desired','snapshot')"); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"HIKYO_REAUTH_WINDOW_SECONDS": "0", "HIKYO_MAIL_PASSWORD": "  preserve\nbytes  ", "HIKYO_MCP_ENABLED": "false"}
	if scenario == "existing-true" {
		values["HIKYO_MCP_ENABLED"] = "true"
		values["HIKYO_MCP_WRITE_ENABLED"] = "true"
	}
	if scenario == "existing-false" {
		values["HIKYO_MCP_WRITE_ENABLED"] = "false"
	}
	if scenario == "invalid-value" {
		values["HIKYO_MCP_ENABLED"] = "invalid"
	}
	for _, key := range migrationCatalogue(t).Catalogue {
		if key.Name == "HIKYO_MCP_WRITE_ENABLED" && scenario != "missing-value" && scenario != "existing-true" && scenario != "existing-false" {
			continue
		}
		if _, err := s.conn.ExecContext(ctx, "INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES($1,'org','project',$1,'',$2,'',FALSE,'',$3,'none','none','2026-09-19T00:00:00Z')", key.Name, key.Classification, key.Declaration); err != nil {
			t.Fatal(err)
		}
		value, ok := values[key.Name]
		if !ok {
			continue
		}
		valID := "value-" + key.Name
		entryID := "entry-" + key.Name
		cipher, err := sealer.SealValue(crypto.ValueAAD{OrgID: "org", ProjectID: "project", EnvID: "env", KeyID: key.Name, RowID: valID, FieldTag: "value"}, []byte(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.conn.ExecContext(ctx, "INSERT INTO value_entries VALUES($1,'org','project','env',$2,$3,'2026-09-19T00:00:00Z','operator')", valID, key.Name, cipher); err != nil {
			t.Fatal(err)
		}
		cipher, err = sealer.SealField(crypto.ProjectFieldAAD{OrgID: "org", ProjectID: "project", EnvironmentID: "env", SnapshotID: "snapshot", KeyID: key.Name, OwnerTable: "snapshot_entries", OwnerRowID: entryID, FieldTag: "snapshot_value"}, []byte(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.conn.ExecContext(ctx, "INSERT INTO snapshot_entries VALUES($1,'org','project','env','snapshot',$2,$2,$3,$4,$5)", entryID, key.Name, key.Classification, cipher, valID); err != nil {
			t.Fatal(err)
		}
	}
	return state, root, values
}

func TestManagedConfigurationMigrationBothDatabases(t *testing.T) {
	for _, scenario := range []string{"old-catalogue", "missing-value", "existing-true", "existing-false", "invalid-value", "catalogue-drift", "foreign-owner", "rollback", "stale-generation", "suspended", "restored", "retiring-only"} {
		t.Run(scenario, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				err := WithLock(t.Context(), cfg, func(s *Session) error {
					state, root, values := seedConfigurationMigration(t, s, scenario)
					ctx := t.Context()
					beforePayload := retainedMigrationPayload(t, s)
					switch scenario {
					case "catalogue-drift":
						if _, err := s.conn.ExecContext(ctx, "UPDATE keys SET declaration='{}' WHERE name='HIKYO_MCP_ENABLED'"); err != nil {
							return err
						}
					case "foreign-owner":
						if _, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET owner_instance_id='remote'"); err != nil {
							return err
						}
					case "retiring-only":
						if _, err := s.conn.ExecContext(ctx, "UPDATE tier3_keys SET state='retiring' WHERE purpose='project'"); err != nil {
							return err
						}
					case "suspended":
						if _, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET suspended=TRUE"); err != nil {
							return err
						}
					case "restored":
						if _, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET incarnation='stale'"); err != nil {
							return err
						}
					case "stale-generation":
						state.Generation++
					case "rollback":
						s.beforeCommit = func() error { return errors.New("commit refused") }
					}
					check := func(_ context.Context, p *CandidateConfiguration, v map[string]string) error {
						if len(p.Catalogue) != len(runtimeconfig.Catalogue()) {
							return errors.New("catalogue incomplete")
						}
						for k, want := range values {
							if v[k] != want {
								return fmt.Errorf("changed %s", k)
							}
						}
						return nil
					}
					beforeState := migrationStateCounts(t, s)
					err := s.MigrateConfiguration(ctx, state, bytes.Clone(root), check)
					if !reflect.DeepEqual(beforePayload, retainedMigrationPayload(t, s)) {
						return errors.New("migration rewrote existing ciphertext")
					}
					failure := scenario == "invalid-value" || scenario == "catalogue-drift" || scenario == "foreign-owner" || scenario == "rollback" || scenario == "stale-generation" || scenario == "suspended" || scenario == "restored" || scenario == "retiring-only"
					if failure {
						if !reflect.DeepEqual(beforeState, migrationStateCounts(t, s)) {
							return errors.New("failed migration left partial writes")
						}
						if err == nil {
							return errors.New("invalid migration accepted")
						}
						var count int
						if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM snapshots").Scan(&count); err != nil {
							return err
						}
						if count != 1 {
							return errors.New("failure changed snapshots")
						}
						return nil
					}
					if err != nil {
						return err
					}
					keys, err := s.CandidateKeys(ctx, state)
					if err != nil {
						return err
					}
					p, err := keys.Configuration(ctx)
					if err != nil {
						return err
					}
					got, err := crypto.OpenExistingProjectFields(ctx, keys, bytes.Clone(root), p.OrgID, p.ProjectID, p.Fields)
					if err != nil {
						return err
					}
					want := maps.Clone(values)
					if _, ok := want["HIKYO_MCP_WRITE_ENABLED"]; !ok {
						want["HIKYO_MCP_WRITE_ENABLED"] = "false"
					}
					if !reflect.DeepEqual(got, want) {
						return errors.New("operator values changed")
					}
					var generation, revision, version, count, audits int
					if err := s.conn.QueryRowContext(ctx, "SELECT generation,desired_revision,migration_version FROM self_config_binding").Scan(&generation, &revision, &version); err != nil {
						return err
					}
					changed := scenario != "existing-true" && scenario != "existing-false"
					wantRevision := 1
					if changed {
						wantRevision = 2
					}
					if generation != wantRevision || revision != wantRevision || version != 1 {
						return fmt.Errorf("revision semantics %d %d %d", generation, revision, version)
					}
					var schemaRevision int
					if err := s.conn.QueryRowContext(ctx, "SELECT revision FROM project_schema_revisions WHERE org_id='org' AND project_id='project'").Scan(&schemaRevision); err != nil {
						return err
					}
					expectedSchema := 27
					if scenario == "old-catalogue" {
						expectedSchema = 28
					}
					if schemaRevision != expectedSchema {
						return fmt.Errorf("schema revision %d want %d", schemaRevision, expectedSchema)
					}
					if changed {
						var previous, desired string
						if err := s.conn.QueryRowContext(ctx, "SELECT snapshot_id FROM self_config_retention WHERE slot='previous'").Scan(&previous); err != nil {
							return err
						}
						if err := s.conn.QueryRowContext(ctx, "SELECT snapshot_id FROM self_config_retention WHERE slot='desired'").Scan(&desired); err != nil {
							return err
						}
						if previous != "snapshot" || desired == "snapshot" || desired != p.Fields[0].AAD.SnapshotID {
							return errors.New("retention did not preserve source")
						}
						var id string
						var cipher []byte
						if err := s.conn.QueryRowContext(ctx, "SELECT id,ciphertext FROM value_entries WHERE org_id='org' AND project_id='project' AND environment_id='env' AND key_id IN (SELECT id FROM keys WHERE name='HIKYO_MCP_WRITE_ENABLED')").Scan(&id, &cipher); err != nil {
							return err
						}
						if err := crypto.WithExistingProjectSealer(ctx, keys, bytes.Clone(root), "org", "project", func(sealer *crypto.ProjectSealer) error {
							var keyID string
							for _, field := range p.Fields {
								if field.Name == "HIKYO_MCP_WRITE_ENABLED" {
									keyID = field.AAD.KeyID
								}
							}
							plain, err := sealer.OpenValue(crypto.ValueAAD{OrgID: "org", ProjectID: "project", EnvID: "env", KeyID: keyID, RowID: id, FieldTag: "value"}, cipher)
							defer crypto.Zero(plain)
							if err != nil {
								return err
							}
							if string(plain) != "false" {
								return errors.New("live default is not disabled")
							}
							return nil
						}); err != nil {
							return err
						}
					}
					for range 2 {
						if err := s.MigrateConfiguration(ctx, state, bytes.Clone(root), check); err != nil {
							return err
						}
					}
					if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM snapshots").Scan(&count); err != nil {
						return err
					}
					if count != wantRevision {
						return errors.New("retry duplicated snapshots")
					}
					if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM audit_instance_events WHERE type='self_config.migrated'").Scan(&audits); err != nil {
						return err
					}
					if audits != wantRevision-1 {
						return errors.New("audit missing or duplicated")
					}
					if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM keys WHERE org_id='remote'").Scan(&count); err != nil {
						return err
					}
					if count != 0 {
						return errors.New("remote mutated")
					}
					healthy, err := s.Advance(ctx, state, Healthy)
					if err != nil {
						return err
					}
					if err := s.MigrateConfiguration(ctx, healthy, bytes.Clone(root), check); !errors.Is(err, ErrConflict) {
						return fmt.Errorf("healthy fence: %v", err)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestMissingMigrationNamesSetting(t *testing.T) {
	p := migrationCatalogue(t)
	p.Catalogue = p.Catalogue[1:]
	_, _, err := PreviewConfigurationMigrations(p, nil)
	if err == nil || !strings.Contains(err.Error(), "HIKYO_MAIL_ADDR") || !strings.Contains(err.Error(), "operator input") {
		t.Fatalf("actionable refusal: %v", err)
	}
}

func TestConfigurationMigrationsUsePerSettingDefaults(t *testing.T) {
	text := func(value string) *string { return &value }
	for _, tc := range []struct {
		name, key string
		fallback  *string
		existing  *string
		want      string
		refused   bool
	}{
		{name: "integer default", key: "HIKYO_REAUTH_WINDOW_SECONDS", fallback: text("600"), want: "600"},
		{name: "string default", key: "HIKYO_UPDATE_CHANNEL", fallback: text("stable"), want: "stable"},
		{name: "explicit zero", key: "HIKYO_REAUTH_WINDOW_SECONDS", fallback: text("600"), existing: text("0"), want: "0"},
		{name: "explicit empty", key: "HIKYO_MAIL_PASSWORD", fallback: text("default"), existing: text(""), want: ""},
		{name: "missing default", key: "HIKYO_UPDATE_CHANNEL", refused: true},
		{name: "invalid default", key: "HIKYO_REAUTH_WINDOW_SECONDS", fallback: text("false"), refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			if tc.existing != nil {
				values[tc.key] = *tc.existing
			}
			_, err := planConfigurationMigrationsWith(migrationCatalogue(t), values, 0, []runtimeconfig.Migration{{Version: 1, Name: tc.key, Default: tc.fallback}})
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), "operator input") {
					t.Fatalf("actionable error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if values[tc.key] != tc.want {
				t.Fatalf("got %q want %q", values[tc.key], tc.want)
			}
		})
	}
}

func retainedMigrationPayload(t *testing.T, s *Session) map[string]string {
	t.Helper()
	rows, err := s.conn.QueryContext(t.Context(), "SELECT id,ciphertext FROM snapshot_entries WHERE snapshot_id='snapshot' UNION ALL SELECT id,ciphertext FROM value_entries WHERE id LIKE 'value-%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id string
		var cipher []byte
		if err := rows.Scan(&id, &cipher); err != nil {
			t.Fatal(err)
		}
		out[id] = string(cipher)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
func migrationStateCounts(t *testing.T, s *Session) []int64 {
	t.Helper()
	queries := []string{"SELECT count(*) FROM keys", "SELECT count(*) FROM snapshots", "SELECT count(*) FROM snapshot_entries", "SELECT count(*) FROM value_entries", "SELECT count(*) FROM revision_key_changes", "SELECT count(*) FROM audit_instance_events", "SELECT count(*) FROM self_config_retention", "SELECT revision FROM project_schema_revisions WHERE org_id='org' AND project_id='project'", "SELECT migration_version FROM self_config_binding", "SELECT generation FROM self_config_binding", "SELECT desired_revision FROM self_config_binding"}
	var out []int64
	for _, query := range queries {
		var n int64
		if err := s.conn.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}
