package lint

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
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
		apis, err := readGeneratedContracts(filepath.Join(root, "internal/store", output), engine)
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
	protocols, err := inventory.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	loaded := map[string][]*packages.Package{}
	dependencies := map[string]map[string]map[string]string{}
	inventory.Helpers, inventory.BuildHelpers = map[string]string{}, map[string]map[string]string{}
	for _, context := range Contexts {
		pkgs, err := LoadRepoIn(context)
		if err != nil {
			t.Fatal(err)
		}
		loaded[context.Name] = pkgs
		declarations := rawSQLDeclarations(pkgs, root, Module)
		for owner, pin := range inventory.Protocols {
			declaration, exists := declarations[owner]
			if !exists {
				t.Fatalf("protocol %s missing in %s", owner, context.Name)
			}
			hash, err := rawSQLBodyHash(declaration.pkg, declaration.fn)
			if err != nil {
				t.Fatal(err)
			}
			if context.Name == Contexts[0].Name {
				pin.Hash = hash
			} else if pin.Hash != hash {
				t.Fatalf("protocol body differs across builds: %s", owner)
			}
			for caller := range pin.Callers {
				declaration, exists := declarations[caller]
				if !exists {
					t.Fatalf("reviewed caller %s missing in %s", caller, context.Name)
				}
				hash, err := rawSQLBodyHash(declaration.pkg, declaration.fn)
				if err != nil {
					t.Fatal(err)
				}
				if context.Name == Contexts[0].Name {
					pin.Callers[caller] = hash
				} else if pin.Callers[caller] != hash {
					t.Fatalf("caller body differs across builds: %s", caller)
				}
			}
			inventory.Protocols[owner] = pin
			if dependencies[owner] == nil {
				dependencies[owner] = map[string]map[string]string{}
			}
			deps := rawSQLDependencies(owner, declarations, protocols, root)
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
		for helper, hash := range contexts[Contexts[0].Name] {
			same := true
			for _, context := range Contexts {
				same = same && contexts[context.Name][helper] == hash
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
	protocols, err = inventory.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	for context, pkgs := range loaded {
		if findings := CheckRawSQL(pkgs, root, Module, protocols, context); len(findings) != 0 {
			t.Fatalf("update refuses ownership drift in %s: %v", context, findings)
		}
	}
	if err := checkReviewedPinAuthorities(reviewedPins, inventory); err != nil {
		t.Fatal(err)
	}
	source, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseReviewedPins(source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/lint/testdata/reviewed_pins.json"), append(source, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	t.Log("Existing hashes updated. Review the diff, then rerun normal CI without -update-reviewed-pins.")
}

func cloneReviewedPins(original reviewedPinInventory) (reviewedPinInventory, error) {
	source, err := json.Marshal(original)
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
		return json.Marshal(clone)
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
	before, err := json.Marshal(reviewedPins)
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
	after, err := json.Marshal(reviewedPins)
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
