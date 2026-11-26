package releasetrust

// DenyBridgeSigner adds a recovery key digest to the set that may not sign routable
// bridges, for tests that cannot hold the retired key, and returns the undo.
func DenyBridgeSigner(sha256 string) func() {
	previous := bridgeDeniedSigners
	bridgeDeniedSigners = append(append([]string{}, previous...), sha256)
	return func() { bridgeDeniedSigners = previous }
}

// BridgeDeniedSigners returns the denied recovery key digests.
func BridgeDeniedSigners() []string { return bridgeDeniedSigners }
