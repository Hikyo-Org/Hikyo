package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
)

// ---------------------------------------------------------------------------
// hikyo run -- <command>
// ---------------------------------------------------------------------------

func runRun(ctx context.Context, ios IO, args []string) error {
	// The child's command and ITS flags sit after the first `--`. Split BEFORE
	// any flag parsing: flag.Parse would otherwise consume `--` and then eat the
	// child's own flags (`hikyo run -- mycmd --config-only`).
	sep := slices.Index(args, "--")
	if sep < 0 {
		return failf(ExitUsage, "usage: hikyo run [flags] -- <command> [args...]")
	}
	hikyoArgs, childArgs := args[:sep], args[sep+1:]
	if len(childArgs) == 0 {
		return failf(ExitUsage, "hikyo run: no command after `--`")
	}

	var (
		configOnly       bool
		useHumanSession  bool
		allowOverrideRaw string
		projectDir       string
	)
	st, flags, err := parseCommon("run", ios, hikyoArgs, func(fs *flag.FlagSet) {
		fs.BoolVar(&configOnly, "config-only", false, "request the config-only projection: no secrets, a distinct authorized mode")
		fs.BoolVar(&useHumanSession, "use-human-session", false, "the locked #18 exception: run under the stored human session, gated by a TTY, an enumerated confirmation, and a live disclosure window")
		fs.StringVar(&allowOverrideRaw, "allow-override", "", "comma-separated keys whose inherited value the fetched value may replace")
		fs.StringVar(&projectDir, "project-directory", "", "directory to look up hikyo-compose.yaml from (walks up); optional")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("run"); err != nil {
		return err
	}
	allowOverride := splitCSV(allowOverrideRaw)

	// The single narrow #18 exception, restated exactly (api-cli-surface ADR line
	// 96): `run` — and only `run` — may use the stored human session, and only
	// when ALL of the flag, a TTY, an enumerated confirmation, and the bound
	// reauth ceremony hold. `render` and `sync` have no human path, so this branch
	// lives here rather than in resolveMachineTarget.
	if useHumanSession {
		cfg, _, err := findComposeConfig(startDir(ios, projectDir))
		if err != nil {
			return err
		}
		if flags.Auth == "machine" {
			return failf(ExitRefused, "hikyo run --use-human-session conflicts with --auth=machine")
		}
		flags.Auth = "human"
		return runHumanSession(ctx, ios, st, flags, cfg, childArgs, configOnly, allowOverride)
	}
	stack, err := openComposeStack(st, ios, flags, composeStackOptions{projectDir: projectDir, configOnly: configOnly})
	if err != nil {
		return err
	}
	snapshotBinding, err := stack.newSnapshotBinding([]string{runGenerationKey})
	if err != nil {
		return failf(ExitRefused, "hikyo run: snapshot binding: %v", err)
	}
	// Flush-before-fetch (ops-spec § 6 ordering rule): pending offline records
	// reconcile BEFORE the fetch proceeds; a failure refuses the fetch. A run
	// without config has no state dir, so the stack method is a no-op.
	if err := stack.flushOffline(ctx); err != nil {
		return err
	}

	// Loader-control acknowledgement (compose ADR § "Loader-control keys"): the
	// config's run block acknowledges by name. Resolved before the fetch so the
	// offline path can refuse a loader-control key BEFORE it appends any offline
	// record (finding 6).
	var ack []string
	if stack.cfg != nil {
		ack = stack.cfg.Run.AcknowledgeLoaderControl
	}

	resp, ferr := stack.fetchDelivery(ctx, ack, "")

	var (
		fetched map[string]string
		live    bool
	)
	if ferr != nil {
		f, herr := serveRunOffline(ios, stack.cfg, stack.stateDir, snapshotBinding, ack, ferr)
		if herr != nil {
			return herr
		}
		fetched = f
	} else {
		if stack.cfg != nil {
			snapshotBinding, err = bindSnapshotDelivery(snapshotBinding, resp)
			if err != nil {
				return failf(ExitRefused, "hikyo run: snapshot binding: %v", err)
			}
		}
		// All-or-nothing (compose ADR § "Authorization"): a secret the principal
		// cannot reveal makes the whole delivery refuse BEFORE anything else. Not
		// reached under --config-only, whose projection carries no secrets.
		if !configOnly {
			if missing := unrevealedSecrets(resp.Keys); len(missing) > 0 {
				return failf(ExitRefused, "hikyo run: cannot deliver secret(s) %s — %s",
					strings.Join(missing, ", "), machineRevealOptIn)
			}
		}
		fetched = deliveredValues(resp.Keys)
		live = true
	}

	// Loader-control refusal for the LIVE path (the offline path already checked
	// pre-append inside serveRunOffline).
	if refused, _ := delivery.Unacknowledged(slices.Sorted(maps.Keys(fetched)), ack); len(refused) > 0 {
		return failf(ExitRefused, "hikyo run: refusing loader-control key(s) %s; acknowledge each by name in the config's `run.acknowledge_loader_control`",
			strings.Join(refused, ", "))
	}

	// Merge: fetched wins; a differing collision is a hard error unless named in
	// --allow-override (compose ADR § "Merge, collisions"). The base is the
	// SANITIZED parent environment — HIKYO_TOKEN (the workload credential) never
	// reaches the child (finding 1).
	merged, _, err := compose.MergeEnv(sanitizedEnviron(), fetched, allowOverride)
	if err != nil {
		return &Error{Code: ExitRefused, Err: err}
	}

	// ARG_MAX preflight (ops-spec § 6): the execve composite bound, refused loud
	// pre-exec rather than as E2BIG at the wrong layer.
	if ok, detail := compose.ExecPreflight(merged, childArgs, compose.DefaultArgMax()); !ok {
		return failf(ExitRefused, "hikyo run: %s; reduce the delivered set or shorten the command", detail)
	}

	// Snapshot: after a LIVE delivering fetch and only when a config file exists.
	// Opt-in governs SERVING, not saving — a silent save failure is the silent
	// fallback the house forbids, so it is a hard error.
	if live && stack.cfg != nil {
		if err := saveRunSnapshot(stack.stateDir, snapshotBinding, resp); err != nil {
			return failf(ExitInternal, "hikyo run: saving offline snapshot: %v", err)
		}
	}

	// Exec. 127 = not found, 126 = found-but-not-executable — the child-side
	// convention (exit.go), the only exits outside the closed set. On success
	// there is no hikyo process (unix syscall.Exec): the child's status is the
	// invocation's.
	command := childArgs[0]
	resolvedPath, cerr := resolveChildCommand(command)
	if cerr != nil {
		return cerr
	}
	if err := ios.exec(resolvedPath, childArgs, merged); err != nil {
		return failf(ExitCommandNotExecutable, "hikyo run: %s: %v", command, err)
	}
	// Unreachable on a real unix exec (the process image is replaced); reached
	// only through the injected test seam, which returns nil to signal capture.
	return nil
}

// runHumanSession implements the locked #18 exception for `hikyo run`. All four
// conditions must hold before the child is launched:
//
//  1. the --use-human-session flag (the caller is here, so it is set);
//  2. stderr is a TTY — an ADDITIONAL refusal, never the control (ADR line 96):
//     a human session driving a non-interactive process is refused;
//  3. an enumerated confirmation — the environment and the exact key names to be
//     injected, printed to the controlling terminal, answered y/N there;
//  4. the bound reauth ceremony — a live disclosure window for the environment,
//     opened inline by the TOTP ceremony where the window allows it. Required
//     under --config-only too: the four conditions are locked as a set.
//
// The offline snapshot machinery is deliberately NOT reached on this path: a
// human-session snapshot served offline later would bypass the machine-only rule
// the delivery model rests on, so a human-session run never saves one.
func runHumanSession(ctx context.Context, ios IO, st *State, flags commonFlags, cfg *compose.Config, childArgs []string, configOnly bool, allowOverride []string) error {
	// (2) TTY gate, first: a refusal that needs no session and no server. Both
	// halves are required by name: a controlling terminal (the confirmation and
	// the code are read there) AND stderr being a TTY (the locked condition -
	// a human session driving a process whose stderr is captured is refused).
	terminalSession, terminalErr := ios.terminalSession()
	if terminalErr != nil {
		return failf(ExitRefused,
			"hikyo run --use-human-session requires a controlling terminal for the confirmation and reauth ceremony: %v", terminalErr)
	}
	if ios.StderrIsTerminal == nil || !ios.StderrIsTerminal() {
		return failf(ExitRefused,
			"hikyo run --use-human-session requires stderr to be a terminal; a captured stderr means a non-interactive process, which the human-session exception refuses")
	}

	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	session, err := requireHumanSession("hikyo run --use-human-session", artifact)
	if err != nil {
		return err
	}
	if cfg != nil {
		for _, d := range []struct {
			dim Dimension
			val string
		}{{DimOrg, cfg.Org}, {DimProject, cfg.Project}, {DimEnv, cfg.Environment}} {
			if err := foldConfigDim(&resolved, d.dim, d.val, composeConfigName); err != nil {
				return err
			}
		}
	}
	project, err := projectBase(resolved)
	if err != nil {
		return err
	}
	org, env := resolved.Get(DimOrg), resolved.Get(DimEnv)
	if echo := resolved.Echo(); echo != "" {
		fmt.Fprintf(ios.Stderr, "target: %s [origin %s, artifact human-session]\n", echo, session.Origin)
	}

	// (4) Bound reauth ceremony: a live disclosure window over the environment,
	// opened inline (TOTP) where the environment's window allows it; a
	// 0-window environment is refused with the browser path named. Required
	// under --config-only too: the exception's four conditions are locked
	// without a projection carve-out (api-cli-surface ADR).
	if err := ensureRevealWindow(ctx, client, st, ios, &session, project, env,
		// The unit is what the run will inject: the environment's secret keys
		// for the full projection, its config keys under --config-only - the
		// exception's "enumerated environment/key-set" named as the act is.
		disclosure{purpose: "reveal", keys: func(ctx context.Context, env string) ([]string, error) {
			class := apigen.KeyClassificationSecret
			if configOnly {
				class = apigen.KeyClassificationConfig
			}
			return keyIDsOf(ctx, client, project, env, class, nil)
		}},
		failf(ExitAuth, "a live disclosure window is required: run the reveal ceremony first")); err != nil {
		return err
	}

	resp, err := fetchDelivery(ctx, client, org, resolved.Get(DimProject), env, configOnly, nil, "")
	if err != nil {
		return err
	}
	if !configOnly {
		if missing := unrevealedSecrets(resp.Keys); len(missing) > 0 {
			return failf(ExitRefused, "hikyo run: cannot deliver secret(s) %s — %s",
				strings.Join(missing, ", "), machineRevealOptIn)
		}
	}
	fetched := deliveredValues(resp.Keys)

	// (3) Enumerated confirmation on the controlling terminal.
	names := slices.Sorted(maps.Keys(fetched))
	prompt := fmt.Sprintf("About to inject %d value(s) into environment %s and exec %q:\n  %s\nProceed",
		len(names), env, childArgs[0], strings.Join(names, "\n  "))
	ok, err := terminalSession.ConfirmEnumerated(prompt)
	if err != nil {
		return failf(ExitRefused, "reading the confirmation: %v", err)
	}
	if !ok {
		return failf(ExitRefused, "hikyo run --use-human-session: declined at the confirmation")
	}

	if refused, _ := delivery.Unacknowledged(slices.Sorted(maps.Keys(fetched)), runLoaderControlAck(cfg)); len(refused) > 0 {
		return failf(ExitRefused, "hikyo run: refusing loader-control key(s) %s; acknowledge each by name in the config's `run.acknowledge_loader_control`",
			strings.Join(refused, ", "))
	}
	merged, _, err := compose.MergeEnv(sanitizedEnviron(), fetched, allowOverride)
	if err != nil {
		return &Error{Code: ExitRefused, Err: err}
	}
	if ok, detail := compose.ExecPreflight(merged, childArgs, compose.DefaultArgMax()); !ok {
		return failf(ExitRefused, "hikyo run: %s; reduce the delivered set or shorten the command", detail)
	}
	command := childArgs[0]
	resolvedPath, cerr := resolveChildCommand(command)
	if cerr != nil {
		return cerr
	}
	if err := ios.exec(resolvedPath, childArgs, merged); err != nil {
		return failf(ExitCommandNotExecutable, "hikyo run: %s: %v", command, err)
	}
	return nil
}

// runLoaderControlAck is the loader-control acknowledgement in force for a run,
// from the config's run block (empty when there is no config).
func runLoaderControlAck(cfg *compose.Config) []string {
	if cfg == nil {
		return nil
	}
	return cfg.Run.AcknowledgeLoaderControl
}

// serveRunOffline handles a failed run fetch: if it failed as UNAVAILABLE and
// the stack opted into offline serve, it opens the snapshot, refuses any
// unacknowledged loader-control key (finding 6, BEFORE any record is written),
// records one offline disclosure per key BEFORE returning the values, prints the
// stale line, and returns them. Any other failure (or no opt-in) is surfaced
// unchanged. The snapshot is bound to run's identity: TargetNames ["__run__"],
// so a render snapshot cannot be served here (finding 3).
func serveRunOffline(ios IO, cfg *compose.Config, stateDir string, binding crypto.SnapshotBinding, ack []string, fetchErr error) (map[string]string, error) {
	if !isUnavailable(fetchErr) {
		return nil, fetchErr
	}
	if cfg == nil || !cfg.Snapshot.OfflineServe {
		fmt.Fprintln(ios.Stderr, "hikyo run: offline serve is not enabled for this stack; set snapshot.offline_serve: true in hikyo-compose.yaml to serve stale values during an outage")
		return nil, fetchErr
	}
	payload, binding, err := loadOfflineSnapshot(ios, cfg, binding)
	if err != nil {
		return nil, err
	}
	// Loader-control BEFORE any disclosure record is written (finding 6): an
	// unacknowledged loader-control key is refused even when serving stale.
	if refused, _ := delivery.Unacknowledged(rowNames(payload.Rows), ack); len(refused) > 0 {
		return nil, failf(ExitRefused, "hikyo run: refusing loader-control key(s) %s; acknowledge each by name in the config's `run.acknowledge_loader_control`",
			strings.Join(refused, ", "))
	}
	stamp := payload.GenerationStamps[runGenerationKey]
	if err := appendOfflineRecords(ios, stateDir, payload.Rows, binding, stamp); err != nil {
		return nil, failf(ExitInternal, "hikyo run: recording offline disclosure: %v", err)
	}
	aad, err := binding.AAD()
	if err != nil {
		return nil, failf(ExitInternal, "hikyo run: reading offline snapshot binding: %v", err)
	}
	fmt.Fprintf(ios.Stderr, "serving stale from %s, generation %s\n", aad.IssuedAt, stamp)
	return rowsToValues(payload.Rows), nil
}

// saveRunSnapshot seals the delivered env plus one run "generation" stamp. The
// snapshot's TargetNames is ["__run__"] (run holds no render target), so
// ContextMatches refuses a render snapshot for run and vice versa (finding 3),
// and its credential binding is the fingerprint of the presented token (R1-3),
// authenticated in the header — no separate credential record on disk.
func saveRunSnapshot(stateDir string, binding crypto.SnapshotBinding, resp apigen.DeliveryResponse) error {
	if _, err := binding.CanonicalAAD(); err != nil {
		return err
	}
	keys, err := loadLocalKeys(stateDir)
	if err != nil {
		return err
	}
	rows := deliveredRows(resp.Keys)
	// run's "generation" stamp is over the canonical row set, keyed to the
	// run generation key (TargetStamp does not grammar-check the name — only
	// WriteGeneration does — so the __run__ sentinel is legal here).
	stamp := compose.TargetStamp(keys, runGenerationKey, canonicalRows(rows))
	payload := compose.SnapshotPayload{Rows: rows, GenerationStamps: map[string]string{runGenerationKey: stamp}}
	return saveSnapshot(keys, binding, payload)
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func unrevealedSecrets(keys []apigen.DeliveredKey) []string {
	var missing []string
	for _, k := range keys {
		if isUnrevealedSecret(k) {
			missing = append(missing, k.Name)
		}
	}
	slices.Sort(missing)
	return missing
}

func isUnrevealedSecret(k apigen.DeliveredKey) bool {
	return k.Presence == apigen.DeliveredKeyPresenceSet &&
		k.Classification == apigen.KeyClassificationSecret && k.Value == nil
}

func deliveredValues(keys []apigen.DeliveredKey) map[string]string {
	out := map[string]string{}
	for _, k := range keys {
		if k.Value != nil {
			out[k.Name] = *k.Value
		}
	}
	return out
}

func deliveredRows(keys []apigen.DeliveredKey) []compose.SnapshotRow {
	var rows []compose.SnapshotRow
	for _, k := range keys {
		if k.Value == nil {
			continue
		}
		rows = append(rows, compose.SnapshotRow{Name: k.Name, KeyID: k.KeyId, Classification: string(k.Classification), Value: *k.Value})
	}
	return rows
}

// rowNames returns the snapshot row names (for the loader-control check).
func rowNames(rows []compose.SnapshotRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func rowsToValues(rows []compose.SnapshotRow) map[string]string {
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Name] = r.Value
	}
	return out
}

func canonicalRows(rows []compose.SnapshotRow) []byte {
	sorted := slices.SortedFunc(slices.Values(rows), func(a, b compose.SnapshotRow) int { return cmp.Compare(a.Name, b.Name) })
	data, _ := json.Marshal(sorted)
	return data
}

// sanitizedEnviron returns the process environment with the workload credential
// (HIKYO_TOKEN) removed: it is the ONLY credential-transport env var (the token
// file is a flag, not an env var), and it must never reach the child or any
// subprocess the CLI spawns (finding 1). Building the child env from this — not
// os.Environ() — means the credential was never present, not stripped after.
//
// CREDENTIALS_DIRECTORY is deliberately NOT stripped (R1-1, accepted): it is a
// directory PATH, not a secret, and access to the files under it is per-open and
// uid-gated by systemd — so stripping the variable protects nothing, while
// stripping it would break a child that legitimately consumes its own systemd
// credentials.
func sanitizedEnviron() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, e := range src {
		if strings.HasPrefix(e, "HIKYO_TOKEN=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// resolveChildCommand resolves the command after `--`, distinguishing 127 (not
// found) from 126 (found but not executable). exec.LookPath alone is
// insufficient: for a BARE name it swallows a found-but-non-executable file as
// ErrNotFound (verified on darwin), so that case is detected by scanning PATH; a
// command containing a path separator is stat'd directly and LookPath surfaces
// fs.ErrPermission (finding 13).
//
// A command resolved ONLY via a relative PATH entry (`.` or any non-absolute
// dir) returns exec.ErrDot: Go deliberately refuses to run cwd-controlled code
// implicitly, and so do we — treat it as NOT FOUND (127), naming the relative
// entry, never execute it (NEW-1).
func resolveChildCommand(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err == nil {
		return path, nil
	}
	if errors.Is(err, exec.ErrDot) {
		return "", failf(ExitCommandNotFound, "hikyo run: %s: resolved only via the relative PATH entry %q; refusing to execute cwd-controlled code — use an absolute path or a PATH of absolute directories", command, filepath.Dir(path))
	}
	if errors.Is(err, fs.ErrPermission) {
		return "", failf(ExitCommandNotExecutable, "hikyo run: %s: found but not executable", command)
	}
	if !strings.ContainsRune(command, filepath.Separator) && pathHasNonExecutable(command) {
		return "", failf(ExitCommandNotExecutable, "hikyo run: %s: found on PATH but not executable", command)
	}
	return "", failf(ExitCommandNotFound, "hikyo run: %s: command not found", command)
}

// pathHasNonExecutable reports whether a bare command name matches a regular
// file on PATH that lacks execute permission. It reads the PROCESS PATH (the
// same one exec.LookPath consulted), not an injected env, so the two agree.
func pathHasNonExecutable(command string) bool {
	// Windows has no 0o111 execute bit (executability is by extension via
	// PATHEXT), so this Unix-mode scan would misclassify; LookPath already
	// classifies correctly there.
	if runtime.GOOS == "windows" {
		return false
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		fi, err := os.Stat(filepath.Join(dir, command))
		if err != nil || fi.IsDir() {
			continue
		}
		if fi.Mode().Perm()&0o111 == 0 {
			return true
		}
	}
	return false
}
