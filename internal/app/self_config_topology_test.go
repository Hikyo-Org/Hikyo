package app

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestBootstrapSingletonTopologyRequiresActualPrerequisites(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(map[bool]string{false: "sqlite", true: "postgres"}[postgres], func(t *testing.T) {
			d, _, _ := deploymentAdapterFixture(t, postgres)
			d.cfg.NodeID = "local"
			d.cfg.RootKeyFile = devRootKeyPath(d.cfg)
			d.enrollment.Target.TopologyNodeIDs = []string{"local", "renamed"}
			d.installed.Topology = domain.SingletonTopology{NodeID: "local"}
			selected := d.installed
			selected.Topology.HA = true
			prepared, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, selected), 1)
			if !postgres {
				if !errors.Is(err, configrollout.ErrUnsupported) {
					t.Fatalf("SQLite HA: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Command.Topology == nil || prepared.Command.Topology.After != selected.Topology {
				t.Fatal("missing signed mode correspondence")
			}
			submitted, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 2, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
			if err != nil {
				t.Fatal(err)
			}
			if submitted.Command.Topology == nil || *submitted.Command.Topology != *prepared.Command.Topology {
				t.Fatal("submit lost membership correspondence")
			}
			if err := d.VerifyInstalled(t.Context(), deploymentBundle(t, selected)); !errors.Is(err, service.ErrDeploymentSourcesPending) {
				t.Fatalf("old process acknowledged desired mode: %v", err)
			}
			selected.Topology.NodeID = "foreign"
			if _, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, selected), 3); err == nil {
				t.Fatal("uninstalled identity accepted")
			}
			d.cfg.RootKeyFile = ""
			d.cfg.RootKeyFromEnv = false
			selected.Topology.NodeID = "local"
			if _, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, selected), 4); err == nil {
				t.Fatal("shared key prerequisite bypassed")
			}
		})
	}
}

func TestSingletonReplacementBootUsesActualHAMode(t *testing.T) {
	cfg := nodePostgresConfig(t)
	cfg.NodeID = "stable-server"
	first, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if first.owner.haCoord != nil {
		t.Fatal("singleton unexpectedly coordinated")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.RootKeyFile = devRootKeyPath(cfg)
	cfg.HA = true
	enabled, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if enabled.owner.haCoord == nil || enabled.owner.current.graph.scheduler.Lease == nil || enabled.owner.current.graph.scheduler.NodeID != cfg.NodeID {
		t.Fatal("replacement boot did not install HA lease")
	}
	if err := enabled.Close(); err != nil {
		t.Fatal(err)
	}
	disabledCfg := *cfg
	disabledCfg.HA = false
	disabledCfg.NodeID = "renamed-server"
	disabled, err := Boot(t.Context(), &disabledCfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if disabled.owner.haCoord != nil || disabled.owner.current.graph.scheduler.Lease != nil || disabled.selfConfig.NodeID != "renamed-server" {
		t.Fatal("replacement boot retained HA or stale identity")
	}
}

func TestEnrolledSourceLoaderAcceptsOnlyExplicitSingletonHAIdentity(t *testing.T) {
	d, _, _ := deploymentAdapterFixture(t, false)
	cfg := *d.cfg
	cfg.HA = true
	cfg.NodeID = "renamed"
	cfg.ConfigRolloutEnrollment = filepath.Join(t.TempDir(), "enrollment.json")
	d.enrollment.Target.TopologyNodeIDs = []string{"local", "renamed"}
	raw, err := json.Marshal(d.enrollment)
	if err != nil {
		t.Fatal(err)
	}
	writeDeploymentFixture(t, cfg.ConfigRolloutEnrollment, string(raw), 0444)
	sources, err := loadEnrolledRootSources(&cfg, d.sourcesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if alias, err := sources.nextRootAlias(filepath.Join(d.sourcesDirectory, "root", "root-next", "root-key")); err != nil || alias != "root-next" {
		t.Fatalf("enrolled HA candidate source: %v", err)
	}
	cfg.NodeID = "foreign"
	if _, err := loadEnrolledRootSources(&cfg, d.sourcesDirectory); err == nil {
		t.Fatal("foreign HA identity accepted")
	}
	cfg.NodeID = "local"
	d.enrollment.Target.TopologyNodeIDs = nil
	raw, err = json.Marshal(d.enrollment)
	if err != nil {
		t.Fatal(err)
	}
	writeDeploymentFixture(t, cfg.ConfigRolloutEnrollment, string(raw), 0444)
	if _, err := loadEnrolledRootSources(&cfg, d.sourcesDirectory); err == nil {
		t.Fatal("unenrolled ordinary HA gained source authority")
	}
}

func TestBootstrapInitialHASourceCarriesUnchangedCorrespondenceAndReproof(t *testing.T) {
	d, _, _ := deploymentAdapterFixture(t, true)
	d.cfg.HA, d.cfg.NodeID, d.cfg.RootKeyFile = true, "local", devRootKeyPath(d.cfg)
	d.enrollment.Target.TopologyNodeIDs = []string{"local", "renamed"}
	d.installed.Topology = domain.SingletonTopology{HA: true, NodeID: "local"}
	selected := d.installed
	selected.DatabaseSource = "database-next"
	prepared, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, selected), 1)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Command.Topology == nil || prepared.Command.Topology.Before != d.installed.Topology || prepared.Command.Topology.After != d.installed.Topology || prepared.Command.Bootstrap == nil {
		t.Fatal("initial source plan omitted unchanged membership correspondence")
	}
	submitted, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 2, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Command.Topology == nil || *submitted.Command.Topology != *prepared.Command.Topology {
		t.Fatal("submit dropped initial membership correspondence")
	}
	// The correspondence must not bypass the existing source proof at commit.
	writeDeploymentFixture(t, filepath.Join(d.sourcesDirectory, "database/database-next/dsn"), "postgres://uninstalled.invalid/hikyo", 0440)
	if _, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 3, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil); err == nil {
		t.Fatal("unchanged correspondence bypassed source reproof")
	}
}
