package store

// optionalStoredBytes keeps the existing NULL representation for absent material.
func optionalStoredBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return value
}
