// assemble-upgrade authenticates public release evidence before publishing the
// exact offline layout consumed by the mandatory runtime gate. It never signs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type directories []string

func (d *directories) String() string { return strings.Join(*d, ",") }
func (d *directories) Set(value string) error {
	if value == "" {
		return errors.New("empty evidence directory")
	}
	*d = append(*d, value)
	return nil
}

type options struct {
	root, recovery, snapshot, keys, output, floor string
	releases, nightlies, bridges                  directories
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "assemble-upgrade:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var o options
	fs := flag.NewFlagSet("assemble-upgrade", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.root, "root", "", "independently pinned release root.json")
	fs.StringVar(&o.recovery, "recovery-key", "", "independently pinned recovery public key")
	fs.StringVar(&o.snapshot, "snapshot", "", "directory containing metadata/catalog and their signatures")
	fs.StringVar(&o.keys, "keys", "", "directory containing only metadata-named public primary keys")
	fs.StringVar(&o.output, "out", "", "new output directory; never overwrite")
	fs.StringVar(&o.floor, "floor", "", "optional existing SnapshotFloor JSON; never updated by assembly")
	fs.Var(&o.releases, "release", "directory containing four stable public release proofs; repeat for source and route hops")
	fs.Var(&o.nightlies, "nightly", "complete flat signed nightly download; repeat for source and route hops")
	fs.Var(&o.bridges, "bridge", "directory containing statement.json and statement.sigstore.json; repeat for every authorized bridge")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || o.root == "" || o.recovery == "" || o.snapshot == "" || o.keys == "" || o.output == "" || len(o.releases)+len(o.nightlies) == 0 || len(o.releases)+len(o.nightlies) > upgradecompat.MaxReleases || len(o.bridges) > upgradecompat.MaxEdges {
		return errors.New("require --root, --recovery-key, --snapshot, --keys, --release and --out with bounded inventories")
	}
	if err := assemble(ctx, o); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Authenticated public bundle published. Runtime revalidates installation trust and source state before admission.")
	return err
}

func assemble(ctx context.Context, o options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := upgradeassembly.ReadDocument(o.root)
	if err != nil {
		return err
	}
	recovery, err := upgradeassembly.ReadDocument(o.recovery)
	if err != nil {
		return err
	}
	floor := releaseidentity.SnapshotFloor{}
	if o.floor != "" {
		raw, err := upgradeassembly.ReadDocument(o.floor)
		if err != nil {
			return err
		}
		if err := definitions.DecodeStrict(raw, &floor); err != nil {
			return err
		}
	}
	return upgradeassembly.Assemble(ctx, upgradeassembly.Options{
		Pinned: releasetrust.PinnedTrust{Root: root, RecoveryPublicKey: recovery},
		Floor:  floor, SnapshotDirectory: o.snapshot, KeysDirectory: o.keys,
		OutputDirectory: o.output, Releases: o.releases, Nightlies: o.nightlies, Bridges: o.bridges,
	})
}
