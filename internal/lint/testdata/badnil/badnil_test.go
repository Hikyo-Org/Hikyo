package badnil

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
