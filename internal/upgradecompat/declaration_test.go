package upgradecompat_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestDeclarationRejectsAmbiguousAndDescendingClaims(t *testing.T) {
	f := testfixture.New(t)
	m := manifest(releaseidentity.SQLite, 1)
	a := add(t, f, 1, m)
	b := add(t, f, 2, m, edge(a, m))
	for _, name := range []string{"unknown field", "duplicate field", "schema", "engine", "mode", "descending", "cycle", "duplicate source", "changed restart", "missing schema", "profile confusion"} {
		t.Run(name, func(t *testing.T) {
			var d upgradecompat.Declaration
			if err := json.Unmarshal(b.Material.Compatibility, &d); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "schema":
				d.Schema = "hikyo.dev/upgrade-compatibility/v2"
			case "engine":
				d.Engines[0].Migrations.Engine = "mysql"
			case "mode":
				d.Engines[0].Sources[0].Mode = "rolling"
			case "descending":
				d.Engines[0].Sources[0].Source.Release.Sequence = 3
			case "cycle":
				d.Engines[0].Sources[0].Source.Release = b.Identity
			case "duplicate source":
				d.Engines[0].Sources = append(d.Engines[0].Sources, d.Engines[0].Sources[0])
			case "changed restart":
				d.Engines[0].Sources[0].Mode = upgradecompat.Restart
				d.Engines[0].Migrations = manifest(releaseidentity.SQLite, 2)
			case "missing schema":
				d.Engines[0].SchemaSHA256 = ""
			case "profile confusion":
				d.Profile = releaseidentity.NightlyV1
			}
			raw := testfixture.JSON(t, d)
			if name == "unknown field" {
				raw = append([]byte(`{"extra":true,`), raw[1:]...)
			}
			if name == "duplicate field" {
				raw = append([]byte(`{"version":"forged",`), raw[1:]...)
			}
			if _, err := upgradecompat.Parse(raw); err == nil {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
}

func TestExactNodeAndEdgeLimits(t *testing.T) {
	for _, edges := range []int{1024, 1025} {
		t.Run(fmt.Sprint(edges), func(t *testing.T) {
			f := testfixture.New(t)
			m := manifest(releaseidentity.SQLite, 1)
			releases := []testfixture.SignedRelease{}
			for seq := 1; seq <= 256; seq++ {
				sources := []upgradecompat.SourceEdge{}
				if seq >= 253 {
					for _, source := range releases {
						sources = append(sources, edge(source, m))
					}
				}
				if seq == 252 {
					for _, source := range releases[:edges-1014] {
						sources = append(sources, edge(source, m))
					}
				}
				releases = append(releases, add(t, f, seq, m, sources...))
			}
			snapshot := f.Snapshot(t)
			ns := nodes(t, snapshot, releases...)
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: releases[0].Identity}, Migrations: m, SchemaSHA256: catalog(m)}
			plan, err := upgradecompat.PlanRoute(snapshot, source, releases[255].Identity, ns, nil)
			if edges == 1024 {
				if err != nil || len(plan.Steps()) != 1 {
					t.Fatal("exact256-node1024-edge graph rejected", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "edge bound") {
				t.Fatal("1025 edges not refused at bound", err)
			}
		})
	}
}
