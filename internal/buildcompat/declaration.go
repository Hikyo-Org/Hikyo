// Package buildcompat exposes the source-owned declaration embedded at build
// time. Embedded claims are never release signature authority.
package buildcompat

import (
	"encoding/base64"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// Set only by the release linker. No environment or runtime setter exists.
var encodedDeclaration string
var declarationSHA256 string

func Current() ([]byte, upgradecompat.Declaration, error) {
	if encodedDeclaration == "" || len(encodedDeclaration) > 6<<20 {
		return nil, upgradecompat.Declaration{}, errors.New("binary has no bounded release compatibility declaration")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encodedDeclaration)
	if err != nil {
		return nil, upgradecompat.Declaration{}, err
	}
	if releaseidentity.Digest(declarationSHA256).Validate() != nil || string(releaseidentity.Hash(raw)) != declarationSHA256 {
		return nil, upgradecompat.Declaration{}, errors.New("embedded compatibility digest mismatch")
	}
	declaration, err := upgradecompat.Parse(raw)
	if err != nil {
		return nil, upgradecompat.Declaration{}, err
	}
	if declaration.Version == DevelopmentVersion {
		return nil, upgradecompat.Declaration{}, errors.New("development declaration is not a production build binding")
	}
	return raw, declaration, nil
}

// Verify binds an independently authenticated target to this binary's exact
// source declaration. The platform payload remains separately authenticated.
func Verify(node upgradecompat.VerifiedNode) error {
	raw, declaration, err := Current()
	if err != nil {
		return err
	}
	if !node.Valid() {
		return errors.New("missing authenticated target")
	}
	id := node.Identity()
	if id.CompatibilitySHA256 != releaseidentity.Hash(raw) || id.Profile != declaration.Profile || id.Version != declaration.Version || id.Sequence != declaration.Sequence || id.Commit != declaration.Commit {
		return errors.New("authenticated target differs from running binary declaration")
	}
	return nil
}
