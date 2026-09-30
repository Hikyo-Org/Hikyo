package badobserver

import hooks "github.com/Hikyo-Org/hikyo/internal/lint/testdata/badobserver/owner"

func aliasCall() { hooks.SetQueryObserver() }

var captured = hooks.SetQueryObserver
var mutation = hooks.SetMutationFailureObserver
var phase = hooks.SetSCIMPhaseObserver
