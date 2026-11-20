package lint

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

var updateReviewedPins = flag.Bool("update-reviewed-pins", false, "rewrite existing reviewed hashes for explicit diff review; never add authorities or protocol owners")

// Run explicitly with:
// go test ./internal/lint -run '^TestUpdateReviewedPins$' -update-reviewed-pins
// Updating is not proof or approval: normal CI still checks the reviewed diff,
// query authority/evidence, protocol ownership, and all build-context closures.
func TestUpdateReviewedPins(t *testing.T) {
	if !*updateReviewedPins {
		t.Skip("explicit reviewed-pin update only")
	}
	if reviewedPinsError != nil {
		t.Fatal(reviewedPinsError)
	}
	root := repoRoot(t)
	inventory, err := cloneReviewedPins(reviewedPins)
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		queries, err := ParseQueries(filepath.Join(root, "internal/store/queries", engine))
		if err != nil {
			t.Fatal(err)
		}
		output := map[string]string{"sqlite": "sqlitegen", "postgres": "pggen"}[engine]
		apis, err := readGeneratedContracts(osSource{}, filepath.Join(root, "internal/store", output), engine)
		if err != nil {
			t.Fatal(err)
		}
		byName := map[string]Query{}
		for _, query := range queries {
			byName[query.Name] = query
		}
		for _, records := range []map[string]reviewedQueryPin{inventory.ContractDifferences, inventory.EngineQueries, inventory.ScopedQueries} {
			for name, pin := range records {
				if _, exists := pin.SQLHash[engine]; !exists {
					continue
				}
				query, exists := byName[name]
				api, generated := apis[name]
				if !exists || !generated || !api.BindSitesKnown {
					t.Fatalf("cannot update missing/unbound %s query %s", engine, name)
				}
				pin.SQLHash[engine], pin.APIHash[engine] = query.Hash(), api.hash()
			}
		}
	}
	protocols, err := inventory.rawProtocolsForGOOS("linux")
	if err != nil {
		t.Fatal(err)
	}
	// The vet tool reports the actual hashes (-rawsql.dump). The canonical pins
	// are linux's: the tool expands them for linux whatever the host (-rawsql.native),
	// and the default and ui contexts are analyzed as linux.
	bin := buildVetTool(t)
	dependencies := map[string]map[string]map[string]string{}
	bodies := map[string]string{} // owner and caller key -> hash, equal in every context
	inventory.Helpers, inventory.BuildHelpers = map[string]string{}, map[string]map[string]string{}
	for _, context := range Contexts {
		if context.GOOS == "" {
			context.GOOS = "linux"
		}
		active, err := rawSQLProtocolsInContext(protocols, context.Name)
		if err != nil {
			t.Fatal(err)
		}
		dump, err := rawSQLDump(runVet(t, bin, context, rawSQLUpdaterFlags(t, "-rawsql.dump"), []string{Module + "/..."}))
		if err != nil {
			t.Fatal(err)
		}
		for owner, pin := range inventory.Protocols {
			if _, applies := active[owner]; !applies {
				continue
			}
			keys := []string{owner}
			for caller := range pin.Callers {
				keys = append(keys, caller)
			}
			for _, key := range keys {
				hash, exists := dump.bodies[key]
				if !exists {
					t.Fatalf("protocol %s or reviewed caller %s missing in %s", owner, key, context.Name)
				}
				if previous, seen := bodies[key]; seen && previous != hash {
					t.Fatalf("protocol or caller body differs across builds: %s", key)
				}
				bodies[key] = hash
				if key == owner {
					pin.Hash = hash
				} else {
					pin.Callers[key] = hash
				}
			}
			inventory.Protocols[owner] = pin
			if dependencies[owner] == nil {
				dependencies[owner] = map[string]map[string]string{}
			}
			deps := dump.dependencies[owner]
			if deps == nil {
				deps = map[string]string{}
			}
			dependencies[owner][context.Name] = deps
			for helper, hash := range deps {
				if previous, exists := inventory.Helpers[helper]; !exists {
					inventory.Helpers[helper] = hash
				} else if previous != hash {
					if inventory.BuildHelpers[context.Name] == nil {
						inventory.BuildHelpers[context.Name] = map[string]string{}
					}
					inventory.BuildHelpers[context.Name][helper] = hash
				}
			}
		}
	}
	for owner, contexts := range dependencies {
		pin := inventory.Protocols[owner]
		pin.Dependencies = nil
		pin.BuildDependencies = map[string][]string{}
		common := map[string]bool{}
		var first map[string]string
		for _, context := range Contexts {
			if deps, exists := contexts[context.Name]; exists {
				first = deps
				break
			}
		}
		for helper, hash := range first {
			same := true
			for _, deps := range contexts {
				same = same && deps[helper] == hash
			}
			if same {
				common[helper] = true
				pin.Dependencies = append(pin.Dependencies, helper)
			}
		}
		slices.Sort(pin.Dependencies)
		for context, deps := range contexts {
			for helper := range deps {
				if !common[helper] {
					pin.BuildDependencies[context] = append(pin.BuildDependencies[context], helper)
				}
			}
			slices.Sort(pin.BuildDependencies[context])
		}
		inventory.Protocols[owner] = pin
	}
	if err := checkReviewedPinAuthorities(reviewedPins, inventory); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(inventory, jsonwire.Stable, jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseReviewedPins(source); err != nil {
		t.Fatal(err)
	}
	pinsPath := filepath.Join(root, filepath.FromSlash(reviewedPinsPath))
	if err := os.WriteFile(pinsPath, append(source, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	// The vet tool reads the pins from disk, so the update is verified after the
	// write and rolled back when ownership drifted.
	if findings := verifyUpdatedPins(t, bin); len(findings) != 0 {
		if err := os.WriteFile(pinsPath, reviewedPinsJSON, 0644); err != nil {
			t.Error("restoring the reviewed pins:", err)
		}
		t.Fatalf("update refuses ownership drift: %v", findings)
	}
	t.Log("Existing hashes updated. Review the diff, then rerun normal CI without -update-reviewed-pins.")
}

// rawSQLUpdaterFlags are the flags of a rawsql-only vet run for the updater.
func rawSQLUpdaterFlags(t *testing.T, extra ...string) []string {
	t.Helper()
	return append(append(repoInputFlags(t), "-rawsql", "-rawsql.native=linux"), extra...)
}

// verifyUpdatedPins vets the repository under every context against the pins on
// disk and returns what the per-package passes and the aggregate report.
func verifyUpdatedPins(t *testing.T, bin string) []string {
	t.Helper()
	var findings []string
	for _, context := range Contexts {
		if context.GOOS == "" {
			context.GOOS = "linux"
		}
		out := runVet(t, bin, context, rawSQLUpdaterFlags(t), []string{Module + "/..."})
		aggregated, err := rawSQLAggregate(out, moduleRoot())
		if err != nil {
			t.Fatal(err)
		}
		for _, finding := range append(findingMessages(out.Findings, RawSQL.Name), aggregated...) {
			findings = append(findings, context.Name+": "+finding)
		}
	}
	return findings
}

// rawSQLDumped are the actual hashes a -rawsql.dump run reported.
type rawSQLDumped struct {
	bodies       map[string]string            // key -> body hash
	dependencies map[string]map[string]string // owner -> helper key -> hash
}

func rawSQLDump(out *vetOutcome) (rawSQLDumped, error) {
	dump := rawSQLDumped{bodies: map[string]string{}, dependencies: map[string]map[string]string{}}
	for _, s := range out.Summaries[RawSQL.Name] {
		if s.Kind != summaryRawDump {
			continue
		}
		fields := strings.Fields(s.Subject)
		switch {
		case len(fields) == 3 && fields[0] == "fn":
			dump.bodies[fields[1]] = fields[2]
		case len(fields) == 4 && fields[0] == "dep":
			if dump.dependencies[fields[1]] == nil {
				dump.dependencies[fields[1]] = map[string]string{}
			}
			dump.dependencies[fields[1]][fields[2]] = fields[3]
		default:
			return rawSQLDumped{}, fmt.Errorf("malformed %s summary %q", s.Kind, s.Subject)
		}
	}
	return dump, nil
}

func cloneReviewedPins(original reviewedPinInventory) (reviewedPinInventory, error) {
	source, err := json.Marshal(original, jsonwire.Stable)
	if err != nil {
		return reviewedPinInventory{}, err
	}
	var clone reviewedPinInventory
	err = json.Unmarshal(source, &clone)
	return clone, err
}

// Hashes and reachable helper closures may change in an explicit update. The
// owner/caller allowlist and every authority/evidence/engine declaration may not.
func checkReviewedPinAuthorities(before, after reviewedPinInventory) error {
	identity := func(inventory reviewedPinInventory) ([]byte, error) {
		clone, err := cloneReviewedPins(inventory)
		if err != nil {
			return nil, err
		}
		clone.Helpers, clone.BuildHelpers = nil, nil
		for _, records := range []map[string]reviewedQueryPin{clone.ContractDifferences, clone.EngineQueries, clone.ScopedQueries} {
			for name, pin := range records {
				for engine := range pin.SQLHash {
					pin.SQLHash[engine] = ""
				}
				for engine := range pin.APIHash {
					pin.APIHash[engine] = ""
				}
				records[name] = pin
			}
		}
		for owner, pin := range clone.Protocols {
			pin.Hash, pin.Dependencies, pin.BuildDependencies = "", nil, nil
			for caller := range pin.Callers {
				pin.Callers[caller] = ""
			}
			clone.Protocols[owner] = pin
		}
		return json.Marshal(clone, jsonwire.Stable)
	}
	original, err := identity(before)
	if err != nil {
		return err
	}
	updated, err := identity(after)
	if err != nil {
		return err
	}
	if !bytes.Equal(original, updated) {
		return fmt.Errorf("reviewed-pin update changed owners, authorities, engines, evidence or caller allowlists")
	}
	return nil
}

func TestReviewedPinUpdatePreservesAuthorityAndOriginalMaps(t *testing.T) {
	before, err := json.Marshal(reviewedPins, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := cloneReviewedPins(reviewedPins)
	if err != nil {
		t.Fatal(err)
	}
	for _, records := range []map[string]reviewedQueryPin{updated.ContractDifferences, updated.EngineQueries, updated.ScopedQueries} {
		for _, pin := range records {
			for engine := range pin.SQLHash {
				pin.SQLHash[engine] = "updated"
			}
			for engine := range pin.APIHash {
				pin.APIHash[engine] = "updated"
			}
		}
	}
	for owner, pin := range updated.Protocols {
		pin.Hash = "updated"
		for caller := range pin.Callers {
			pin.Callers[caller] = "updated"
		}
		pin.Dependencies = append(pin.Dependencies, "newly-reachable-helper")
		updated.Protocols[owner] = pin
	}
	updated.Helpers["newly-reachable-helper"] = "updated"
	if err := checkReviewedPinAuthorities(reviewedPins, updated); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(reviewedPins, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("updater mutated original shared maps")
	}
	for name, widen := range map[string]func(*reviewedPinInventory){
		"owner": func(v *reviewedPinInventory) {
			v.Protocols["new.go:owner"] = rawSQLProtocolPin{Reason: "new authority"}
		},
		"caller": func(v *reviewedPinInventory) {
			for owner, pin := range v.Protocols {
				if pin.Callers == nil {
					pin.Callers = map[string]string{}
				}
				pin.Callers["new.go:caller"] = "pin"
				v.Protocols[owner] = pin
				break
			}
		},
		"reason": func(v *reviewedPinInventory) {
			for owner, pin := range v.Protocols {
				pin.Reason = "changed"
				v.Protocols[owner] = pin
				break
			}
		},
		"authority": func(v *reviewedPinInventory) {
			for name, pin := range v.ScopedQueries {
				pin.Authority = "changed"
				v.ScopedQueries[name] = pin
				break
			}
		},
		"engine": func(v *reviewedPinInventory) {
			for name, pin := range v.ScopedQueries {
				pin.SQLHash["new-engine"] = "new"
				v.ScopedQueries[name] = pin
				break
			}
		},
		"evidence": func(v *reviewedPinInventory) {
			for name, pin := range v.ScopedQueries {
				pin.Tests = []string{"different_test.go:TestDifferent"}
				v.ScopedQueries[name] = pin
				break
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := cloneReviewedPins(updated)
			if err != nil {
				t.Fatal(err)
			}
			widen(&candidate)
			if err := checkReviewedPinAuthorities(reviewedPins, candidate); err == nil {
				t.Fatal("authority widening accepted")
			}
		})
	}
}
