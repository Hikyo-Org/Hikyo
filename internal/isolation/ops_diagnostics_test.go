package isolation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestOpsDiagnosticsAuthorityAndEscrowEpoch(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		now := time.Now().UTC()
		rootKey, err := (probeRootSource{db: db}).Current(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(rootKey)
		escrow := &service.Escrow{DB: db, Now: func() time.Time { return now }}
		health := &service.Retention{DB: db, Now: func() time.Time { return now }, Diagnostics: &service.Diagnostics{Passwords: &crypto.PasswordFloor}}
		if _, err := health.GetHealth(ctx, service.LocalPrincipal(nobody)); err == nil {
			t.Fatal("ungranted health read accepted")
		}
		get := func() service.PruneHealth {
			t.Helper()
			h, err := health.GetHealth(ctx, service.LocalPrincipal(root))
			if err != nil {
				t.Fatal(err)
			}
			if len(h.Diagnostics.Findings) != 7 {
				t.Fatal("incomplete diagnostic checklist")
			}
			return h
		}
		if get().Diagnostics.EscrowCurrent {
			t.Fatal("missing escrow became verified")
		}
		if err := escrow.Verify(ctx, bytes.Clone(rootKey), false); err == nil {
			t.Fatal("missing custody assertion accepted")
		}
		wrong, err := crypto.GenerateRootKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := escrow.Verify(ctx, wrong, true); !errors.Is(err, crypto.ErrRootKeyMismatch) {
			t.Fatalf("wrong escrow key: %v", err)
		}
		if err := escrow.Verify(ctx, bytes.Clone(rootKey), true); err != nil {
			t.Fatal(err)
		}
		if !get().Diagnostics.EscrowCurrent {
			t.Fatal("valid escrow not visible")
		}
		// No other system site or ordinary operator read can write the record.
		for _, site := range []authz.SystemSite{authz.SiteBoot, authz.SiteScheduler} {
			err := tx.Write(ctx, db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				p, err := authz.SystemAuthority(site, az.Token())
				if err != nil {
					return err
				}
				return r.Retention().RecordEscrow(ctx, p, store.EscrowRecord{At: now})
			})
			if err == nil {
				t.Fatalf("%s gained escrow write authority", site)
			}
		}
		newRoot, err := crypto.GenerateRootKey()
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(newRoot)
		source := &mutableRootSource{current: bytes.Clone(rootKey), next: bytes.Clone(newRoot)}
		rotation := &service.Rotation{DB: db, Keyring: probeKeyring(t, db), RootKey: source}
		if _, err := rotation.RotateRootKey(ctx, service.LocalPrincipal(root), service.RootRotatePrepare); err != nil {
			t.Fatal(err)
		}
		if get().Diagnostics.EscrowCurrent {
			t.Fatal("dual-wrapped hierarchy reused escrow record")
		}
		if err := escrow.Verify(ctx, bytes.Clone(newRoot), true); err == nil {
			t.Fatal("ambiguous dual-root escrow accepted")
		}
		source.install()
		if _, err := rotation.RotateRootKey(ctx, service.LocalPrincipal(root), service.RootRotateFinalize); err != nil {
			t.Fatal(err)
		}
		if get().Diagnostics.EscrowCurrent {
			t.Fatal("old root epoch reused escrow record")
		}
		if err := escrow.Verify(ctx, bytes.Clone(newRoot), true); err != nil {
			t.Fatal(err)
		}
		if !get().Diagnostics.EscrowCurrent {
			t.Fatal("current root epoch not verified")
		}
		// An archived attestation from a different recovery incarnation cannot
		// make a healthy new incarnation claim current escrow.
		execRaw(t, db, `UPDATE ops_diagnostics SET escrow_incarnation='old-recovery' WHERE singleton=1`)
		if get().Diagnostics.EscrowCurrent {
			t.Fatal("stale recovery attestation accepted")
		}
		// Runtime admission must also refuse recording after its source identity
		// changes. This controlled corrupt/stale source cannot mutate escrow.
		execRaw(t, db, `UPDATE upgrade_control SET incarnation='`+strings.Repeat("a", 64)+`' WHERE singleton=1`)
		if err := escrow.Verify(ctx, bytes.Clone(newRoot), true); err == nil {
			t.Fatal("stale admitted incarnation wrote escrow")
		}
	})
}

