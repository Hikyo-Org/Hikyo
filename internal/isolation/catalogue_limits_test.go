package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func seedCatalogueLimitKeys(t *testing.T, db *store.DB, f definitionsFixture, count int) {
	t.Helper()
	rows := make([]string, count)
	for i := range rows {
		rows[i] = fmt.Sprintf("('key_limit_%s_%d','org_a','%s','KEY_%d','','config','',FALSE,'','{\"rule\":{\"type\":\"string\"}}','none','none',%s)", f.project, i, f.project, i, ts)
	}
	execRaw(t, db, "INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES "+strings.Join(rows, ","))
}

func TestConfiguredCatalogueLimits(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		t.Run("key creation above default and lower cap recovery", func(t *testing.T) {
			f := seedDefinitionsProject(t, db, "key_limit", false)
			seedCatalogueLimitKeys(t, db, f, 1000)
			keys := keySvc(t, db)
			actor := service.LocalPrincipal(alice)
			if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("NEW_KEY", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
				t.Fatalf("default cap bypassed: %v", err)
			}
			keys.DefaultKeysPerProject = 2000
			created, err := keys.Create(t.Context(), actor, f.scope(), keySpec("NEW_KEY", "config"), nil)
			if err != nil {
				t.Fatalf("raised cap refused key 1001: %v", err)
			}
			keys.DefaultKeysPerProject = 1000
			if _, err := keys.Rename(t.Context(), actor, f.scope(), created.ID, "RENAMED_KEY", nil); err != nil {
				t.Fatalf("lowered cap blocked editing existing key: %v", err)
			}
			if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("OVERFLOW", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) {
				t.Fatalf("lowered cap allowed growth: %v", err)
			}
		})
		t.Run("apply rechecks the current cap", func(t *testing.T) {
			f := seedDefinitionsProject(t, db, "plan_limit", false)
			svc := definitionsService(t, db)
			svc.DefaultKeysPerProject = 2
			bundle := parseDefinitions(t, exportPortableDefinitions(t, svc, f))
			bundle.Keys = append(bundle.Keys, definitionKey("FIRST", "config"), definitionKey("SECOND", "config"))
			plan := planDefinitions(t, svc, f, encodeDefinitions(t, bundle))
			svc.DefaultKeysPerProject = 1
			if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{}); !errors.Is(err, domain.ErrLimitExceeded) {
				t.Fatalf("apply used stale plan-time cap: %v", err)
			}
			svc.DefaultKeysPerProject = 2
			if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{}); err != nil {
				t.Fatalf("raised cap did not admit the plan: %v", err)
			}
			svc.DefaultKeysPerProject = 1
			plan = planDefinitions(t, svc, f, exportDefinitions(t, svc, f))
			if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{}); err != nil {
				t.Fatalf("lowered cap blocked unchanged existing definitions: %v", err)
			}
		})
		t.Run("key and environment writes enforce resolved cells", func(t *testing.T) {
			f := seedDefinitionsProject(t, db, "cell_limit", false)
			seedCatalogueLimitKeys(t, db, f, 2500)
			rows := make([]string, 40)
			for i := range rows {
				rows[i] = fmt.Sprintf("('env_limit_%d','org_a','%s','env-%d','',%s,%d)", i, f.project, i, ts, i)
			}
			execRaw(t, db, "INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES "+strings.Join(rows, ","))
			keys := keySvc(t, db)
			keys.DefaultKeysPerProject = 3000
			actor := service.LocalPrincipal(alice)
			if _, err := keys.Create(t.Context(), actor, f.scope(), keySpec("OVERFLOW", "config"), nil); !errors.Is(err, domain.ErrLimitExceeded) || !strings.Contains(safeError(err), "resolved cells") {
				t.Fatalf("key write bypassed cell budget: %v", err)
			}
			envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
			if _, err := envs.Create(t.Context(), actor, f.scope(), "overflow", nil); !errors.Is(err, domain.ErrLimitExceeded) || !strings.Contains(safeError(err), "resolved cells") {
				t.Fatalf("environment write bypassed cell budget: %v", err)
			}
		})
	})
}
