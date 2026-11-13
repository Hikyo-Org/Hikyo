// Package badsetter is the negative fixture for the containment-setter
// analyzer: Licensed is the one allowed caller, and the rest reach the
// setters by routes a name-only check must still catch.
package badsetter

type setter interface {
	ContainPrincipal(id string) error
	ReleasePrincipalContainment(id string) error
}

// Licensed is allowed.
func Licensed(s setter) error { return s.ContainPrincipal("a") }

// ServiceContain is the violation: service code setting the state.
func ServiceContain(s setter) error { return s.ContainPrincipal("a") }

// ServiceRelease is the violation: service code clearing the state.
func ServiceRelease(s setter) error { return s.ReleasePrincipalContainment("a") }

type localSetter interface{ ContainPrincipal(id string) error }

// ViaAssertion renames the interface through a type assertion.
func ViaAssertion(s any) error { return s.(localSetter).ContainPrincipal("a") }

// ViaMethodValue takes the method as a value and calls it later.
func ViaMethodValue(s setter) func(string) error { return s.ContainPrincipal }
