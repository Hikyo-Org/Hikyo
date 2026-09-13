// hikyo is the multicall binary: `hikyo server` and `hikyo migrate` are real;
// client verbs are stubs until their tickets land.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/importer"
	"github.com/Hikyo-Org/hikyo/internal/operator"
	binaryupdate "github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
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
	if handled, code := runRolloutAuthorityStage(os.Args[1:]); handled {
		return code
	}
	if handled, code := runRootKeyStageMode(os.Args[1:]); handled {
		return code
	}
	if handled, code := runTLSStageMode(os.Args[1:]); handled {
		return code
	}
	if handled, code := importer.RunInternalSubprocess(os.Args[1:], os.Stdout); handled {
		return code
	}
	arguments, verbosity := cli.ParseVerbosity(os.Args[1:])
	if len(arguments) == 0 {
		usage(os.Stderr)
		return 2
	}
	invocation, handled, code := runHelp(arguments, os.Stdout, os.Stderr)
	if handled {
		return code
	}
	cmd, args := invocation[0], invocation[1:]
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
	ctx = diagnostics.With(ctx, verbosity, os.Stderr)
	if slices.Contains([]string{"server", "operator", "config-rollout", "updater", "migrate", "upgrade", "admin", "backup", "escrow", "restore"}, cmd) && !cli.HelpRequested(args) {
		diagnostics.Printf(ctx, 1, "starting %s", cmd)
		defer diagnostics.Time(ctx, cmd)()
	}
	if shouldCheckForUpdate(cmd) {
		terminalSession, terminalError := disclose.OpenTerminalSession()
		updated := cli.NotifyUpdate(ctx, updateIO(terminalSession, terminalError, builtChannel))
		_ = terminalSession.Close()
		if updated {
			return 0
		}
	}

	switch {
	case cmd == "--upgrade-bundle-formats":
		fmt.Fprintln(os.Stdout, upgradebundle.IndexFormat)
		fmt.Fprintln(os.Stdout, upgradebundle.PlatformIndexFormat)
		return 0
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
	case cmd == "config-rollout":
		return runConfigRollout(ctx, args, os.Stderr)
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
	case cmd == "escrow":
		return runOperator(ctx, "escrow", args, app.RunEscrow)
	case cmd == "restore":
		return runOperator(ctx, "restore", args, app.RunRestore)
	case slices.Contains(cli.Verbs, cmd):
		if cli.HelpRequested(args) {
			// Help never opens the controlling terminal.
			return cli.Run(ctx, updateIO(nil, nil, builtChannel), invocation)
		}
		terminalSession, terminalError := disclose.OpenTerminalSession()
		return cli.Run(ctx, updateIO(terminalSession, terminalError, builtChannel), invocation)
	default:
		fmt.Fprintf(os.Stderr, "hikyo: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

// runHelp answers `hikyo --help`, `hikyo help [command...]` and
// `hikyo <command...> --help` for every role of the binary, on stdout with
// exit 0, before any mode touches its configuration, datastore, network, or
// terminal. Client verbs answer inside cli.Run, which owns their help text,
// so for them only the `help` spelling is rewritten and handed back.
func runHelp(args []string, stdout, stderr io.Writer) (invocation []string, handled bool, code int) {
	if args[0] == "help" {
		args = append(slices.Clone(args[1:]), "--help")
	}
	cmd := args[0]
	if cli.IsHelpFlag(cmd) {
		usage(stdout)
		return args, true, 0
	}
	if slices.Contains(cli.Verbs, cmd) || !cli.HelpRequested(args[1:]) {
		return args, false, 0
	}
	switch cmd {
	case "client":
		cli.Usage(stdout)
	case "admin":
		app.AdminUsage(stdout)
	case "backup":
		app.BackupUsage(stdout)
	case "restore":
		app.RestoreUsage(stdout)
	case "escrow":
		app.EscrowUsage(stdout)
	case "server", "migrate", "config-rollout":
		// These parse their own flag sets, and the flag package's generated
		// usage is the complete, always-current list of what they accept.
		return args, false, 0
	case "upgrade":
		// The automatic upgrade parses its own flags; `upgrade operator`
		// loads server configuration first, so it answers from the text.
		if len(args) < 2 || args[1] != "operator" {
			return args, false, 0
		}
		cli.HelpFromText(stdout, usageText, cli.CommandPath(args))
	default:
		if !cli.HelpFromText(stdout, usageText, cli.CommandPath(args)) {
			fmt.Fprintf(stderr, "hikyo: unknown command %q\n\n", cmd)
			usage(stderr)
			return args, true, 2
		}
	}
	return args, true, 0
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
	loaded := diagnostics.Time(ctx, "load bootstrap configuration")
	cfg, warnings, err := config.LoadBootstrap("server", args, os.Getenv, os.Environ())
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	loaded()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo server:", err)
		return 1
	}
	diagnostics.Printf(ctx, 1, "Bootstrap configuration loaded")
	log := app.Logger(cfg.Dev)
	for _, w := range warnings {
		log.Warn(w)
	}
	window := app.NewUpgradeWindow()
	upgradeCtx, cancelUpgrade := context.WithCancel(ctx)
	defer cancelUpgrade()
	var windowWatchDone chan struct{}
	cleanupUpgrade, err := app.RunUnattendedUpgrade(upgradeCtx, cfg, app.UnattendedUpgradeOptions{
		Progress: window.Progress,
		PreparedConfiguration: func(effective *config.Config) error {
			if err := window.Start(effective); err != nil {
				return err
			}
			windowWatchDone = make(chan struct{})
			go func() {
				defer close(windowWatchDone)
				if err := window.Wait(upgradeCtx); err != nil {
					log.Error("upgrade maintenance listener failed", "err", err)
					cancelUpgrade()
				}
			}()
			return nil
		},
	})
	defer func() {
		if err := cleanupUpgrade(); err != nil {
			log.Error("release unattended upgrade custody", "err", err)
		}
	}()
	defer func() {
		cancelUpgrade()
		if windowWatchDone != nil {
			<-windowWatchDone
		}
		if err := window.Close(); err != nil {
			log.Error("close upgrade maintenance listener", "err", err)
		}
	}()
	if err != nil {
		log.Error("unattended upgrade failed", "err", err)
		if window.Started() && ctx.Err() == nil && upgradeCtx.Err() == nil {
			window.Progress("recovery-required")
			_ = window.Wait(ctx)
		}
		return 1
	}
	cancelUpgrade()
	if windowWatchDone != nil {
		<-windowWatchDone
	}
	if err := window.Close(); err != nil {
		log.Error("upgrade maintenance handoff failed", "err", err)
		return 1
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
	if len(configurationArgs) == 0 && os.Getenv("HIKYO_DB") == "" {
		if code, handled := runOperatorThroughDeployment(ctx, name, args); handled {
			return code
		}
	}
	cfg, warnings, err := config.LoadBootstrap(name, configurationArgs, os.Getenv, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 2
	}
	log := app.Logger(cfg.Dev)
	for _, w := range warnings {
		log.Warn(w)
	}
	terminalSession, terminalError := disclose.OpenTerminalSession()
	var output io.Writer = os.Stderr
	if name == "backup" && len(args) > 0 && args[0] == "upgrade-export" {
		// Public artifact locators (including --json) are command results.
		// Diagnostics retain stderr so the coordinator can parse stdout alone.
		output = os.Stdout
	}
	err = run(ctx, cfg, log, args, output, terminalSession, terminalError)
	_ = terminalSession.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 1
	}
	return 0
}

// runOperatorThroughDeployment lets a root shell on a systemd host run an
// operator verb without hand-feeding the service's configuration. When the
// root-owned deployment file that `hikyo upgrade` maintains exists, the verb
// runs as the runtime user with the unit's exact environment and root key.
// Without that file nothing is assumed; the ordinary environment path follows.
func runOperatorThroughDeployment(ctx context.Context, name string, args []string) (int, bool) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return 0, false
	}
	if _, err := os.Lstat(hostupgrade.ConfigPath); err != nil {
		return 0, false
	}
	c, err := hostupgrade.LoadConfig(hostupgrade.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %s: %v\n", name, hostupgrade.ConfigPath, err)
		return 2, true
	}
	host, err := hostupgrade.New(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 2, true
	}
	diagnostics.Printf(ctx, 1, "running operator command through host deployment")
	operatorArgs := cli.WithVerbosityArguments(append([]string{name}, args...), diagnostics.Level(ctx))
	err = host.RunOperator(ctx, operatorArgs, os.Stdin, os.Stdout, os.Stderr)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), true
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "hikyo %s: %v\n", name, err)
		return 2, true
	}
	return 0, true
}

