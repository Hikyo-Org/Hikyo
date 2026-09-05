// hikyo is the multicall binary: `hikyo server` and `hikyo migrate` are real;
// client verbs are stubs until their tickets land.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"golang.org/x/term"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/importer"
	"github.com/Hikyo-Org/hikyo/internal/operator"
	binaryupdate "github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

// Set by GoReleaser. Development builds deliberately identify themselves as
// unversioned instead of guessing from the local checkout.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
	// updateChannel is stamped by release builds. Direct source builds stay off.
	updateChannel = "off"
	// Stable artifacts also stamp the pinned recovery root and the canonical
	// verifier used by the offline signing ceremony. Empty values fail closed.
	updateTrustRoot   = ""
	updateRecoveryKey = ""
)

func main() {
	os.Exit(run())
}

func run() int {
	if handled, code := runRootKeyStageMode(os.Args[1:]); handled {
		return code
	}
	if handled, code := runTLSStageMode(os.Args[1:]); handled {
		return code
	}
	if handled, code := importer.RunInternalSubprocess(os.Args[1:], os.Stdout); handled {
		return code
	}
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	cmd, args := os.Args[1], os.Args[2:]
	// Datastore and custody commands must reach their gate before any optional
	// executable housekeeping. Only remote client verbs own CLI update cleanup.
	if slices.Contains(cli.Verbs, cmd) {
		if err := binaryupdate.CleanupPrevious(); err != nil {
			fmt.Fprintln(os.Stderr, "hikyo: clean up previous update:", err)
		}
	}
	builtChannel, err := builtUpdateChannel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo:", err)
		return 1
	}

	app.Version = version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if shouldCheckForUpdate(cmd) {
		terminalSession, terminalError := disclose.OpenTerminalSession()
		updated := cli.NotifyUpdate(ctx, updateIO(terminalSession, terminalError, builtChannel))
		_ = terminalSession.Close()
		if updated {
			return 0
		}
	}

	switch {
	case cmd == "--version":
		writeMachineVersion(os.Stdout)
		return 0
	case cmd == "version":
		writeVersion(os.Stdout)
		return 0
	case cmd == "about":
		writeAbout(os.Stdout)
		return 0
	case cmd == "welcome":
		writeWelcome(os.Stdout)
		return 0
	case cmd == "server":
		return runServer(ctx, args)
	case cmd == "operator":
		return runOperatorMode(ctx)
	case cmd == "updater":
		return runUpdaterMode(ctx, args)
	case cmd == "migrate":
		return runMigrate(ctx, args)
	case cmd == "upgrade":
		return runUpgradeOperator(ctx, args)
	case cmd == "admin":
		return runAdmin(ctx, args)
	case cmd == "backup":
		return runOperator(ctx, "backup", args, app.RunBackup)
	case cmd == "restore":
		return runOperator(ctx, "restore", args, app.RunRestore)
	case slices.Contains(cli.Verbs, cmd):
		terminalSession, terminalError := disclose.OpenTerminalSession()
		return cli.Run(ctx, updateIO(terminalSession, terminalError, builtChannel), os.Args[1:])
	default:
		fmt.Fprintf(os.Stderr, "hikyo: unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

// writeVersion prints the readable build summary. `hikyo version` shows this
// regardless of TTY; the machine contract is `--version` (writeMachineVersion).
func writeVersion(output io.Writer) {
	fmt.Fprint(output, console.VersionMessage(console.VersionInfo{
		Version: version, Commit: commit, BuildDate: buildDate,
	}))
}

func writeMachineVersion(output io.Writer) {
	fmt.Fprintln(output, version)
}

func writeAbout(output io.Writer) {
	fmt.Fprint(output, console.AboutMessage(console.VersionInfo{
		Version: version, Commit: commit, BuildDate: buildDate,
	}))
}

func writeWelcome(output io.Writer) {
	fmt.Fprint(output, console.WelcomeMessage(console.VersionInfo{
		Version: version, Commit: commit, BuildDate: buildDate,
	}))
}

func builtUpdateChannel() (updatecheck.Channel, error) {
	channel, err := updatecheck.ParseChannel(updateChannel)
	if err != nil {
		return "", fmt.Errorf("invalid built-in update channel: %w", err)
	}
	return channel, nil
}

type unavailableBinaryUpdater struct{ err error }

func (u unavailableBinaryUpdater) Apply(context.Context, updatecheck.Status) error {
	return u.err
}

func binaryUpdater() cli.BinaryUpdater {
	state, err := cli.NewState(cli.Env{Getenv: os.Getenv})
	if err != nil {
		return unavailableBinaryUpdater{err: err}
	}
	installer, err := binaryupdate.NewInstaller(binaryupdate.Config{
		StateDir:          state.Dir(),
		TrustRootBase64:   updateTrustRoot,
		RecoveryKeyBase64: updateRecoveryKey,
	})
	if err != nil {
		return unavailableBinaryUpdater{err: err}
	}
	return installer
}

func updateIO(terminalSession *disclose.TerminalSession, terminalError error, channel updatecheck.Channel) cli.IO {
	return cli.IO{
		Stdin:                os.Stdin,
		Stdout:               os.Stdout,
		Stderr:               os.Stderr,
		Env:                  cli.Env{Getenv: os.Getenv},
		Workdir:              workdir(),
		Version:              version,
		DefaultUpdateChannel: channel,
		TerminalSession:      terminalSession,
		TerminalError:        terminalError,
		OpenURL:              cli.OpenBrowser,
		BinaryUpdater:        binaryUpdater(),
		StderrIsTerminal: func() bool {
			return term.IsTerminal(int(os.Stderr.Fd()))
		},
	}
}

func shouldCheckForUpdate(command string) bool {
	return command != "update" && slices.Contains(cli.Verbs, command)
}

func runServer(ctx context.Context, args []string) int {
	cfg, warnings, err := config.Load("server", args, os.Getenv, os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo server:", err)
		return 1
	}
	log := app.Logger(cfg.Dev)
	for _, w := range warnings {
		log.Warn(w)
	}
	srv, err := app.Boot(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		return 1
	}
	mode := "production"
	if cfg.Dev {
		mode = "development"
	}
	appURL := serverAppURL(cfg, srv)
	message := ""
	if !srv.Maintenance && term.IsTerminal(int(os.Stdout.Fd())) {
		message = console.ServerReadyMessage(console.ServerInfo{
			Version:        version,
			AppURL:         appURL,
			ListenAddress:  srv.Addr,
			OperationalURL: "http://" + srv.OperationalAddr,
			Mode:           mode,
		})
	}
	stopTLSReload := watchTLSReloadSignal(ctx, srv.ReloadTLS)
	defer stopTLSReload()
	if err := srv.ServeWithReady(ctx, func() { fmt.Fprint(os.Stdout, message) }); err != nil {
		log.Error("server failed", "err", err)
		return 1
	}
	return 0
}

func serverAppURL(cfg *config.Config, srv *app.Server) string {
	origin, err := url.Parse(cfg.ExternalOrigin)
	if err != nil || origin.Host != cfg.Listen {
		return cfg.ExternalOrigin
	}
	origin.Host = srv.Addr
	return origin.String()
}

// runOperatorMode is the `hikyo operator` deployable (k8s-integration ADR): a
// separate process, not a mode of the running server. It loads no keyring and no
// root key — configuration is HIKYO_OPERATOR_* env only, read inside
// internal/operator. It is a real multicall MODE, never a client verb.
func runOperatorMode(ctx context.Context) int {
	operator.Version = version
	log := app.Logger(false)
	if err := operator.Run(ctx, log); err != nil {
		log.Error("operator failed", "err", err)
		return 1
	}
	return 0
}

// runUpdaterMode preserves the legacy command name but refuses execution before
// reading a profile, datastore, root key, or deployment credentials.
func runUpdaterMode(ctx context.Context, args []string) int {
	log := app.Logger(false)
	if err := updater.Run(ctx, args, log); err != nil {
		log.Error("updater failed", "err", err)
		return 1
	}
	return 0
}

// runAdmin is the local-admin group: `hikyo admin create` on the server's own
// host. It is a client verb of the same binary, not a new multicall mode -
// the mode set (server/operator/migrate/client) is unchanged. It shares
// runOperator's shape: own flags and explicit group-level development opt-in.
func runAdmin(ctx context.Context, args []string) int {
	return runOperator(ctx, "admin", args, app.RunAdmin)
}

// runOperator is the shared shape of the host-only operator verb groups
// (`admin`, `backup`, `restore`). Datastore/custody configuration comes from
// the server's environment. Only a leading group-level --dev opts into the
// distinct development trust domain. Verb flags and values are passed untouched.
func runOperator(ctx context.Context, name string, args []string,
	run func(context.Context, *config.Config, *slog.Logger, []string, io.Writer, *disclose.TerminalSession, error) error,
) int {
	var configurationArgs []string
	if len(args) > 0 && args[0] == "--dev" {
		configurationArgs, args = args[:1], args[1:]
	}
	cfg, warnings, err := config.Load(name, configurationArgs, os.Getenv, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 2
	}
	log := app.Logger(cfg.Dev)
	for _, w := range warnings {
		log.Warn(w)
	}
	terminalSession, terminalError := disclose.OpenTerminalSession()
	err = run(ctx, cfg, log, args, os.Stderr, terminalSession, terminalError)
	_ = terminalSession.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 1
	}
	return 0
}

func workdir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func runMigrate(ctx context.Context, args []string) int {
	cfg, warnings, err := config.Load("migrate", args, os.Getenv, os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo migrate:", err)
		return 1
	}
	log := app.Logger(cfg.Dev)
	for _, w := range warnings {
		log.Warn(w)
	}
	if err := app.RunMigrate(ctx, cfg, log); err != nil {
		log.Error("migration failed", "err", err)
		return 1
	}
	return 0
}

func runUpgradeOperator(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] != "operator" {
		fmt.Fprintln(os.Stderr, "usage: hikyo upgrade operator rotate --statement FILE --signature FILE --new-public-key FILE")
		return 2
	}
	cfg, warnings, err := config.Load("upgrade", nil, os.Getenv, os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo upgrade:", err)
		return 2
	}
	log := app.Logger(cfg.Dev)
	for _, warning := range warnings {
		log.Warn(warning)
	}
	if err := app.RunUpgradeOperator(ctx, cfg, args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "hikyo upgrade:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintf(os.Stderr, `hikyo — one binary, several roles

server commands:
  hikyo server [--dev] [--listen ADDR] [--auto-migrate=BOOL]
  hikyo migrate [--dev]

kubernetes operator (separate deployable; HIKYO_OPERATOR_* env only):
  hikyo operator

privileged local update helper (separate service; JSON config only):
  hikyo updater --config /etc/hikyo/updater.json

information:
  hikyo version
  hikyo about
  hikyo welcome

local host authority (server host only):
  hikyo admin [--dev] create --username USER
  hikyo backup [--dev] export [--out DIR]
  hikyo backup [--dev] keygen
  hikyo restore [--dev] run --from ARCHIVE --identity-file PATH
  hikyo restore [--dev] status
  hikyo restore [--dev] reconcile --principal ID

client verbs:
  %v
`, cli.Verbs)
}
