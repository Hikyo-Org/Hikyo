package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func runAdminConfig(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: hikyo admin config status | recover --revision NUMBER")
	}
	fs := flag.NewFlagSet("admin config "+args[0], flag.ContinueOnError)
	fs.SetOutput(out)
	var revision int64
	if args[0] == "recover" {
		fs.Int64Var(&revision, "revision", 0, "exact published revision to recover after stopping every replica")
	} else if args[0] != "status" {
		return errors.New("usage: hikyo admin config status | recover --revision NUMBER")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	auth, closeDB, err := adminAuth(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer closeDB()
	if args[0] == "status" {
		status, err := auth.SelfConfig.LocalStatus(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(status)
	}
	if revision < 1 {
		return errors.New("--revision must name a published revision; stop every replica and wait 30 seconds before recovery")
	}
	status, err := auth.SelfConfig.Recover(ctx, revision)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(status)
}
