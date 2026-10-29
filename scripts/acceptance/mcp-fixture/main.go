//go:build darwin || linux

// mcp-fixture prepares and probes the single disposable #651 fixture.
// It never deploys, publishes, changes DNS, or calls a model provider.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type settings struct {
	DSN         string `json:"dsn"`
	Root        string `json:"root_key_file"`
	State       string `json:"state_directory"`
	Development bool   `json:"development"`
	Bundle      string `json:"bundle_directory"`
	Operator    string `json:"operator_public_key_file"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		// Do not propagate network, SQL, subprocess or service error strings:
		// they can contain credentials or tool material.
		fmt.Fprintln(os.Stderr, "MCP fixture failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("use local, seed, revoke, grant-remove, grant-restore, or probe; see README.md")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := fs.String("runtime", "", "existing owner-only runtime directory")
	binary := fs.String("server", "", "local hikyo executable, local mode only")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if err := privatePath(*dir, true); err != nil {
		return err
	}
	if args[0] == "gateway" {
		return serveGateway(ctx, *dir)
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	if args[0] == "local" {
		return local(ctx, *dir, *binary)
	}
	if args[0] == "render" {
		return render(*dir)
	}
	if args[0] == "authorize" {
		return authorizeDeployment(*dir)
	}
	if args[0] == "reserve" {
		return createReservation(ctx, *dir)
	}
	if args[0] == "authorize-resume" {
		return authorizeContinuation(ctx, *dir)
	}
	if args[0] == "audit-seed" {
		return auditSeedConfigured(ctx, *dir)
	}
	if args[0] == "public" {
		return publicCampaign(ctx, *dir)
	}
	if args[0] == "wire-snapshot" {
		return readWireSnapshot(ctx, *dir)
	}
	if args[0] == "listeners" {
		return listeners(ctx, *dir)
	}
	var cfg settings
	if err := readPrivate(filepath.Join(*dir, "settings.json"), &cfg); err != nil {
		return err
	}
	if args[0] == "probe" {
		return probeConfigured(ctx, *dir)
	}
	if args[0] == "observe" {
		return observeReadOnly(ctx, cfg)
	}
	opener := openFixture
	if args[0] == "seed" {
		opener = openFreshFixture
	}
	db, kr, err := opener(ctx, cfg)
	if err != nil {
		return errors.New("datastore admission refused; protected custody retained")
	}
	defer db.Close()
	switch args[0] {
	case "seed":
		return seed(ctx, *dir, db, kr)
	case "revoke", "revoke-smoke", "grant-remove", "grant-restore":
		if err := mutate(ctx, *dir, db, kr, args[0]); err != nil {
			return errors.New("supported fixture mutation refused")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"action": args[0], "committed_at_utc": time.Now().UTC()})
	case "hold":
		return holdFixtureCatalogue(ctx, cfg)
	default:
		return errors.New("unknown command")
	}
}

func privatePath(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return privatePathModes(path, directory, mode)
}

func privatePathModes(path string, directory bool, modes ...os.FileMode) error {
	info, err := os.Lstat(path)
	permitted := false
	if err == nil {
		for _, mode := range modes {
			if info.Mode().Perm() == mode {
				permitted = true
			}
		}
	}
	if err != nil || !filepath.IsAbs(path) || !permitted || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("custody requires canonical owner-only directories and regular files with the declared mode")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("custody must belong to the current user")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return errors.New("symlinked custody is refused")
	}
	return nil
}

func readPrivate(path string, value any) error {
	if err := privatePath(path, false); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("protected input unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("protected input must be a bounded regular file")
	}
	d := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("invalid protected input")
	}
	var extra json.RawMessage
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing protected input refused")
	}
	return nil
}

func writePrivate(path string, value any) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("refusing to overwrite protected output")
	}
	if err := json.NewEncoder(f).Encode(value); err != nil {
		f.Close()
		return errors.New("protected output failed")
	}
	err = f.Sync()
	closeErr := f.Close()
	return errors.Join(err, closeErr)
}
