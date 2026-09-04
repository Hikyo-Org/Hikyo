package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func runComposeRender(ctx context.Context, ios IO, args []string) (bool, error) {
	var (
		configOnly bool
		projectDir string
	)
	st, flags, err := parseCommon("compose render", ios, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&configOnly, "config-only", false, "request the config-only projection: no secrets")
		fs.StringVar(&projectDir, "project-directory", "", "directory to look up hikyo-compose.yaml from (walks up)")
	})
	if err != nil {
		return false, err
	}
	if err := flags.checkNoPositionals("compose render"); err != nil {
		return false, err
	}
	moved, _, err := composeRenderCore(ctx, ios, st, flags, projectDir, configOnly)
	return moved, err
}

// composeRenderCore is the render pipeline, shared by `compose render` and the
// render step of `compose sync`. It returns whether any target's stamp moved
// (so sync knows whether to recreate) and the opened stack.
func composeRenderCore(ctx context.Context, ios IO, st *State, flags commonFlags, projectDir string, configOnly bool) (bool, *composeStack, error) {
	stack, err := openComposeStack(st, ios, flags, composeStackOptions{
		projectDir: projectDir, configOnly: configOnly, requireConfig: true,
	})
	if err != nil {
		return false, nil, err
	}
	snapshotBinding, err := stack.newSnapshotBinding(stack.cfg.TargetNames())
	if err != nil {
		return false, stack, failf(ExitRefused, "compose render: snapshot binding: %v", err)
	}
	if stack.runtimeErr != nil {
		return false, stack, stack.runtimeErr
	}
	// The DEFAULT runtime dir MUST be tmpfs-backed or render refuses (compose ADR
	// § Where plaintext lives; ops-spec § 6). An EXPLICIT runtime_dir is the
	// operator's accepted disposition (doctor reports `runtime_not_tmpfs` but the
	// renderer does not block) — the orchestrator's binding call for finding 2.
	if !stack.explicitRuntime {
		if err := os.MkdirAll(stack.runtimeDir, 0o700); err != nil {
			return false, stack, failf(ExitInternal, "compose render: create runtime dir: %v", err)
		}
		if ok, terr := compose.IsTmpfs(stack.runtimeDir); terr != nil {
			return false, stack, failf(ExitInternal, "compose render: checking runtime dir filesystem: %v", terr)
		} else if !ok {
			return false, stack, failf(ExitRefused, "compose render: default runtime dir %s is not backed by tmpfs; rendered plaintext must live only on tmpfs — set an explicit `runtime_dir` on tmpfs in %s", stack.runtimeDir, composeConfigName)
		}
	}
	keys, err := loadLocalKeys(stack.stateDir)
	if err != nil {
		return false, stack, err
	}

	w := compose.NewWriter(stack.stateDir, nil)
	lock, err := w.BeginRender(stack.cfgDir)
	if err != nil {
		return false, stack, failf(ExitRefused, "another hikyo compose process holds the lock for %s", stack.slug)
	}
	defer lock.Close()

	// 1. Recover incomplete (torn) generations before anything reads them.
	if err := lock.Recover(stack.runtimeDir); err != nil {
		return false, stack, failf(ExitInternal, "compose render: recover: %v", err)
	}
	// 2. Flush-before-fetch.
	if err := stack.flushOffline(ctx); err != nil {
		return false, stack, err
	}
	// 3. Cursor: present it only when the full local eligibility test holds.
	currentStamps, err := compose.CurrentStamps(stack.cfgDir)
	if err != nil {
		return false, stack, failf(ExitRefused, "compose render: %v", err)
	}
	present := stack.eligibleCursor(currentStamps)

	// The acknowledgement in force for this render is the UNION of every target's
	// acknowledge_loader_control (#64 audit field). The server records it and
	// filters nothing; per-target refusal below stays client-side authoritative.
	resp, ferr := stack.fetchDelivery(ctx, renderAcknowledged(stack.cfg), present)
	if ferr != nil {
		moved, err := stack.renderOffline(ctx, ios, lock, keys, snapshotBinding, ferr)
		return moved, stack, err
	}
	if resp.Current {
		for _, t := range stack.cfg.TargetNames() {
			fmt.Fprintf(ios.Stderr, "up to date (generation %s)\n", currentStamps[t])
		}
		return false, stack, nil
	}
	snapshotBinding, err = bindSnapshotDelivery(snapshotBinding, resp)
	if err != nil {
		return false, stack, failf(ExitRefused, "compose render: snapshot binding: %v", err)
	}
	moved, err := stack.renderApply(ios, lock, keys, snapshotBinding, resp, currentStamps)
	return moved, stack, err
}

