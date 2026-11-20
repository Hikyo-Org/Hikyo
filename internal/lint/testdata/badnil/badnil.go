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

// A package handling Proof values while importing reflect is itself a
// finding, independent of the nil literals.
var _ = reflect.TypeOf(returnsNil())
