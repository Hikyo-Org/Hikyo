package store

// LiveRowsLimit bounds how many blocking row ids an environment-delete refusal
// names.
const LiveRowsLimit = 10

// LiveRowsError is an environment-delete refusal that names the rows still
// live. Ids are not secret.
type LiveRowsError struct {
	err error
	IDs []string
	// Truncated reports that more rows block than IDs names.
	Truncated bool
}

func newLiveRowsError(err error, ids []string) *LiveRowsError {
	return &LiveRowsError{err: err, IDs: ids[:min(len(ids), LiveRowsLimit)], Truncated: len(ids) > LiveRowsLimit}
}

func (e *LiveRowsError) Error() string { return e.err.Error() }
func (e *LiveRowsError) Unwrap() error { return e.err }
