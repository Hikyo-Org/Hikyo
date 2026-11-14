// Package badrecovery is the negative fixture for the recovery-lookup
// analyzer: ResetAllowed is the one licensed caller, and the rest reach the
// protected name by routes a name-only check must still catch.
package badrecovery

type lookup interface{ RecoveryAccountByID(id string) string }

// ResetAllowed is licensed.
func ResetAllowed(l lookup) string { return l.RecoveryAccountByID("a") }

// SignIn is the violation: a sign-in path using the ungated lookup.
func SignIn(l lookup) string { return l.RecoveryAccountByID("a") }

type localLookup interface{ RecoveryAccountByID(id string) string }

// ViaAssertion renames the interface through a type assertion.
func ViaAssertion(l any) string { return l.(localLookup).RecoveryAccountByID("a") }

// ViaMethodValue takes the method as a value and calls it later.
func ViaMethodValue(l lookup) func(string) string { return l.RecoveryAccountByID }

type licensedType struct{}

// Reset is licensed through its receiver type.
func (licensedType) Reset(l lookup) string { return l.RecoveryAccountByID("a") }

type otherType struct{}

// Reset shares the licensed method's name but not its receiver type.
func (*otherType) Reset(l lookup) string { return l.RecoveryAccountByID("a") }
