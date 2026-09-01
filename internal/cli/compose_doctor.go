package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
)

// ---------------------------------------------------------------------------
// hikyo compose doctor
// ---------------------------------------------------------------------------

func runComposeDoctor(ctx context.Context, ios IO, args []string) error {
	var (
		format     string
		projectDir string
	)
	st, flags, err := parseCommon("compose doctor", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		fs.StringVar(&projectDir, "project-directory", "", "directory to look up hikyo-compose.yaml from (walks up)")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("compose doctor"); err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	findings, err := composeDoctorGather(ctx, ios, st, flags, projectDir, true)
	if err != nil {
		return err
	}
	// Propagate a rendering failure as ExitInternal (finding 15): doctor must not
	// return success or its findings-derived refusal when its required output
	// never reached the caller.
	if rerr := renderComposeFindings(ios.Stdout, f, findings); rerr != nil {
		return failf(ExitInternal, "hikyo compose doctor: rendering findings: %v", rerr)
	}
	if hasErrorFinding(findings) {
		return failf(ExitRefused, "hikyo compose doctor found errors")
	}
	return nil
}

// composeDoctorGather assembles every input compose.Doctor needs — docker
// version/config, the raw compose file, managed stamps, generation state, file
// modes, and server agreement via a conditional fetch — and returns the merged
// finding list.
func composeDoctorGather(ctx context.Context, ios IO, st *State, flags commonFlags, projectDir string, includeServerAgreement bool) ([]compose.Finding, error) {
	stack, err := openComposeStack(st, ios, flags, composeStackOptions{projectDir: projectDir, requireConfig: true})
	if err != nil {
		return nil, err
	}

	// Flush-before-fetch (ops-spec § 6): reconcile pending offline records BEFORE
	// any doctor network request (the catalogue and agreement fetches), so a POST
	// always precedes every GET (finding 9). A flush failure is a hard error.
	if err := stack.flushOffline(ctx); err != nil {
		return nil, err
	}

	var findings []compose.Finding

	// Docker version + resolved config.
	dockerFindings, version, resolvedConfig := doctorDocker(ctx, ios, stack.cfgDir)
	findings = append(findings, dockerFindings...)

	managed, err := compose.CurrentStamps(stack.cfgDir)
	if err != nil {
		return nil, failf(ExitRefused, "compose doctor: %v", err)
	}

	existingKeyIDs, catFinding := doctorExistingKeyIDs(ctx, stack.client, stack.org, stack.project, stack.cfg)
	if catFinding != nil {
		findings = append(findings, *catFinding)
	}
	stateEntries, scanFinding := doctorStateEntries(stack.stateDir)
	if scanFinding != nil {
		findings = append(findings, *scanFinding)
	}

	in := compose.DoctorInput{
		ComposeVersion: version,
		Config:         resolvedConfig,
		RawComposeYAML: doctorRawCompose(ios, stack.cfgDir),
		ManagedStamps:  managed,
		ConfigTargets:  stack.cfg.Targets,
		ExistingKeyIDs: existingKeyIDs,
		StateEntries:   stateEntries,
		TokenFile:      doctorTokenFile(flags.TokenFile),

		SystemdInvocation:       ios.Env.Getenv("INVOCATION_ID") != "",
		TokenFromCredentialsDir: tokenFromCredentialsDir(ios, flags.TokenFile),
	}
	// runtime_dir must resolve; when it cannot (not root, no XDG_RUNTIME_DIR, no
	// explicit config), surface it as its own error and do not let the derived
	// runtime checks fire on an empty path (finding 12).
	runtimeResolved := stack.runtimeErr == nil
	if runtimeResolved {
		in.RuntimeDir = stack.runtimeDir
		in.RuntimeTmpfs = doctorRuntimeTmpfs(stack.runtimeDir)
	}

	// Server agreement: feed compose.Doctor the per-target server stamps it needs
	// (finding 4). The structural check then compares them against the managed
	// stamp and the label. Only the DOCTOR verb reaches the server; sync repairs
	// freshness and must not gate on it (finding 11).
	var serverStamps map[string]string
	var serverFindings []compose.Finding
	haveServerStamps := false
	if includeServerAgreement {
		serverStamps, serverFindings, haveServerStamps = stack.doctorServerStamps(ctx, managed)
		in.ServerStamps = serverStamps
	}

	findings = append(findings, compose.Doctor(in)...)
	findings = append(findings, serverFindings...)

	// A docker_missing finding makes the version check redundant; drop the
	// compose.Doctor floor finding so the two do not both fire for one cause.
	if hasCode(findings, "docker_missing") {
		findings = dropCode(findings, "compose_version_below_floor")
	}
	if !runtimeResolved {
		findings = dropCode(findings, "runtime_not_tmpfs")
		findings = dropCode(findings, "runtime_dir_not_absolute")
		findings = append(findings, compose.Finding{Severity: compose.SeverityError, Code: "runtime_dir_unresolved",
			Message: fmt.Sprintf("could not resolve a runtime dir: %v", stack.runtimeErr)})
	}
	// server_stamp_unknown is compose.Doctor's honest "no server stamp to compare"
	// finding. Drop it whenever we deliberately did not (or could not) obtain the
	// server stamps — sync never fetches, and doctor's never_rendered /
	// server_unreachable / drift cases explain the gap directly.
	if !haveServerStamps {
		findings = dropCode(findings, "server_stamp_unknown")
	}

	sortFindings(findings)
	return findings, nil
}

// doctorDocker runs `docker compose version --short` and `docker compose config
// --format json`. A missing docker binary is docker_missing; a config invocation
// or JSON parse failure is docker_config_failed (fail closed — a nil config used
// to silently disable the service checks, finding 12).
func doctorDocker(ctx context.Context, ios IO, cfgDir string) ([]compose.Finding, string, *compose.ComposeConfig) {
	bin, found := dockerBinary(ios)
	if !found {
		return []compose.Finding{{Severity: compose.SeverityError, Code: "docker_missing",
			Message: "docker not found on PATH and HIKYO_COMPOSE_DOCKER is unset; path 2 needs Docker Compose ≥ 2.30"}}, "", nil
	}
	version, err := runCapture(ctx, ios, bin, cfgDir, "compose", "version", "--short")
	if err != nil {
		return []compose.Finding{{Severity: compose.SeverityError, Code: "docker_missing",
			Message: fmt.Sprintf("`docker compose version` failed: %v", err)}}, "", nil
	}
	raw, cerr := runCapture(ctx, ios, bin, cfgDir, "compose", "config", "--format", "json")
	if cerr != nil {
		return []compose.Finding{{Severity: compose.SeverityError, Code: "docker_config_failed",
			Message: fmt.Sprintf("`docker compose config` failed: %v", cerr)}}, strings.TrimSpace(version), nil
	}
	parsed, perr := compose.ParseComposeConfig([]byte(raw))
	if perr != nil {
		return []compose.Finding{{Severity: compose.SeverityError, Code: "docker_config_failed",
			Message: fmt.Sprintf("could not parse `docker compose config` JSON: %v", perr)}}, strings.TrimSpace(version), nil
	}
	return nil, strings.TrimSpace(version), parsed
}

// doctorServerStamps performs the conditional agreement fetch and returns the
// per-target server stamps to feed compose.Doctor's structural check, plus any
// standalone findings. The bool reports whether server stamps were obtained (so
// the caller can drop the honest server_stamp_unknown when they were not).
func (s *composeStack) doctorServerStamps(ctx context.Context, managed map[string]string) (map[string]string, []compose.Finding, bool) {
	present := s.eligibleCursor(managed)
	if present == "" {
		// No eligible cursor: never rendered, or the local render is gone. A full
		// fetch would be a disclosure, so doctor does not do one.
		return nil, []compose.Finding{{Severity: compose.SeverityError, Code: "never_rendered",
			Message: "no eligible cursor: this box has not rendered, or its render is gone; run `hikyo compose render`"}}, false
	}
	resp, err := s.fetchDelivery(ctx, renderAcknowledged(s.cfg), present)
	if err != nil {
		sev := compose.SeverityError
		msg := fmt.Sprintf("the server refused the agreement check: %v", err)
		if isUnavailable(err) {
			sev = compose.SeverityWarn
			msg = fmt.Sprintf("could not reach the server to confirm agreement: %v", err)
		}
		return nil, []compose.Finding{{Severity: sev, Code: "server_unreachable", Message: msg}}, false
	}
	if resp.Current {
		// The server agrees the presented cursor is current: our managed stamps ARE
		// the server's, no plaintext crossed the wire.
		return managed, nil, true
	}
	// Drift: the cursor is not current. We cannot always recompute the server's
	// per-target stamp (a read-only credential holds no secret plaintext), so
	// report the drift directly and leave the stamps unknown.
	return nil, []compose.Finding{{Severity: compose.SeverityError, Code: "server_manifest_drift",
		Message: "the server's current manifest is not the one this box rendered — run `hikyo compose render`"}}, false
}

// doctorRuntimeTmpfs reports whether the runtime dir (or its nearest existing
// ancestor, since the dir may not exist yet) is tmpfs-backed. On non-Linux
// IsTmpfs returns true, so this never falsely flags there.
func doctorRuntimeTmpfs(dir string) bool {
	for dir != "" {
		if ok, err := compose.IsTmpfs(dir); err == nil {
			return ok
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return true
}

// ---------------------------------------------------------------------------
// doctor input gathering
// ---------------------------------------------------------------------------

// doctorRawCompose reads the raw compose file so ${HIKYO_GEN_*:?} is visible
// (the resolved config discards the required form). COMPOSE_FILE wins when set;
// otherwise the first conventional name found in the project dir.
func doctorRawCompose(ios IO, cfgDir string) string {
	if cf := ios.Env.Getenv("COMPOSE_FILE"); cf != "" {
		path := cf
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfgDir, path)
		}
		if b, err := os.ReadFile(path); err == nil {
			return string(b)
		}
		return ""
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if b, err := os.ReadFile(filepath.Join(cfgDir, name)); err == nil {
			return string(b)
		}
	}
	return ""
}

// doctorExistingKeyIDs reads the project key catalogue for the target_key_missing
// check. A workload credential deliberately CANNOT enumerate the catalogue (it
// reads values through delivery, not the catalogue), so a UNIFORM not-found /
// unauthorized there (unauthorized ≡ nonexistent) is not a drift signal — it
// means the check is not answerable from this credential, and the configured ids
// are treated as existing (a no-op). Any OTHER failure — transport, 5xx, decode
// — is NOT swallowed: it fails closed as `catalogue_unavailable` (finding 12),
// while still treating the ids as existing so target_key_missing does not fire
// on an unknown set.
func doctorExistingKeyIDs(ctx context.Context, client *Client, org, project string, cfg *compose.Config) (map[string]bool, *compose.Finding) {
	allExist := func() map[string]bool {
		out := map[string]bool{}
		for _, id := range allTargetKeyIDs(cfg) {
			out[id] = true
		}
		return out
	}
	var list apigen.KeyList
	path := api.PathPrefix + "/orgs/" + url.PathEscape(org) + "/projects/" + url.PathEscape(project) + "/keys"
	if err := client.Do(ctx, http.MethodGet, path, nil, &list); err != nil {
		if isNotFound(err) {
			// Uniform 404/unauthorized: unanswerable from this credential, no-op.
			return allExist(), nil
		}
		return allExist(), &compose.Finding{Severity: compose.SeverityError, Code: "catalogue_unavailable",
			Message: fmt.Sprintf("could not read the key catalogue to confirm target key ids: %v", err)}
	}
	out := make(map[string]bool, len(list.Items))
	for _, k := range list.Items {
		out[k.Id] = true
	}
	return out, nil
}

// doctorStateEntries walks the client state dir for mode/ownership checks. A
// walk or stat error is NOT swallowed: it fails closed as `state_scan_failed`
// (finding 12) rather than silently reporting an incomplete tree.
func doctorStateEntries(stateDir string) ([]compose.StateEntry, *compose.Finding) {
	var entries []compose.StateEntry
	var scanErr error
	_ = filepath.WalkDir(stateDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A missing state dir is legitimate (nothing rendered yet); any other
			// walk error is a real scan failure.
			if errors.Is(err, os.ErrNotExist) && path == stateDir {
				return nil
			}
			scanErr = fmt.Errorf("%s: %w", path, err)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			scanErr = fmt.Errorf("%s: %w", path, ierr)
			return nil
		}
		entries = append(entries, compose.StateEntry{
			Path: path, Perm: info.Mode(), IsDir: d.IsDir(), OwnedByEUID: ownedByEUID(info),
		})
		return nil
	})
	if scanErr != nil {
		return entries, &compose.Finding{Severity: compose.SeverityError, Code: "state_scan_failed",
			Message: fmt.Sprintf("could not fully scan the client state dir: %v", scanErr)}
	}
	return entries, nil
}

