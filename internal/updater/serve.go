package updater

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Run starts the separately privileged updater helper. It reads only its own
// root-owned JSON configuration and never loads Hikyo's database or keyring.
func Run(ctx context.Context, args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("updater", flag.ContinueOnError)
	configPath := fs.String("config", "", "absolute path to updater JSON configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || !filepath.IsAbs(*configPath) {
		return errors.New("updater: --config must be an absolute path")
	}
	if rest := fs.Args(); len(rest) != 0 {
		return fmt.Errorf("updater: unexpected argument %q", rest[0])
	}
	if err := validateConfigCustody(*configPath); err != nil {
		return err
	}
	config, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(config.Socket) || !filepath.IsAbs(config.StateFile) {
		return errors.New("updater: socket and state_file must be absolute paths")
	}
	if err := prepareSocket(config.Socket); err != nil {
		return err
	}
	defer os.Remove(config.Socket)

	journal := &Journal{Path: config.StateFile}
	if err := journal.RecoverInterrupted(time.Now().UTC()); err != nil {
		return fmt.Errorf("updater: recover journal: %w", err)
	}
	listener, err := net.Listen("unix", config.Socket)
	if err != nil {
		return fmt.Errorf("updater: listen: %w", err)
	}
	defer listener.Close()
	if err := os.Chmod(config.Socket, 0o660); err != nil {
		return fmt.Errorf("updater: chmod socket: %w", err)
	}
	control := &ControlServer{
		Executor: Executor{Config: config, Runner: CommandRunner{}},
		Journal:  journal,
		Log:      log,
		Context:  ctx,
	}
	server := &http.Server{Handler: control.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	log.Info("updater helper ready", "backend", config.Backend, "socket", config.Socket)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("updater: shutdown: %w", err)
		}
		control.Wait()
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("updater: serve: %w", err)
	}
}

func prepareSocket(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("updater: create socket directory: %w", err)
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("updater: inspect socket path: %w", err)
	case info.Mode()&os.ModeSocket == 0:
		return fmt.Errorf("updater: refuse to replace non-socket path %q", path)
	default:
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("updater: remove stale socket: %w", err)
		}
		return nil
	}
}