// renderApply renders each target from a live full delivery. On ANY
// refusal it writes no generation and does not advance the cursor.
func (s *composeStack) renderApply(ios IO, lock *compose.RenderLock, keys *crypto.LocalKeys, binding crypto.SnapshotBinding, resp apigen.DeliveryResponse, currentStamps map[string]string) (bool, error) {
	if _, err := binding.CanonicalAAD(); err != nil {
		return false, failf(ExitRefused, "compose render: snapshot binding: %v", err)
	}
	plan, err := compose.BuildRenderPlan(liveRenderInput(s.cfg, s.configOnly, resp.Keys))
	if err != nil {
		return false, failf(ExitInternal, "compose render: %v", err)
	}
	if refusals := renderRefusalMessages(plan.Refusals); len(refusals) > 0 {
		return false, failf(ExitRefused, "hikyo compose render refused; no generation written, cursor not advanced:\n  %s", strings.Join(refusals, "\n  "))
	}

	// Publish owns generation materialization, the single stamp commit, and GC.
	// Snapshot + cursor remain post-publish bookkeeping: they are deliberately
	// not claimed atomic with the filesystem publication.
	var allRows []compose.SnapshotRow
	for _, target := range plan.Targets {
		allRows = append(allRows, target.SnapshotRows...)
	}
	published, err := publishRenderPlan(lock, s.runtimeDir, keys, plan)
	if err != nil {
		if pendingErr := persistCommittedPublish(s.stateDir, s.runtimeDir, plan, currentStamps, published); pendingErr != nil {
			return false, failf(ExitInternal, "compose render: publish: %v", errors.Join(err, fmt.Errorf("persist apply-pending: %w", pendingErr)))
		}
		return false, failf(ExitInternal, "compose render: publish: %v", err)
	}
	finalStamps := published.Stamps
	moved, lines := publishOutcome(s.runtimeDir, plan, currentStamps, published)
	if err := persistCommittedPublish(s.stateDir, s.runtimeDir, plan, currentStamps, published); err != nil {
		return false, failf(ExitInternal, "compose render: persist apply-pending: %v", err)
	}

	// Snapshot BEFORE cursor: a snapshot is a harmless cache, but a cursor saved
	// without a snapshot could read "current" after a reboot with nothing to
	// serve. If the cursor save fails the snapshot still stands and the next
	// render does a full fetch.
	if err := saveSnapshot(keys, binding, compose.SnapshotPayload{Rows: allRows, GenerationStamps: finalStamps}); err != nil {
		return false, failf(ExitInternal, "compose render: save snapshot: %v", err)
	}
	if err := s.saveCursor(resp, finalStamps); err != nil {
		return false, failf(ExitInternal, "compose render: save cursor: %v", err)
	}

	for _, l := range lines {
		fmt.Fprintln(ios.Stderr, l)
	}
	return moved, nil
}

