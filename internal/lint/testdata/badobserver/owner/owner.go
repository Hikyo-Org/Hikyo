package owner

func SetQueryObserver()           {}
func SetMutationFailureObserver() {}
func SetSCIMPhaseObserver()       {}

func unsafeOwnerCall() { SetQueryObserver() } // want "production reference to .*internal/lint/testdata/badobserver/owner\\.SetQueryObserver"
