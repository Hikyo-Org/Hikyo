package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// RunUpgradeOperator is local-only administration. It must dispatch before server
// initialization, admission, listeners and ordinary root-key configuration.
func RunUpgradeOperator(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "rotate" {
		return errors.New("usage: hikyo upgrade operator rotate --statement FILE --signature FILE --new-public-key FILE")
	}
	fs := flag.NewFlagSet("upgrade operator rotate", flag.ContinueOnError)
	fs.SetOutput(out)
	statement := fs.String("statement", "", "signed operator transition JSON")
	signature := fs.String("signature", "", "Sigstore signature bundle")
	public := fs.String("new-public-key", "", "next public operator key")
	local := fs.Bool("local-break-glass", false, "explicit local root-escrow recovery")
	rootPath := fs.String("root-key-file", "", "separately held root-key escrow file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || *statement == "" || *signature == "" || *public == "" || (*local != (*rootPath != "")) {
		return errors.New("rotation requires statement, signature, new public key; break-glass requires both --local-break-glass and --root-key-file")
	}
	req := upgradegate.OperatorRotationRequest{Store: upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}, StateDirectory: cfg.Upgrade.StateDirectory}
	for _, item := range []struct {
		path string
		dst  *[]byte
	}{{*statement, &req.Statement}, {*signature, &req.Signature}, {*public, &req.NewPublicKey}} {
		raw, err := backupreceipt.ReadPublicArtifact(item.path, 1<<20)
		if err != nil {
			return err
		}
		*item.dst = raw
	}
	parsed, err := backupreceipt.ParseRotation(req.Statement)
	if err != nil {
		return err
	}
	if (parsed.Mode == backupreceipt.LocalBreakGlass) != *local {
		return errors.New("rotation mode differs from explicit local command")
	}
	if *local {
		req.LocalRecoveryRoot, err = crypto.ReadRootKey(*rootPath, "")
		if err != nil {
			return err
		}
		defer crypto.Zero(req.LocalRecoveryRoot)
	}
	state, err := upgradegate.RotateOperator(ctx, req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Operator rotated; epoch %d, generation %d. Update the configured operator public key. Maintenance retained; prepare fresh evidence before recovery.\n", state.RestoreEpoch, state.Generation)
	return err
}
