//go:build !linux

package app

import (
	"bytes"
	"errors"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// Non-Linux development acceptance can reuse the existing explicitly supplied
// root file. It never creates a plaintext credential copy or falls back to env.
func unattendedRootCredential(cfg *config.Config, root []byte) (*os.File, string, error) {
	if cfg.RootKeyFile == "" {
		return nil, "", errors.New("unattended child credentials require Linux or an existing configured root key file")
	}
	existing, err := crypto.ReadRootKey(cfg.RootKeyFile, "")
	defer crypto.Zero(existing)
	if err != nil || !bytes.Equal(existing, root) {
		return nil, "", errors.New("configured child root key differs from coordinator custody")
	}
	return nil, cfg.RootKeyFile, nil
}
