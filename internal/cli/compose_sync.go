package cli

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"

	"github.com/Hikyo-Org/hikyo/internal/compose"
)

// ---------------------------------------------------------------------------
// hikyo compose sync
// ---------------------------------------------------------------------------

func runComposeSync(ctx context.Context, ios IO, args []string) error {
	var projectDir string
	st, flags, err := parseCommon("compose sync", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&projectDir, "project-directory", "", "directory to look up hikyo-compose.yaml from (walks up)")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("compose sync"); err != nil {
		return err
	}

	// (1) Doctor checks run BEFORE the first render; any BLOCKING error finding
	// refuses without rendering. Findings go to stderr — stdout stays empty. The
	// server-agreement family (`server_manifest_drift`, `never_rendered`,
	// `server_stamp_unknown`, `server_unreachable`) is DELIBERATELY EXCLUDED from
	// the gate: those describe exactly the staleness this sync is about to repair,
	// so gating on them would brick sync on every publish and every fresh box.
	// Sync's gate is the LOCAL integrity checks (version floor, format raw, stamp
	// grammar, token/state modes); the drift stays an error for the doctor VERB,
	// which reports rather than repairs (finding 11 — this is the interpretation
	// of the ADR "same checks" sentence offered for human disposition; see the
	// spellings § Compose delivery).
	findings, err := composeDoctorGather(ctx, ios, st, flags, projectDir, false)
	if err != nil {
		return err
	}
	if hasBlockingError(findings) {
		if rerr := renderComposeFindings(ios.Stderr, FormatTable, findings); rerr != nil {
			return failf(ExitInternal, "hikyo compose sync: rendering findings: %v", rerr)
		}
		return failf(ExitRefused, "hikyo compose sync: doctor found errors; not rendering")
	}

	// (2) Render (conditional).
	moved, stack, err := composeRenderCore(ctx, ios, st, flags, projectDir, false)
	if err != nil {
		return err
	}

	// (3) Apply through `docker compose up -d` when a stamp moved, a prior sync
	// left an apply-pending marker, OR active stamps differ from the durable
	// last-applied record. The last comparison closes the crash window after the
	// stamp rename but before a marker write: no active generation can become
	// permanently unapplied merely because the process died at that boundary.
	pending := applyPendingExists(stack.stateDir)
	stamps, err := compose.CurrentStamps(stack.cfgDir)
	if err != nil {
		return failf(ExitRefused, "hikyo compose sync: %v", err)
	}
	applied, err := loadAppliedStamps(stack.stateDir)
	if err != nil {
		return failf(ExitRefused, "hikyo compose sync: %v", err)
	}
	if !moved && !pending && !stampsNeedApply(stamps, applied) {
		return nil
	}
	if err := writeApplyPending(stack.stateDir, stamps); err != nil {
		return failf(ExitInternal, "hikyo compose sync: writing apply-pending marker: %v", err)
	}
	if err := dockerComposeUp(ctx, ios, stack.cfgDir); err != nil {
		return err // marker stays: the next sync retries the apply
	}
	if err := writeAppliedStamps(stack.stateDir, stamps); err != nil {
		return failf(ExitInternal, "hikyo compose sync: recording applied stamps: %v", err)
	}
	if err := removeApplyPending(stack.stateDir); err != nil {
		return failf(ExitInternal, "hikyo compose sync: clearing apply-pending marker: %v", err)
	}
	return nil
}

func dockerComposeUp(ctx context.Context, ios IO, projectDir string) error {
	bin, found := dockerBinary(ios)
	if !found {
		return failf(ExitRefused, "hikyo compose sync: docker not found on PATH; install Docker Compose or set HIKYO_COMPOSE_DOCKER")
	}
	cmd := exec.CommandContext(ctx, bin, "compose", "up", "-d")
	cmd.Dir = projectDir
	cmd.Stdin = os.Stdin
	// `compose sync` is a delivery operation: nothing is printed on hikyo stdout
	// (output ADR). Docker's own stdout is diagnostic, so it is routed to hikyo
	// STDERR, keeping sync stdout empty (finding 14).
	cmd.Stdout = ios.Stderr
	cmd.Stderr = ios.Stderr
	// The workload credential never reaches a subprocess (finding 1).
	cmd.Env = sanitizedEnviron()
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return failf(ExitInternal, "hikyo compose sync: `docker compose up -d` exited %d", ee.ExitCode())
		}
		return failf(ExitInternal, "hikyo compose sync: `docker compose up -d`: %v", err)
	}
	return nil
}

// syncRepairableCodes is the family sync's pre-render gate excludes: these are
// the findings sync's own render REPAIRS, not the local integrity it must not
// proceed past (finding 11). Two groups: the server-agreement freshness family
// (the staleness sync exists to reconcile), and the runtime-generation family
// (`generation_absent`/`generation_incomplete` — a wiped or torn tmpfs copy the
// render re-materialises, R1-10). Gating on either would brick sync on exactly
// the state it is invoked to fix (every publish, every fresh box, every reboot
// that lost the tmpfs).
var syncRepairableCodes = map[string]bool{
	"server_manifest_drift": true,
	"never_rendered":        true,
	"server_stamp_unknown":  true,
	"server_unreachable":    true,
	"generation_absent":     true,
	"generation_incomplete": true,
}

// hasBlockingError reports whether any error finding OUTSIDE the sync-repairable
// family is present — the set sync gates on before rendering.
func hasBlockingError(findings []compose.Finding) bool {
	for _, f := range findings {
		if f.Severity == compose.SeverityError && !syncRepairableCodes[f.Code] {
			return true
		}
	}
	return false
}
