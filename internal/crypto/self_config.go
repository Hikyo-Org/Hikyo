package crypto

// SelfConfigAdoptionToken binds the server's one-time seed to its owning
// instance. The dedicated derivation label keeps preview and HA comparison
// tokens separate from delivery, scanning and authentication artifacts.
func (k *Keyring) SelfConfigAdoptionToken(owner string, encoded []byte) (string, error) {
	key, err := k.deriveScopedTokenKey("hikyo/self-config-adoption/v1", owner, "", "")
	if err != nil {
		return "", err
	}
	defer Zero(key)
	return tag(key, encoded), nil
}
