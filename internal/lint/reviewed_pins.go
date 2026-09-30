package lint

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
)

// Query pins share one representation. Proof consumers still distinguish
// cross-engine differences, engine protocols and reviewed tenant scope.
type reviewedQueryPin struct {
	Authority string            `json:"authority"`
	SQLHash   map[string]string `json:"sql_hash"`
	APIHash   map[string]string `json:"api_hash"`
	Tests     []string          `json:"tests,omitempty"`
	FlowTests []string          `json:"flow_tests,omitempty"`
}

type rawSQLProtocolPin struct {
	Hash              string              `json:"hash"`
	Reason            string              `json:"reason"`
	Callers           map[string]string   `json:"callers,omitempty"`
	Dependencies      []string            `json:"dependencies,omitempty"`
	BuildDependencies map[string][]string `json:"build_dependencies,omitempty"`
}

type reviewedPinInventory struct {
	ContractDifferences map[string]reviewedQueryPin  `json:"contract_differences"`
	EngineQueries       map[string]reviewedQueryPin  `json:"engine_queries"`
	ScopedQueries       map[string]reviewedQueryPin  `json:"scoped_queries"`
	Protocols           map[string]rawSQLProtocolPin `json:"protocols"`
	Helpers             map[string]string            `json:"helpers"`
	BuildHelpers        map[string]map[string]string `json:"build_helpers,omitempty"`
}

//go:embed testdata/reviewed_pins.json
var reviewedPinsJSON []byte

// Invalid checked-in metadata is a CI finding, never an application startup
// panic. Failed decoding exposes no query exception maps.
var reviewedPins, reviewedPinsError = parseReviewedPins(reviewedPinsJSON)
var approvedContractDifferences, approvedEngineOnlyQueries = queryContractPins(reviewedPins, reviewedPinsError)

func parseReviewedPins(source []byte) (reviewedPinInventory, error) {
	var inventory reviewedPinInventory
	if err := definitions.DecodeStrict(source, &inventory); err != nil {
		return inventory, err
	}
	for kind, records := range map[string]map[string]reviewedQueryPin{"contract difference": inventory.ContractDifferences, "engine protocol": inventory.EngineQueries, "scoped query": inventory.ScopedQueries} {
		for name, pin := range records {
			if strings.TrimSpace(pin.Authority) == "" {
				return inventory, fmt.Errorf("%s %s: missing authority", kind, name)
			}
			if len(pin.SQLHash) != len(pin.APIHash) || len(pin.SQLHash) == 0 || (kind != "engine protocol" && len(pin.SQLHash) != 2) {
				return inventory, fmt.Errorf("%s %s: incomplete engine contracts", kind, name)
			}
			for engine, hash := range pin.SQLHash {
				if (engine != "sqlite" && engine != "postgres") || strings.TrimSpace(hash) == "" || strings.TrimSpace(pin.APIHash[engine]) == "" {
					return inventory, fmt.Errorf("%s %s: invalid contract for engine %q", kind, name, engine)
				}
			}
			if kind == "engine protocol" && len(pin.SQLHash) != 1 {
				return inventory, fmt.Errorf("engine protocol %s: requires one engine", name)
			}
		}
	}
	if _, err := inventory.rawProtocols(); err != nil {
		return inventory, err
	}
	return inventory, nil
}

func queryContractPins(inventory reviewedPinInventory, err error) (map[string]approvedContractDifference, map[string]map[string]engineQueryPin) {
	if err != nil {
		return nil, nil
	}
	differences := map[string]approvedContractDifference{}
	engines := map[string]map[string]engineQueryPin{}
	for name, pin := range inventory.ContractDifferences {
		differences[name] = approvedContractDifference{SQLiteSQLHash: pin.SQLHash["sqlite"], PostgresSQLHash: pin.SQLHash["postgres"], SQLiteAPIHash: pin.APIHash["sqlite"], PostgresAPIHash: pin.APIHash["postgres"], Reason: pin.Authority}
	}
	for name, pin := range inventory.EngineQueries {
		for engine, hash := range pin.SQLHash {
			if engines[engine] == nil {
				engines[engine] = map[string]engineQueryPin{}
			}
			engines[engine][name] = engineQueryPin{SQLHash: hash, APIHash: pin.APIHash[engine], Reason: pin.Authority}
		}
	}
	return differences, engines
}

// Expand helper names for the existing reachability checker. A shared hash
// grants no raw execution and cannot override a common pin in a build context.
func (inventory reviewedPinInventory) rawProtocols() (map[string]RawSQLProtocol, error) {
	protocols := map[string]RawSQLProtocol{}
	used := map[string]bool{}
	for owner, pin := range inventory.Protocols {
		protocol := RawSQLProtocol{Hash: pin.Hash, Reason: pin.Reason, Callers: pin.Callers}
		expand := func(names []string, context string) (map[string]string, error) {
			result := map[string]string{}
			for _, name := range names {
				hash := inventory.Helpers[name]
				if override := inventory.BuildHelpers[context][name]; override != "" {
					hash = override
				}
				if strings.TrimSpace(hash) == "" {
					return nil, fmt.Errorf("rawsql: %s has unknown helper %s in context %s", owner, name, context)
				}
				if _, duplicate := result[name]; duplicate {
					return nil, fmt.Errorf("rawsql: %s repeats helper %s", owner, name)
				}
				result[name] = hash
				used[name] = true
			}
			return result, nil
		}
		var err error
		if protocol.Dependencies, err = expand(pin.Dependencies, ""); err != nil {
			return nil, err
		}
		protocol.BuildDependencies = map[string]map[string]string{}
		for context, names := range pin.BuildDependencies {
			if protocol.BuildDependencies[context], err = expand(names, context); err != nil {
				return nil, err
			}
		}
		protocols[owner] = protocol
	}
	for name, hash := range inventory.Helpers {
		if !used[name] || strings.TrimSpace(hash) == "" {
			return nil, fmt.Errorf("rawsql: unused or empty shared helper %s", name)
		}
	}
	for context, helpers := range inventory.BuildHelpers {
		known := false
		for _, supported := range Contexts {
			known = known || supported.Name == context
		}
		if !known {
			return nil, fmt.Errorf("rawsql: unknown helper build context %s", context)
		}
		for name, hash := range helpers {
			usedInContext := false
			for _, pin := range inventory.Protocols {
				for _, dependency := range pin.BuildDependencies[context] {
					usedInContext = usedInContext || dependency == name
				}
			}
			if !usedInContext || inventory.Helpers[name] == "" || strings.TrimSpace(hash) == "" {
				return nil, fmt.Errorf("rawsql: unused or empty build helper %s in %s", name, context)
			}
		}
	}
	return protocols, nil
}
