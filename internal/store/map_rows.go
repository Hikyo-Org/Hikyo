package store

// mapRows preserves nil results for empty queries and discards partial results
// if any stored row is invalid.
func mapRows[Row, Value any](rows []Row, convert func(Row) (Value, error)) ([]Value, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]Value, 0, len(rows))
	for _, row := range rows {
		value, err := convert(row)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
