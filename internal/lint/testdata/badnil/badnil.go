// Package badnil is a negative fixture for the proof-forgery guard: every
// construct below is a nil-in-Proof-position forgery the analyzer must flag.
package badnil

import (
	"reflect" // want "imports \"reflect\" while handling authz\\.Proof values"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

func returnsNil() authz.Proof { return nil } // want "nil in an authz\\.Proof position"

var globalNil authz.Proof = nil // want "nil in an authz\\.Proof position"

func passesNil() {
	take(nil) // want "nil in an authz\\.Proof position"
}

func take(p authz.Proof) { _ = p }

func mint() (authz.Proof, error) { return nil, nil } // want "nil in an authz\\.Proof position"

// A package-level Proof variable with no initialiser is nil for good.
var unsetProof authz.Proof // want "nil in an authz\\.Proof position"

var convertedNil = authz.Proof(nil) // want "nil in an authz\\.Proof position"

var allocated = new(authz.Proof) // want "nil in an authz\\.Proof position"

// A local declare-then-assign variable is the production pattern and is not
// flagged.
func localAssigned() {
	var proof authz.Proof
	proof, _ = mint()
	take(proof)
}

// A package handling Proof values while importing reflect is itself a
// finding, independent of the nil literals.
var _ = reflect.TypeOf(returnsNil())
