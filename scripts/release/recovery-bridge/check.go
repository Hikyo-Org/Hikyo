package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
)

// directoryFetcher serves a published list from a local directory, or reports
// the endpoint as unreachable, so the ceremony rehearsal runs the real Checker
// without a network.
type directoryFetcher struct {
	directory   string
	unreachable bool
}

func (f directoryFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	if f.unreachable {
		return nil, nil, bridgerevocation.ErrUnreachable
	}
	list, err := os.ReadFile(filepath.Join(f.directory, "recovery-revocations.json"))
	if err != nil {
		return nil, nil, err
	}
	signature, err := os.ReadFile(filepath.Join(f.directory, "recovery-revocations.sigstore.json"))
	return list, signature, err
}

// checkRevocations runs the apply-time check against a snapshot and prints the
// outcome and which of the given bridge digests are revoked.
func checkRevocations(ctx context.Context, args []string, out io.Writer) error {
	var root, recovery, snapshot, keys, state, list string
	var unreachable bool
	fs := flag.NewFlagSet("recovery-bridge check-revocations", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&root, "root", "", "independently pinned release root.json")
	fs.StringVar(&recovery, "recovery-key", "", "independently pinned recovery public key")
	fs.StringVar(&snapshot, "snapshot", "", "directory with the metadata, catalog, policy and their signatures")
	fs.StringVar(&keys, "keys", "", "directory containing only metadata-named public primary keys")
	fs.StringVar(&state, "state", "", "directory holding the remembered list and floor")
	fs.StringVar(&list, "list", "", "directory with the published recovery-revocations files")
	fs.BoolVar(&unreachable, "unreachable", false, "behave as if the endpoint cannot be reached")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if root == "" || recovery == "" || snapshot == "" || keys == "" || state == "" || (list == "") != unreachable {
		return errors.New("require --root, --recovery-key, --snapshot, --keys, --state and exactly one of --list or --unreachable")
	}
	rootRaw, err := upgradeassembly.ReadDocument(root)
	if err != nil {
		return err
	}
	recoveryRaw, err := upgradeassembly.ReadDocument(recovery)
	if err != nil {
		return err
	}
	verified, _, _, err := upgradeassembly.LoadEvidence(ctx, releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recoveryRaw}, snapshot, keys)
	if err != nil {
		return err
	}
	result, err := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: directoryFetcher{directory: list, unreachable: unreachable}, Directory: state}.Check(ctx, verified)
	if err != nil {
		return err
	}
	revoked := []releaseidentity.Digest{}
	for _, digest := range fs.Args() {
		if bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{releaseidentity.Digest(digest)}) {
			revoked = append(revoked, releaseidentity.Digest(digest))
		}
	}
	_, err = fmt.Fprintf(out, "outcome=%s sequence=%d revoked=%v\n", result.Outcome, result.Sequence, revoked)
	return err
}
