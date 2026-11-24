package cli

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
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
	if crypto.ParseArtifact(stack.token, crypto.ArtifactDeveloperCredential) == nil {
		return runDeveloperOnline(ctx, ios, stack, childArgs, allowOverride)
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

	merged, err := prepareRunEnvironment(fetched, ack, allowOverride, childArgs)
	if err != nil {
		return err
	}

	// Snapshot: after a LIVE delivering fetch and only when a config file exists.
	// Opt-in governs SERVING, not saving — a silent save failure is the silent
	// fallback the house forbids, so it is a hard error.
	if live && stack.cfg != nil {
		if err := saveRunSnapshot(stack.stateDir, snapshotBinding, resp); err != nil {
			return failf(ExitInternal, "hikyo run: saving offline snapshot: %v", err)
		}
	}

	return executeRunChild(ios, childArgs, merged)
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
		// A human session must agree with the repository's instance declaration,
		// just like a machine credential. Reject before disclosure or reauth I/O.
		if strings.TrimSpace(cfg.Instance) != "" {
			cfgOrigin, err := CanonicalOrigin(cfg.Instance)
			if err != nil {
				return err
			}
			if !sameOrigin(session.Origin, cfgOrigin) {
				return failf(ExitUsage,
					"instance %q resolves to origin %s but %s names %s; refusing rather than picking one",
					session.Instance, session.Origin, composeConfigName, cfgOrigin)
			}
		}
		for _, d := range []struct {
			dim Dimension
			val string
		}{{DimOrg, cfg.Org}, {DimProject, cfg.Project}, {DimEnv, cfg.Environment}} {
			if err := foldConfigDim(&resolved, d.dim, d.val, composeConfigName, SourceConfig); err != nil {
				return err
			}
		}
	}
	project, err := projectBase(resolved)
	if err != nil {
		return err
	}
	base, err := environmentBase(project, resolved, flags, "run --use-human-session")
	if err != nil {
		return err
	}
	env := resolved.Get(DimEnv)
	if echo := resolved.Echo(); echo != "" {
		fmt.Fprintf(ios.Stderr, "target: %s [origin %s, artifact human-session]\n", echo, session.Origin)
	}

	// (3) Enumerated confirmation, from revision metadata: names cross here, and
	// plaintext and disclosure records come only after the human has agreed.
	var covered apigen.RevisionDetail
	if err := client.Do(ctx, http.MethodGet, base+"/revisions/latest", nil, &covered); err != nil {
		return err
	}
	var names []string
	for _, key := range covered.Keys {
		if !configOnly || !key.Sensitive {
			names = append(names, key.Name)
		}
	}
	slices.Sort(names)
	if err := refuseLoaderControl(names, runLoaderControlAck(cfg)); err != nil {
		return err
	}
	if err := refuseHumanPersonalKeys(ctx, client, project, env, names); err != nil {
		return err
	}
	prompt := fmt.Sprintf("About to inject %d value(s) into environment %s and exec %q:\n  %s\nProceed",
		len(names), env, childArgs[0], strings.Join(names, "\n  "))
	ok, err := terminalSession.ConfirmEnumerated(prompt)
	if err != nil {
		return failf(ExitRefused, "reading the confirmation: %v", err)
	}
	if !ok {
		return failf(ExitRefused, "hikyo run --use-human-session: declined at the confirmation")
	}

	// (4) Bound reauth ceremony: a live disclosure window over the environment,
	// opened inline (TOTP) where the environment's window allows it, otherwise by
	// the browser's ceremony over the secret keys the run injects, or the config
	// keys under --config-only. Required under --config-only too: the exception's
	// four conditions are locked without a projection carve-out (api-cli-surface ADR).
	d := disclosure{purpose: "reveal", keys: func(ctx context.Context, env string) ([]string, error) {
		if !configOnly {
			return sensitiveKeyIDs(covered.Keys), nil
		}
		// A config-only export spends no single-decision window, so at a 0-second
		// window the passkey ceremony would authorize nothing this run does.
		var window apigen.RevealWindow
		if err := client.Do(ctx, http.MethodGet, revealWindowPath(project, env), nil, &window); err != nil {
			return nil, err
		}
		if window.EffectiveWindowSeconds <= 0 {
			return nil, failf(ExitRefused, "hikyo run --use-human-session --config-only: environment %s takes a passkey ceremony per disclosure, and a config-only run has no disclosure to spend it on; run without --config-only, or use a machine credential", env)
		}
		var unit []string
		for _, key := range covered.Keys {
			if !key.Sensitive {
				unit = append(unit, string(key.KeyId))
			}
		}
		return unit, nil
	}}
	if err := ensureRevealWindow(ctx, client, st, ios, &session, project, env, d,
		failf(ExitAuth, "a live disclosure window is required: run the reveal ceremony first")); err != nil {
		return err
	}

	// delivery.fetch refuses human sessions at artifact admission, so this
	// exception discloses through the human export verb. Pinning the confirmed
	// revision keeps a publish during this run from disclosing unconfirmed keys:
	// it makes this export historical, and secrets there need reveal-history.
	reveal := !configOnly
	var exported apigen.ExportedValues
	if err := client.Do(ctx, http.MethodPost, base+"/values/export",
		apigen.ExportValuesRequest{Reveal: &reveal, Revision: &covered.Revision}, &exported); err != nil {
		// Only a refusal the server answered proves nothing was disclosed.
		var refusal *Error
		if asCLIError(err, &refusal) && (refusal.Code == ExitRefused || refusal.Code == ExitNotFound) {
			var latest apigen.RevisionDetail
			if client.Do(ctx, http.MethodGet, base+"/revisions/latest", nil, &latest) == nil && latest.Revision != covered.Revision {
				return failf(ExitRefused, "hikyo run --use-human-session: environment %s was published to during this run; nothing was disclosed or injected, run it again (server: %v)", env, err)
			}
		}
		return err
	}
	fetched := map[string]string{}
	for _, item := range exported.Items {
		if item.Value != nil {
			fetched[item.Name] = *item.Value
		}
	}
	// A key reclassified during this run can leave a confirmed name undelivered.
	var missing []string
	for _, name := range names {
		if _, ok := fetched[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return failf(ExitRefused, "hikyo run: the server did not deliver confirmed key(s) %s; nothing was injected", strings.Join(missing, ", "))
	}

	merged, err := prepareRunEnvironment(fetched, runLoaderControlAck(cfg), allowOverride, childArgs)
	if err != nil {
		return err
	}
	return executeRunChild(ios, childArgs, merged)
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
	if err := refuseLoaderControl(rowNames(payload.Rows), ack); err != nil {
		return nil, err
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
		rows = append(rows, compose.SnapshotRow{Name: k.Name, KeyID: k.KeyId, Classification: string(k.Classification), Value: *k.Value, Receipt: deliveryReceipt(k)})
	}
	return rows
}

