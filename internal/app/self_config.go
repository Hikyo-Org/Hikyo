package app

import (
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func newSelfConfig(cfg *config.Config, db *store.DB, kr *crypto.Keyring, auth *service.Auth) *service.SelfConfig {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	return &service.SelfConfig{DB: db, Keyring: kr, Auth: auth, NodeID: nodeID, Seed: cfg.ManagedSeed}
}
