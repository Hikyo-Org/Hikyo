package badappendonly // want "appendonly: sync.go issues SET synchronous_commit"

// A Go file of the analyzed subtree: the per-package scan finds the statement.
const downgrade = "SET synchronous_commit = off"
