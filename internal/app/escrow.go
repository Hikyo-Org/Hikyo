package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func RunEscrow(ctx context.Context, cfg *config.Config, _ *slog.Logger, args []string, out io.Writer, _ *disclose.TerminalSession, _ error) error {
	if len(args) == 0 || args[0] != "verify" {
		return errors.New("usage: hikyo escrow verify --root-key-file FILE --assert-separate-custody")
	}
	fs := flag.NewFlagSet("escrow verify", flag.ContinueOnError)
	fs.SetOutput(out)
	file := fs.String("root-key-file", "", "separately recovered escrow root file")
	asserted := fs.Bool("assert-separate-custody", false, "assert that this file was recovered from the separate offline custody store")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || *file == "" || !*asserted {
		return errors.New("escrow verification requires --root-key-file and --assert-separate-custody")
	}
	serverRoot := cfg.RootKeyFile
	if serverRoot == "" && cfg.Dev && !cfg.RootKeyFromEnv {
		serverRoot = devRootKeyPath(cfg)
	}
	if serverRoot == "" {
		return errors.New("escrow verification requires the server root file identity through HIKYO_ROOT_KEY_FILE; environment-only root custody cannot prove distinct files")
	}
	root, err := crypto.ReadEscrowRootKey(*file, serverRoot)
	if err != nil {
		return err
	}
	defer crypto.Zero(root)
	db, err := openBackupRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := (&service.Escrow{DB: db}).Verify(ctx, root, *asserted); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Escrow root verified against the current hierarchy. Separate offline custody is the operator's recorded assertion; filesystem checks cannot prove it.")
	return err
}