func workdir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func runMigrate(ctx context.Context, args []string) int {
	loaded := diagnostics.Time(ctx, "load bootstrap configuration")
	cfg, warnings, err := config.Load("migrate", args, os.Getenv, os.Environ())
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	loaded()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hikyo migrate:", err)
		return 1
	}
	diagnostics.Printf(ctx, 1, "Bootstrap configuration loaded")
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
		terminal, terminalErr := disclose.OpenTerminalSession()
		defer terminal.Close()
		readPassword := func(prompt string) (string, error) {
			if terminalErr != nil {
				return "", terminalErr
			}
			return terminal.ReadPassword(prompt)
		}
		err := app.RunAutomaticUpgrade(ctx, args, os.Stdout, readPassword)
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var handoff *app.AutomaticHandoff
		if errors.As(err, &handoff) {
			if err := terminal.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "hikyo upgrade:", err)
				return 1
			}
			diagnostics.Printf(ctx, 1, "handing upgrade to verified candidate")
			command := exec.CommandContext(ctx, handoff.Executable, cli.WithVerbosityArguments(handoff.Arguments, diagnostics.Level(ctx))...)
			command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
			command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
			err = command.Run()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "hikyo upgrade:", err)
			return 1
		}
		return 0
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

func usage(w io.Writer) {
	fmt.Fprint(w, usageText)
	fmt.Fprint(w, cli.VerbosityHelp())
	fmt.Fprintf(w, "\nclient verbs (hikyo help client prints the full reference):\n%s\n", wrapWords(cli.Verbs, 2, 78))
}

