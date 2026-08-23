package adapter

import (
	"fmt"
	"slices"
	"strings"
)

type LedgerKey struct {
	surface Surface
	name    string
}

func NewLedgerKey(surface Surface, name string) LedgerKey {
	return LedgerKey{surface: surface, name: strings.ToUpper(name)}
}

func IndexLedger(rows []LedgerEntry) (map[LedgerKey]LedgerEntry, error) {
	out := make(map[LedgerKey]LedgerEntry, len(rows))
	for _, row := range rows {
		if err := validateMissingState(row.State, row.Missing); err != nil {
			return nil, fmt.Errorf("adapter: ledger entry %s/%s: %w", row.Surface, row.EffectiveName, err)
		}
		out[NewLedgerKey(row.Surface, row.EffectiveName)] = row
	}
	return out, nil
}

func ValidateCompletion(completion Completion) error {
	return validateMissingState(completion.State, completion.Missing)
}

func validateMissingState(state LedgerState, missing bool) error {
	if missing && state != Owned && state != Dispatched {
		return fmt.Errorf("missing requires owned or dispatched state, got %q", state)
	}
	return nil
}

type DesiredRow struct {
	ManifestEntry
	Surface       Surface
	EffectiveName string
}

func DesiredRows(prefix string, manifest []ManifestEntry, sentinels bool) []DesiredRow {
	rows := make([]DesiredRow, 0, len(manifest)+2)
	if sentinels {
		rows = append(rows,
			DesiredRow{ManifestEntry: ManifestEntry{Classification: SecretClassification, Value: SentinelName}, Surface: Secret, EffectiveName: prefix + SentinelName},
			DesiredRow{ManifestEntry: ManifestEntry{Classification: ConfigClassification, Value: SentinelName}, Surface: Variable, EffectiveName: prefix + SentinelName},
		)
	}
	for _, entry := range manifest {
		rows = append(rows, DesiredRow{ManifestEntry: entry, Surface: entry.Surface(), EffectiveName: prefix + entry.CanonicalName})
	}
	slices.SortStableFunc(rows, func(a, b DesiredRow) int {
		aSentinel, bSentinel := a.KeyID == "", b.KeyID == ""
		if aSentinel != bSentinel {
			if aSentinel {
				return -1
			}
			return 1
		}
		if a.Surface != b.Surface {
			return strings.Compare(string(a.Surface), string(b.Surface))
		}
		return strings.Compare(a.EffectiveName, b.EffectiveName)
	})
	return rows
}

func NameSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToUpper(value)] = true
	}
	return out
}

func CompletedNames(changes []Change) map[LedgerKey]bool {
	out := make(map[LedgerKey]bool, len(changes))
	for _, change := range changes {
		out[NewLedgerKey(change.Surface, change.EffectiveName)] = true
	}
	return out
}

func PlanChanges(desired []DesiredRow, ledger map[LedgerKey]LedgerEntry, providerSecrets map[string]bool) []Change {
	changes := make([]Change, 0, len(desired)+len(ledger))
	desiredSet := make(map[LedgerKey]bool, len(desired))
	for _, row := range desired {
		key := NewLedgerKey(row.Surface, row.EffectiveName)
		desiredSet[key] = true
		record, claimed := ledger[key]
		disposition := Create
		switch {
		case claimed && (record.State == Owned || record.State == Dispatched) && !record.Missing:
			disposition = Update
		case row.Surface == Secret && providerSecrets[row.EffectiveName]:
			disposition = Conflict
		case row.Surface == Variable && !claimed:
			disposition = Unknown
		}
		changes = append(changes, Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: disposition})
	}
	for key, record := range ledger {
		if desiredSet[key] || record.State == Reserved {
			continue
		}
		changes = append(changes, Change{Surface: record.Surface, EffectiveName: record.EffectiveName, Disposition: Delete})
	}
	SortChanges(changes)
	return changes
}

func Undesired(desired []DesiredRow, ledger map[LedgerKey]LedgerEntry) (reservations, prunes []LedgerEntry) {
	desiredSet := make(map[LedgerKey]bool, len(desired))
	for _, row := range desired {
		desiredSet[NewLedgerKey(row.Surface, row.EffectiveName)] = true
	}
	for key, row := range ledger {
		if desiredSet[key] {
			continue
		}
		if row.State == Reserved {
			reservations = append(reservations, row)
		} else {
			prunes = append(prunes, row)
		}
	}
	sortLedger(reservations, false)
	sortLedger(prunes, true)
	return reservations, prunes
}

func SortChanges(rows []Change) {
	slices.SortFunc(rows, func(a, b Change) int {
		if a.Surface != b.Surface {
			return strings.Compare(string(a.Surface), string(b.Surface))
		}
		return strings.Compare(a.EffectiveName, b.EffectiveName)
	})
}

func sortLedger(rows []LedgerEntry, sentinelsLast bool) {
	slices.SortFunc(rows, func(a, b LedgerEntry) int {
		if sentinelsLast {
			aSentinel := strings.HasSuffix(a.EffectiveName, SentinelName)
			bSentinel := strings.HasSuffix(b.EffectiveName, SentinelName)
			if aSentinel != bSentinel {
				if aSentinel {
					return 1
				}
				return -1
			}
		}
		if a.Surface != b.Surface {
			return strings.Compare(string(a.Surface), string(b.Surface))
		}
		return strings.Compare(a.EffectiveName, b.EffectiveName)
	})
}
