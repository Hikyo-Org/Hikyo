package owner

func SetQueryObserver()           {}
func SetMutationFailureObserver() {}
func SetSCIMPhaseObserver()       {}

func unsafeOwnerCall() { SetQueryObserver() }
