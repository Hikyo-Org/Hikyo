package badobserver

import hooks "github.com/Hikyo-Org/hikyo/internal/lint/testdata/badobserver/owner"

func aliasCall() { hooks.SetQueryObserver() } // want "production reference to .*internal/lint/testdata/badobserver/owner\\.SetQueryObserver"

var captured = hooks.SetQueryObserver           // want "production reference to .*internal/lint/testdata/badobserver/owner\\.SetQueryObserver"
var mutation = hooks.SetMutationFailureObserver // want "production reference to .*internal/lint/testdata/badobserver/owner\\.SetMutationFailureObserver"
var phase = hooks.SetSCIMPhaseObserver          // want "production reference to .*internal/lint/testdata/badobserver/owner\\.SetSCIMPhaseObserver"