func TestOpsVolumeDiagnosticsBoundaries(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		health := &service.Retention{DB: db, Diagnostics: &service.Diagnostics{Passwords: &crypto.PasswordFloor}}
		for _, test := range []struct {
			free     uint64
			severity string
		}{{21, "ok"}, {20, "warn"}, {10, "error"}, {0, "error"}} {
			health.Diagnostics.Volume = func() (storagehealth.Capacity, error) {
				return storagehealth.Capacity{TotalBytes: 100, AvailableBytes: test.free}, nil
			}
			h, err := health.OperationalHealth(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !h.Diagnostics.Volume.Known || h.Diagnostics.Findings[0].Severity != test.severity {
				t.Fatalf("wrong boundary at free=%d", test.free)
			}
		}
		health.Diagnostics.Volume = func() (storagehealth.Capacity, error) { return storagehealth.Capacity{}, errors.New("unavailable") }
		h, err := health.OperationalHealth(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if h.Diagnostics.Volume.Known || h.Diagnostics.Findings[0].Severity != "unknown" {
			t.Fatal("unmeasured volume reported healthy")
		}
	})
}

func TestOpsDiagnosticsPinTierBoundaries(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
		health := &service.Retention{DB: db, Now: func() time.Time { return now }}
		// This fixture exercises aggregate age accounting only. Its workload remains
		// environment scoped and the snapshot carries no secret payload.
		execRaw(t, db, `UPDATE grants SET env_id='env_a1' WHERE id='g_wl_read'`)
		execRaw(t, db, `INSERT INTO snapshots (id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snp_ops_pin','org_a','prj_a1','env_a1',999,1,'usr_root','2026-09-01T00:00:00Z')`)
		execRaw(t, db, `INSERT INTO revision_pins (id,org_id,project_id,environment_id,workload_principal_id,snapshot_id,revision,authority_principal_id,expires_at,created_at,authorized_at,history_authorized,schema_override) VALUES ('pin_ops','org_a','prj_a1','env_a1','mch_workload','snp_ops_pin',999,'usr_root','2026-09-05T12:00:00Z','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z',TRUE,FALSE)`)
		for _, tc := range []struct {
			name   string
			offset time.Duration
			want   [4]int64
		}{
			{"expired-exact", 0, [4]int64{1, 0, 0, 0}},
			{"future", time.Second, [4]int64{0, 1, 0, 0}},
			{"day-exact", 24 * time.Hour, [4]int64{0, 1, 0, 0}},
			{"day-past", 24*time.Hour + time.Second, [4]int64{0, 0, 1, 0}},
			{"week-exact", 7 * 24 * time.Hour, [4]int64{0, 0, 1, 0}},
			{"week-past", 7*24*time.Hour + time.Second, [4]int64{0, 0, 0, 1}},
			{"month-exact", 30 * 24 * time.Hour, [4]int64{0, 0, 0, 1}},
			{"outside-window", 30*24*time.Hour + time.Second, [4]int64{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				execRaw(t, db, fmt.Sprintf("UPDATE revision_pins SET expires_at='%s' WHERE id='pin_ops'", now.Add(tc.offset).Format(time.RFC3339)))
				got, err := health.GetHealth(t.Context(), service.LocalPrincipal(root))
				if err != nil {
					t.Fatal(err)
				}
				m := got.Diagnostics.Metadata
				if counts := ([4]int64{m.PinsExpired, m.PinsDay, m.PinsWeek, m.PinsMonth}); counts != tc.want {
					t.Fatalf("counts %v want %v", counts, tc.want)
				}
			})
		}
	})
}

func TestOpsDiagnosticsReencryptCompletion(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		kr := probeKeyring(t, db)
		health := &service.Retention{DB: db}
		get := func() store.OpsMetadata {
			t.Helper()
			h, err := health.GetHealth(ctx, service.LocalPrincipal(root))
			if err != nil {
				t.Fatal(err)
			}
			return h.Diagnostics.Metadata
		}
		if !get().LastReencryptSuccess.IsZero() {
			t.Fatal("invented initial completion")
		}
		rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
		if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{OrgID: "org_a", ProjectID: "prj_a1"}); err != nil {
			t.Fatal(err)
		}
		before := get()
		if before.RetiringScopes != 1 || !before.LastReencryptSuccess.IsZero() {
			t.Fatalf("rotation metadata %+v", before)
		}
		re := &service.Reencrypt{DB: db, Keyring: kr, ChunkSize: 1, ChunkPause: -1}
		if _, err := re.ReencryptProject(ctx, service.LocalPrincipal(nobody), "org_a", "prj_a1"); err == nil {
			t.Fatal("ungranted reencrypt accepted")
		}
		if !get().LastReencryptSuccess.IsZero() {
			t.Fatal("refused operation recorded success")
		}
		if _, err := re.ReencryptProject(ctx, service.LocalPrincipal(root), "org_a", "prj_a1"); err != nil {
			t.Fatal(err)
		}
		after := get()
		if after.RetiringScopes != 0 || after.LastReencryptSuccess.IsZero() {
			t.Fatalf("completion metadata %+v", after)
		}
	})
}