// renderOffline renders each target from the last snapshot when the
// server is unreachable and the stack opted in. Row→key_id now comes from the
// sealed payload's rows (finding 3: no cleartext sidecar).
func (s *composeStack) renderOffline(ctx context.Context, ios IO, lock *compose.RenderLock, keys *crypto.LocalKeys, binding crypto.SnapshotBinding, fetchErr error) (bool, error) {
	_ = ctx
	if !isUnavailable(fetchErr) {
		return false, fetchErr
	}
	if !s.cfg.Snapshot.OfflineServe {
		fmt.Fprintln(ios.Stderr, "hikyo compose render: offline serve is not enabled for this stack; set snapshot.offline_serve: true to render from the last snapshot during an outage")
		return false, fetchErr
	}
	payload, binding, err := loadOfflineSnapshot(ios, s.cfg, binding)
	if err != nil {
		return false, err
	}
	aad, err := binding.AAD()
	if err != nil {
		return false, failf(ExitInternal, "compose render: reading offline snapshot binding: %v", err)
	}
	plan, err := compose.BuildRenderPlan(offlineRenderInput(s.cfg, s.configOnly, payload.Rows))
	if err != nil {
		return false, failf(ExitInternal, "compose render: %v", err)
	}
	if refusals := renderRefusalMessages(plan.Refusals); len(refusals) > 0 {
		return false, failf(ExitRefused, "hikyo compose render (offline) refused; no generation written:\n  %s", strings.Join(refusals, "\n  "))
	}

	// One offline record per served key, fsynced BEFORE Publish can materialize
	// any generation. This audit ordering remains outside filesystem publication.
	var records []compose.OfflineRecord
	stamps := make(map[string]string, len(plan.Targets))
	for _, target := range plan.Targets {
		// The stamp is bound to the target name and must match the value
		// WriteGeneration computes after the disclosure record is durable.
		stamp := compose.TargetStamp(keys, target.Name, target.Content)
		stamps[target.Name] = stamp
		for _, row := range target.SnapshotRows {
			id, err := compose.NewRecordID()
			if err != nil {
				return false, failf(ExitInternal, "compose render: record id: %v", err)
			}
			records = append(records, compose.OfflineRecord{
				RecordID: id, KeyID: row.KeyID, KeyName: row.Name, Classification: row.Classification,
				OccurredAt: ios.now().UTC().Format(time.RFC3339), CredentialID: aad.CredentialID,
				Generation: stamp, ServedFrom: aad.IssuedAt,
			})
		}
	}
	if err := compose.Append(s.stateDir, records); err != nil {
		return false, failf(ExitInternal, "compose render: recording offline disclosure: %v", err)
	}

	currentStamps, err := compose.CurrentStamps(s.cfgDir)
	if err != nil {
		return false, failf(ExitRefused, "compose render: %v", err)
	}
	published, err := publishRenderPlan(lock, s.runtimeDir, keys, plan)
	if err != nil {
		if pendingErr := persistCommittedPublish(s.stateDir, s.runtimeDir, plan, currentStamps, published); pendingErr != nil {
			return false, failf(ExitInternal, "compose render: publish: %v", errors.Join(err, fmt.Errorf("persist apply-pending: %w", pendingErr)))
		}
		return false, failf(ExitInternal, "compose render: publish: %v", err)
	}
	for _, target := range plan.Targets {
		stamp := published.Stamps[target.Name]
		if stamp != stamps[target.Name] {
			return false, failf(ExitInternal, "compose render: target %s stamp changed between offline record and publish", target.Name)
		}
		fmt.Fprintf(ios.Stderr, "serving stale from %s, generation %s\n", aad.IssuedAt, stamp)
	}
	moved, lines := publishOutcome(s.runtimeDir, plan, currentStamps, published)
	if err := persistCommittedPublish(s.stateDir, s.runtimeDir, plan, currentStamps, published); err != nil {
		return false, failf(ExitInternal, "compose render: persist apply-pending: %v", err)
	}
	for _, l := range lines {
		fmt.Fprintln(ios.Stderr, l)
	}
	return moved, nil
}

// publishRenderPlan adapts the pure render plan to the one filesystem-owner
// operation shared by live and offline flows.
func publishRenderPlan(lock *compose.RenderLock, runtimeDir string, keys *crypto.LocalKeys, plan compose.RenderPlan) (compose.PublishResult, error) {
	targets := make(map[string][]byte, len(plan.Targets))
	for _, target := range plan.Targets {
		targets[target.Name] = target.Content
	}
	return lock.Publish(compose.PublishPlan{RuntimeDir: runtimeDir, Keys: keys, Targets: targets})
}

