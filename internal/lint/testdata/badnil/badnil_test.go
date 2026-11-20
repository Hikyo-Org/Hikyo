package badnil

// Nothing exempts a nil proof in a test file: only internal/authz may spell it.
func unmarkedNil() {
	take(nil) // want "nil in an authz\\.Proof position"
}
