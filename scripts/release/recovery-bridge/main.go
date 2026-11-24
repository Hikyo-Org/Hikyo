// recovery-bridge derives the unsigned recovery-bridge statements for one stable
// release pair from authenticated public release evidence. It never signs and
// never reads a private key: signing is a separate, offline ceremony step.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/recoverybridge"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "recovery-bridge:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "author" {
		return errors.New("usage: recovery-bridge author --root FILE --recovery-key FILE --snapshot DIR --keys DIR --source DIR --target DIR --out DIR")
	}
	var root, recovery, snapshot, keys, source, target, output string
	fs := flag.NewFlagSet("recovery-bridge author", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&root, "root", "", "independently pinned release root.json")
	fs.StringVar(&recovery, "recovery-key", "", "independently pinned recovery public key")
	fs.StringVar(&snapshot, "snapshot", "", "directory with the current metadata, catalog, policy and their signatures")
	fs.StringVar(&keys, "keys", "", "directory containing only metadata-named public primary keys")
	fs.StringVar(&source, "source", "", "directory with the source stable release proofs")
	fs.StringVar(&target, "target", "", "directory with the target stable release proofs")
	fs.StringVar(&output, "out", "", "new directory; one <digest>/statement.json per engine")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || root == "" || recovery == "" || snapshot == "" || keys == "" || source == "" || target == "" || output == "" {
		return errors.New("require --root, --recovery-key, --snapshot, --keys, --source, --target and --out")
	}
	rootRaw, err := upgradeassembly.ReadDocument(root)
	if err != nil {
		return err
	}
	recoveryRaw, err := upgradeassembly.ReadDocument(recovery)
	if err != nil {
		return err
	}
	_, releases, declarations, err := upgradeassembly.LoadEvidence(ctx, releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recoveryRaw}, snapshot, keys, source, target)
	if err != nil {
		return err
	}
	statements, err := recoverybridge.Derive(
		recoverybridge.Evidence{Release: releases[0], Declaration: declarations[0]},
		recoverybridge.Evidence{Release: releases[1], Declaration: declarations[1]})
	if err != nil {
		return err
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return fmt.Errorf("create output (must not exist): %w", err)
	}
	for _, s := range statements {
		directory := filepath.Join(output, string(s.Digest))
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, "statement.json"), s.Bytes, 0o600); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s %s\n", s.Engine, s.Digest); err != nil {
			return err
		}
	}
	return nil
}
