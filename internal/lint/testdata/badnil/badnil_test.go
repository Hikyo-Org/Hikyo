package badnil

import "github.com/Hikyo-Org/hikyo/internal/authz"

// A nil proof that is the subject of the test carries a reasoned marker.
func markedNil() {
	take(nil) // forgeguard:nil-proof the boundary under test must refuse a nil proof
}

func unmarkedNil() {
	take(nil) // want "nil in an authz\\.Proof position"
}

func markerWithoutReason() {
	take(nil) /* forgeguard:nil-proof */ // want "marker needs a reason" "nil in an authz\\.Proof position"
}

// The marker never exempts the package-level uninitialised variable: one
// marker would cover every use of it.
var markedUnset authz.Proof /* forgeguard:nil-proof one marker must not cover every use */ // want "nil in an authz\\.Proof position"

// A longer word is not the marker.
func markerPrefix() {
	take(nil) /* forgeguard:nil-proofx not the marker */ // want "nil in an authz\\.Proof position"
}
