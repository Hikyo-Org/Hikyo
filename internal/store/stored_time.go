package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgStoredStamp(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	if v.InfinityModifier != pgtype.Finite {
		return v.InfinityModifier.String()
	}
	return CanonTime(v.Time).Format(timeFormat)
}

func readStoredTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(timeFormat, value)
	if err != nil {
		return nil, fmt.Errorf("store: malformed adapter timestamp %q: %w", value, err)
	}
	parsed = CanonTime(parsed)
	return &parsed, nil
}

func normalizeStoredTimes(values ...*string) error {
	for _, value := range values {
		if *value == "" {
			continue
		}
		parsed, err := time.Parse(timeFormat, *value)
		if err != nil {
			return fmt.Errorf("store: malformed adapter timestamp %q: %w", *value, err)
		}
		*value = CanonTime(parsed).Format(timeFormat)
	}
	return nil
}

// runtimeSQLiteStamp preserves fixed-width ordering for runtime deadlines.
func runtimeSQLiteStamp(t time.Time) sql.NullString {
	return sql.NullString{String: fixedStamp(t), Valid: true}
}
