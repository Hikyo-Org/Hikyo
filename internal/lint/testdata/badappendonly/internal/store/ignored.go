//go:build ignore

package store

// Excluded by its build tag in every context, so no vet unit contains it.
const downgrade = "SET synchronous_commit TO off"