// usageText is the multicall help. cmd/hikyo's TestUsage keeps it aligned
// with run's dispatch table; the client verb list is appended from cli.Verbs.
const usageText = `hikyo - one binary, several roles

  hikyo --help                          this overview
  hikyo help <command...>               help for one command, at any depth
  hikyo <command...> --help             the same

server:
  hikyo server [--dev] [--listen ADDR] [--auto-migrate=BOOL] [flags]
                                        hikyo server --help lists every flag
  hikyo migrate [--dev]
  sudo hikyo upgrade [--target VERSION] [--config FILE]
                                        verified in-place upgrade on a systemd host
  hikyo upgrade operator rotate --statement FILE --signature FILE --new-public-key FILE

kubernetes operator (separate deployable; HIKYO_OPERATOR_* env only):
  hikyo operator
  hikyo config-rollout [--enrollment-file PATH] [--authority-public-key PATH]

privileged local update helper (separate service; JSON config only):
  hikyo updater --config /etc/hikyo/updater.json
                                        remote apply is disabled; see hikyo upgrade

information:
  hikyo version                         readable build summary
  hikyo --version                       the version alone, for scripts
  hikyo about
  hikyo welcome

local host authority (server host only; --dev directly after the group):
  hikyo admin [--dev] create --username USER
  hikyo admin [--dev] reset-credential --principal ID
  hikyo admin [--dev] grant --principal ID --capability CAP
  hikyo admin [--dev] privacy export|restrict|erase|release|correct|reapply
  hikyo admin [--dev] config status|recover
  hikyo backup [--dev] export [--out DIR] [--recipient R]... [--passphrase-file PATH]
  hikyo backup [--dev] keygen
  hikyo backup [--dev] upgrade-export|upgrade-drill
  hikyo escrow [--dev] verify --root-key-file FILE --assert-separate-custody
  hikyo restore [--dev] run --from ARCHIVE (--identity-file PATH | --passphrase-file PATH)
  hikyo restore [--dev] status
  hikyo restore [--dev] reconcile --principal ID
  hikyo restore [--dev] drill --from ARCHIVE

  each group answers --help with its full synopsis: hikyo backup --help

`

// wrapWords lays words out in lines of at most width columns, each indented.
func wrapWords(words []string, indent, width int) string {
	var b strings.Builder
	line := strings.Repeat(" ", indent)
	for _, word := range words {
		if len(line) > indent && len(line)+1+len(word) > width {
			b.WriteString(line + "\n")
			line = strings.Repeat(" ", indent)
		}
		if len(line) > indent {
			line += " "
		}
		line += word
	}
	b.WriteString(line + "\n")
	return b.String()
}
