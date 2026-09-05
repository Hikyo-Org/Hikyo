package buildcompat

import (
	"bytes"
	_ "embed"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

const DevelopmentVersion = "0.0.0+local.dev"
const DevelopmentCommit = "0000000000000000000000000000000000000000"

//go:embed development.json
var development []byte

// Development returns source-owned build claims for the separately isolated
// local-development datastore domain. The zero commit is an explicit synthetic
// identity, never a statement that this is a Git release. It grants no trust.
func Development() ([]byte, upgradecompat.Declaration, error) {
	declaration, err := upgradecompat.Parse(development)
	if err != nil {
		return nil, upgradecompat.Declaration{}, err
	}
	if declaration.Profile != releaseidentity.StableV1 || declaration.Version != DevelopmentVersion || declaration.Sequence != 1 || declaration.Commit != DevelopmentCommit {
		return nil, upgradecompat.Declaration{}, errors.New("invalid source-owned development identity")
	}
	for _, engine := range declaration.Engines {
		for _, source := range engine.Sources {
			if source.Source.Genesis != releaseidentity.FreshGenesisV1 {
				return nil, upgradecompat.Declaration{}, errors.New("development declaration cannot adopt a populated genesis")
			}
		}
	}
	return bytes.Clone(development), declaration, nil
}

// VerifyDevelopment requires a real verified envelope from the independently
// isolated development custody. The caller must enforce that trust domain;
// production always calls Verify and never selects this method via evidence.
func VerifyDevelopment(node upgradecompat.VerifiedNode) error {
	raw, _, err := Development()
	if err != nil {
		return err
	}
	if !node.Valid() || node.Identity().CompatibilitySHA256 != releaseidentity.Hash(raw) {
		return errors.New("verified development target differs from source build")
	}
	return nil
}