func doctorTokenFile(tokenFile string) *compose.FileMode {
	if tokenFile == "" {
		return nil
	}
	info, err := os.Stat(tokenFile)
	if err != nil {
		return nil
	}
	return &compose.FileMode{Perm: info.Mode(), OwnedByEUID: ownedByEUID(info)}
}

func tokenFromCredentialsDir(ios IO, tokenFile string) bool {
	dir := ios.Env.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" || tokenFile == "" {
		return false
	}
	abs, err := filepath.Abs(tokenFile)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// ---------------------------------------------------------------------------
// doctor rendering
// ---------------------------------------------------------------------------

type composeFinding struct {
	Status  string `json:"status"`
	Check   string `json:"check"`
	Message string `json:"message"`
}

type composeDoctorReport struct {
	Status   string           `json:"status"`
	Findings []composeFinding `json:"findings"`
}

func renderComposeFindings(w io.Writer, f Format, findings []compose.Finding) error {
	report := composeDoctorReport{Status: "ok", Findings: []composeFinding{}}
	rows := make([][]string, 0, len(findings))
	for _, fd := range findings {
		report.Findings = append(report.Findings, composeFinding{Status: string(fd.Severity), Check: fd.Code, Message: fd.Message})
		rows = append(rows, []string{string(fd.Severity), fd.Code, fd.Message})
		switch fd.Severity {
		case compose.SeverityError:
			report.Status = "error"
		case compose.SeverityWarn:
			if report.Status == "ok" {
				report.Status = "warning"
			}
		}
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"ok", "compose", "no findings"})
	}
	return Render(w, f, Table{Columns: []string{"STATUS", "CHECK", "MESSAGE"}, Rows: rows, JSON: report})
}