func deliveryReceipt(key apigen.DeliveredKey) string {
	if key.SnapshotReceipt == nil {
		return ""
	}
	return *key.SnapshotReceipt
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
	data, _ := json.Marshal(sorted, jsonwire.Stable)
	return data
}

// sanitizedEnviron returns the process environment without Hikyo's control
// namespace. That namespace includes bearer credentials, database URLs, and
// root encryption keys; none may reach a workload or Docker subprocess.
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
		name, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(strings.ToUpper(name), "HIKYO_") {
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

// Developer delivery cannot read or write snapshots, cursors, or offline audit queues.
func runDeveloperOnline(ctx context.Context, ios IO, stack *composeStack, childArgs, allowOverride []string) error {
	ack := runLoaderControlAck(stack.cfg)
	response, err := stack.fetchDelivery(ctx, ack, "")
	if err != nil {
		return err
	}
	if err := refusePersonalKeys(ios, response.Keys); err != nil {
		return err
	}
	if !stack.configOnly && len(unrevealedSecrets(response.Keys)) > 0 {
		return failf(ExitRefused, "developer credential cannot reveal the entire delivered secret set")
	}
	fetched := deliveredValues(response.Keys)
	merged, err := prepareRunEnvironment(fetched, ack, allowOverride, childArgs)
	if err != nil {
		return err
	}
	return executeRunChild(ios, childArgs, merged)
}

// refuseLoaderControl refuses a key that can steer the child's loader unless the
// config acknowledges it by name.
func refuseLoaderControl(names, ack []string) error {
	if refused, _ := delivery.Unacknowledged(names, ack); len(refused) > 0 {
		return failf(ExitRefused, "hikyo run: refusing loader-control key(s) %s; acknowledge each by name in the config's `run.acknowledge_loader_control`",
			strings.Join(refused, ", "))
	}
	return nil
}

// prepareRunEnvironment validates and merges values before any snapshot or child execution.
func prepareRunEnvironment(fetched map[string]string, ack, allowOverride, childArgs []string) ([]string, error) {
	if err := refuseLoaderControl(slices.Sorted(maps.Keys(fetched)), ack); err != nil {
		return nil, err
	}

	// Merge: fetched wins; a differing collision is a hard error unless named in
	// --allow-override (compose ADR § "Merge, collisions"). The base is the
	// SANITIZED parent environment — HIKYO_TOKEN (the workload credential) never
	// reaches the child (finding 1).
	merged, _, err := compose.MergeEnv(sanitizedEnviron(), fetched, allowOverride)
	if err != nil {
		return nil, &Error{Code: ExitRefused, Err: err}
	}

	// ARG_MAX preflight (ops-spec § 6): the execve composite bound, refused loud
	// pre-exec rather than as E2BIG at the wrong layer.
	if ok, detail := compose.ExecPreflight(merged, childArgs, compose.DefaultArgMax()); !ok {
		return nil, failf(ExitRefused, "hikyo run: %s; reduce the delivered set or shorten the command", detail)
	}

	return merged, nil
}

func executeRunChild(ios IO, childArgs, merged []string) error {
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

// refusePersonalKeys applies the personal-values ADR's no-silent-fallback rule
// to a developer delivery: a key the owner holds a value or notice for, but
// which arrived without that value, refuses the run naming the cause. Keys
// served the owner's own value are named on stderr, never their values.
func refusePersonalKeys(ios IO, keys []apigen.DeliveredKey) error {
	var refused, overridden []string
	for _, k := range keys {
		if k.Personal == nil {
			continue
		}
		switch *k.Personal {
		case apigen.DeliveredKeyPersonalValue:
			overridden = append(overridden, k.Name)
		default:
			reason := ""
			if k.PersonalReason != nil {
				reason = *k.PersonalReason
			}
			if *k.Personal == apigen.DeliveredKeyPersonalMissing {
				reason = "required"
			}
			refused = append(refused, fmt.Sprintf("%s (%s)", k.Name, personalRefusalCause(reason)))
		}
	}
	slices.Sort(refused)
	if len(refused) > 0 {
		return failf(ExitRefused, "hikyo run: refusing key(s) you hold a personal value or notice for, rather than serving the shared value: %s",
			strings.Join(refused, ", "))
	}
	if len(overridden) > 0 {
		slices.Sort(overridden)
		fmt.Fprintf(ios.Stderr, "hikyo run: using your personal value for %s\n", strings.Join(overridden, ", "))
	}
	return nil
}

func personalRefusalCause(reason string) string {
	switch reason {
	case "consent":
		return "this developer credential was minted without --personal; mint a new one with hikyo dev session --personal"
	case "config_only":
		return "--config-only delivers no personal values"
	case "suspended":
		return "your personal value is suspended while your account does not qualify"
	case "required":
		return "a personal value is required here and you have none; set it with hikyo personal set"
	case "":
		return "personal value withheld"
	default:
		return "removal notice: " + reason + "; set a new value or dismiss it with hikyo personal set --clear"
	}
}

// refuseHumanPersonalKeys is the human-session path's half of that rule: the
// export it uses never carries personal values, so a key the caller holds a
// value or notice for (by hikyo personal list) refuses the run. A checklist
// row for a required key the caller holds nothing for does not: this path
// skips the requirement check, so the shared value flows. Only a server older
// than the personal-values revision holds none; any other lookup failure fails.
func refuseHumanPersonalKeys(ctx context.Context, client *Client, project, env string, names []string) error {
	meta, err := client.Meta(ctx)
	if err != nil {
		return err
	}
	ops, err := api.Operations()
	if err != nil {
		return err
	}
	if serves, err := servesPersonalList(meta, ops); err != nil || !serves {
		return err
	}
	var list apigen.PersonalValueList
	if err := client.Do(ctx, http.MethodGet, project+"/environments/"+url.PathEscape(env)+"/personal-values", nil, &list); err != nil {
		return err
	}
	injected := map[string]bool{}
	for _, n := range names {
		injected[n] = true
	}
	var refused []string
	for _, e := range list.Items {
		if injected[e.Name] && (e.Set || e.Notice != nil) {
			refused = append(refused, e.Name)
		}
	}
	if len(refused) > 0 {
		slices.Sort(refused)
		return failf(ExitRefused, "hikyo run --use-human-session: refusing key(s) %s: you hold a personal value or removal notice for them (see hikyo personal list), and only a developer credential (hikyo dev session --personal) delivers personal values",
			strings.Join(refused, ", "))
	}
	return nil
}

// servesPersonalList reports whether the server is new enough to list the
// caller's personal values. A missing contract entry is an error, never "old".
func servesPersonalList(meta apigen.Meta, ops map[string]api.Operation) (bool, error) {
	op, ok := ops["listMyPersonalValues"]
	if !ok {
		return false, failf(ExitInternal, "this client has no contract entry for %q", "listMyPersonalValues")
	}
	return meta.ApiRevision >= op.MinRevision, nil
}