func publishOutcome(runtimeDir string, plan compose.RenderPlan, currentStamps map[string]string, published compose.PublishResult) (bool, []string) {
	var lines []string
	moved := false
	for _, target := range plan.Targets {
		stamp := published.Stamps[target.Name]
		// A changed stamp or re-materialized tmpfs generation both require sync to
		// re-apply the target (R1-10).
		if currentStamps[target.Name] != stamp || published.Materialized[target.Name] {
			moved = true
			lines = append(lines, fmt.Sprintf("rendered %s generation %s → %s", target.Name, stamp, filepath.Join(runtimeDir, stamp, target.Name+".env")))
		} else {
			lines = append(lines, fmt.Sprintf("unchanged %s generation %s", target.Name, stamp))
		}
	}
	return moved, lines
}

// persistCommittedPublish closes the crash/error window between a stamp switch
// and sync's Docker apply. Render writes the marker too: a successful render is
// filesystem-visible but remains unapplied until a later sync clears it.
func persistCommittedPublish(stateDir, runtimeDir string, plan compose.RenderPlan, currentStamps map[string]string, published compose.PublishResult) error {
	if !published.CandidateActive() {
		return nil
	}
	moved, _ := publishOutcome(runtimeDir, plan, currentStamps, published)
	if !moved {
		return nil
	}
	return writeApplyPending(stateDir, published.Stamps)
}

func liveRenderInput(cfg *compose.Config, configOnly bool, keys []apigen.DeliveredKey) compose.RenderInput {
	rows := make([]compose.RenderSourceRow, 0, len(keys))
	for _, key := range keys {
		row := compose.RenderSourceRow{
			KeyID: key.KeyId, Name: key.Name, Classification: string(key.Classification),
		}
		switch {
		case !configOnly && isUnrevealedSecret(key):
			row.State = compose.RenderRowUnrevealedSecret
		case key.Value == nil:
			row.State = compose.RenderRowNoValue
		default:
			row.State = compose.RenderRowValued
			row.Value = *key.Value
		}
		rows = append(rows, row)
	}
	return renderInput(cfg, configOnly, compose.AbsentKeyRefuseNotDelivered, rows)
}

func offlineRenderInput(cfg *compose.Config, configOnly bool, rows []compose.SnapshotRow) compose.RenderInput {
	sourceRows := make([]compose.RenderSourceRow, 0, len(rows))
	for _, row := range rows {
		sourceRows = append(sourceRows, compose.RenderSourceRow{
			KeyID: row.KeyID, Name: row.Name, Classification: row.Classification,
			State: compose.RenderRowValued, Value: row.Value,
		})
	}
	return renderInput(cfg, configOnly, compose.AbsentKeyRefuseNotInSnapshot, sourceRows)
}

func renderInput(cfg *compose.Config, configOnly bool, fullProjectionPolicy compose.AbsentKeyPolicy, rows []compose.RenderSourceRow) compose.RenderInput {
	absentKeys := fullProjectionPolicy
	if configOnly {
		absentKeys = compose.AbsentKeySkip
	}
	targets := make([]compose.RenderTarget, 0, len(cfg.Targets))
	for _, name := range cfg.TargetNames() {
		target := cfg.Targets[name]
		targets = append(targets, compose.RenderTarget{
			Name: name, KeyIDs: append([]string(nil), target.Keys...),
			AcknowledgeLoaderControl: append([]string(nil), target.AcknowledgeLoaderControl...),
		})
	}
	return compose.RenderInput{AbsentKeys: absentKeys, Targets: targets, Rows: rows}
}

func renderRefusalMessages(refusals []compose.RenderRefusal) []string {
	messages := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		var detail string
		switch refusal.Kind {
		case compose.RenderRefusalKeyNotDelivered:
			detail = "key id was not delivered by the server"
		case compose.RenderRefusalKeyNotInSnapshot:
			detail = "not present in the last snapshot"
		case compose.RenderRefusalSecretUnrevealed:
			detail = "secret has no value — " + machineRevealOptIn
		case compose.RenderRefusalLoaderControl:
			detail = "loader-control key not acknowledged (add it to this target's acknowledge_loader_control)"
		case compose.RenderRefusalEncoding:
			detail = refusal.Reason
		default:
			detail = fmt.Sprintf("unknown render refusal %q", refusal.Kind)
		}
		messages = append(messages, fmt.Sprintf("%s: %s: %s", refusal.Target, refusal.Key, detail))
	}
	slices.Sort(messages)
	return messages
}