// ---------------------------------------------------------------------------
// docker seam + capture
// ---------------------------------------------------------------------------

// dockerBinary resolves the docker executable. HIKYO_COMPOSE_DOCKER overrides
// PATH resolution — the test seam, documented in the handoff/package doc only,
// deliberately kept out of the help text.
func dockerBinary(ios IO) (string, bool) {
	if v := ios.Env.Getenv("HIKYO_COMPOSE_DOCKER"); v != "" {
		return v, true
	}
	bin, err := exec.LookPath("docker")
	if err != nil {
		return "", false
	}
	return bin, true
}

func runCapture(ctx context.Context, ios IO, bin, dir string, args ...string) (string, error) {
	_ = ios
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	// The workload credential never reaches a subprocess (finding 1).
	cmd.Env = sanitizedEnviron()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func hasErrorFinding(findings []compose.Finding) bool {
	return hasSeverity(findings, compose.SeverityError)
}

// isNotFound reports the uniform not-found/unauthorized response (unauthorized ≡
// nonexistent), the only catalogue answer doctor treats as "unanswerable".
func isNotFound(err error) bool {
	var ce *Error
	return asCLIError(err, &ce) && (ce.Code == ExitNotFound || ce.Code == ExitAuth)
}

func hasSeverity(findings []compose.Finding, sev compose.Severity) bool {
	for _, f := range findings {
		if f.Severity == sev {
			return true
		}
	}
	return false
}

func hasCode(findings []compose.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func dropCode(findings []compose.Finding, code string) []compose.Finding {
	out := findings[:0]
	for _, f := range findings {
		if f.Code != code {
			out = append(out, f)
		}
	}
	return out
}

func sortFindings(findings []compose.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Message < findings[j].Message
	})
}
