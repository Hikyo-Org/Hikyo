package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/filesync"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

// `hikyo file-sync render|doctor --config FILE` (#164): the client half of a
// generic file destination. The client authenticates with the one workload
// credential bound to the file target, pulls one revision through
// `delivery?target=<id>`, renders every configured file and publishes them as
// one generation into the locally configured directory. The server never
// learns the path. There is no resident watcher: `render` is one-shot or
// poll-until-current, and continuous refresh is a timer running it.
//
// Local state lives under <state>/file-sync/<target id>/, 0700: the local key
// (stamps and the offline snapshot), the encrypted offline snapshot, pending
// offline disclosure records and cursor.json. Plaintext is written only into
// the configured directory.

const (
	fileSyncCursorFile = "cursor.json"
	// fileSyncSnapshotTarget names the one snapshot "target" of a file-sync
	// state dir; it is outside the Compose target grammar on purpose.
	fileSyncSnapshotTarget = "file-target"
)

// fileSyncSleep is the poll-interval wait, a seam so tests do not sleep.
var fileSyncSleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func runFileSync(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("file-sync", args, "render", "doctor")
	if err != nil {
		return err
	}
	var configPath, format string
	st, flags, err := parseCommon("file-sync "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&configPath, "config", "", "path to the client-local hikyo-file-sync.yaml")
		if sub == "doctor" {
			fs.StringVar(&format, "o", "table", "output format: table or json")
		}
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("file-sync " + sub); err != nil {
		return err
	}
	if configPath == "" {
		return failf(ExitUsage, "hikyo file-sync %s requires --config", sub)
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(ios.Workdir, configPath)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return failf(ExitUsage, "reading %s: %v", configPath, err)
	}
	cfg, err := filesync.ParseConfig(raw)
	if err != nil {
		return failf(ExitRefused, "%s: %v", configPath, err)
	}
	policy, err := filesync.ResolvePolicy(cfg)
	if err != nil {
		return failf(ExitRefused, "%s: %v", configPath, err)
	}
	stateDir := filepath.Join(st.Dir(), "file-sync", cfg.Target)
	if sub == "doctor" {
		f, err := ParseFormat(format)
		if err != nil {
			return err
		}
		return fileSyncDoctor(ios, f, cfg, policy, stateDir)
	}
	mc := &machineConfig{Name: filesync.ConfigName, Instance: cfg.Instance, Org: cfg.Org, Project: cfg.Project, Environment: cfg.Environment}
	client, entry, _, token, err := resolveMachineConfigTarget(st, ios, flags, mc, configPath, "file-sync")
	if err != nil {
		return err
	}
	keys, err := loadLocalKeys(stateDir)
	if err != nil {
		return err
	}
	s := &fileSyncSession{cfg: cfg, policy: policy, client: client, origin: entry.Origin, token: token, stateDir: stateDir, keys: keys}
	if cfg.Refresh.Mode == filesync.RefreshOneshot {
		_, err := s.pass(ctx, ios)
		return err
	}
	deadline := ios.now().Add(cfg.PollTimeout())
	for {
		out, err := s.pass(ctx, ios)
		if err != nil {
			return err
		}
		if out != fileSyncApplied {
			return nil
		}
		if !ios.now().Before(deadline) {
			return failf(ExitUnavailable, "file-sync: the target was still changing after %s; the last applied generation stands", cfg.PollTimeout())
		}
		if err := fileSyncSleep(ctx, cfg.PollInterval()); err != nil {
			return err
		}
	}
}

type fileSyncOutcome int

const (
	fileSyncCurrent fileSyncOutcome = iota
	fileSyncApplied
	fileSyncOffline
)

type fileSyncSession struct {
	cfg      *filesync.Config
	policy   filesync.Policy
	client   *Client
	origin   string
	token    string
	stateDir string
	keys     *crypto.LocalKeys
}

// fileSyncCursor is the local cursor record: presented only when the
// credential, the configuration and the destination all still match what it
// was saved against.
type fileSyncCursor struct {
	Cursor           string `json:"cursor"`
	Credential       string `json:"credential"`
	ConfigDigest     string `json:"config_digest"`
	Stamp            string `json:"stamp"`
	Revision         int64  `json:"revision"`
	TargetGeneration int64  `json:"target_generation"`
	AppliedAt        string `json:"applied_at"`
}

// configDigest binds the cursor to every setting that shapes the rendered
// bytes or where they land.
func (s *fileSyncSession) configDigest() string {
	raw, _ := json.Marshal(struct {
		Target      string
		Environment string
		Destination filesync.DestinationSettings
		Files       []filesync.File
		Policy      filesync.Policy
	}{s.cfg.Target, s.cfg.Environment, s.cfg.Destination, s.cfg.Files, s.policy})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *fileSyncSession) fileNames() []string {
	names := make([]string, 0, len(s.cfg.Files))
	for _, f := range s.cfg.Files {
		names = append(names, f.Name)
	}
	return names
}

func (s *fileSyncSession) loadCursor() *fileSyncCursor {
	raw, err := os.ReadFile(filepath.Join(s.stateDir, fileSyncCursorFile))
	if err != nil {
		return nil
	}
	var c fileSyncCursor
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	return &c
}

func (s *fileSyncSession) saveCursor(c fileSyncCursor) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(filepath.Join(s.stateDir, fileSyncCursorFile), raw, 0o600)
}

// pass is one fetch-render-publish round.
func (s *fileSyncSession) pass(ctx context.Context, ios IO) (fileSyncOutcome, error) {
	dest, err := filesync.OpenDestination(s.cfg.Destination.Directory, s.cfg.Destination.RequireTmpfs)
	if err != nil {
		return 0, failf(ExitRefused, "file-sync: %v", err)
	}
	defer dest.Close()
	if err := flushOfflineRecords(ctx, s.client, s.cfg.Org, s.cfg.Project, s.cfg.Environment, s.stateDir); err != nil {
		// Records from an earlier offline serve stay pending; an outage that
		// refuses their flush is the same outage offline serve exists for.
		return s.offline(ctx, ios, dest, err)
	}
	present := ""
	if stored := s.loadCursor(); stored != nil && stored.Credential == credentialFingerprint(s.token) && stored.ConfigDigest == s.configDigest() {
		if st, ok, err := dest.Intact(s.keys, s.cfg.Target, s.policy, s.fileNames()); err == nil && ok && st.Stamp == stored.Stamp {
			present = stored.Cursor
		}
	}
	resp, ferr := s.fetch(ctx, present)
	if ferr != nil {
		return s.offline(ctx, ios, dest, ferr)
	}
	generation := int64(0)
	if resp.FileTargetGeneration != nil {
		generation = *resp.FileTargetGeneration
	}
	if resp.Current {
		stamp := ""
		if stored := s.loadCursor(); stored != nil {
			stamp = stored.Stamp
		}
		fmt.Fprintf(ios.Stderr, "up to date: revision %d, generation %s\n", resp.Revision, stamp)
		s.report(ctx, ios, "current", resp.Revision, generation, stamp)
		return fileSyncCurrent, nil
	}
	rows := make([]filesync.Row, 0, len(resp.Keys))
	var snapshotRows []compose.SnapshotRow
	for _, k := range resp.Keys {
		rows = append(rows, filesync.Row{KeyID: k.KeyId, Name: k.Name, Classification: string(k.Classification), Value: k.Value})
		if k.Value != nil {
			snapshotRows = append(snapshotRows, compose.SnapshotRow{Name: k.Name, KeyID: k.KeyId, Classification: string(k.Classification), Value: *k.Value})
		}
	}
	res, err := s.publish(ctx, dest, rows)
	if err != nil {
		state := "failed"
		var ce *Error
		if asCLIError(err, &ce) && ce.Code == ExitRefused {
			state = "refused"
		}
		s.report(ctx, ios, state, resp.Revision, generation, "")
		return 0, err
	}
	// Snapshot before cursor, as for Compose: a snapshot is a harmless cache,
	// a cursor without one could read "current" with nothing to serve.
	binding, err := s.snapshotBinding()
	if err != nil {
		return 0, err
	}
	if binding, err = bindSnapshotDelivery(binding, resp); err != nil {
		return 0, failf(ExitRefused, "file-sync: snapshot binding: %v", err)
	}
	if err := saveSnapshot(s.keys, binding, compose.SnapshotPayload{Rows: snapshotRows, GenerationStamps: map[string]string{fileSyncSnapshotTarget: res.Stamp}}); err != nil {
		return 0, failf(ExitInternal, "file-sync: save snapshot: %v", err)
	}
	if err := s.saveCursor(fileSyncCursor{
		Cursor: resp.Cursor, Credential: credentialFingerprint(s.token), ConfigDigest: s.configDigest(),
		Stamp: res.Stamp, Revision: resp.Revision, TargetGeneration: generation,
		AppliedAt: ios.now().UTC().Format(time.RFC3339),
	}); err != nil {
		return 0, failf(ExitInternal, "file-sync: save cursor: %v", err)
	}
	s.printResult(ios, res, fmt.Sprintf("revision %d", resp.Revision))
	s.report(ctx, ios, "applied", resp.Revision, generation, res.Stamp)
	return fileSyncApplied, nil
}

// publish renders every file and commits them as one generation. Every
// refusal is by key name and happens before anything is written.
func (s *fileSyncSession) publish(ctx context.Context, dest *filesync.Destination, rows []filesync.Row) (filesync.Result, error) {
	files, refusals := filesync.RenderAll(s.cfg, rows)
	if len(refusals) > 0 {
		lines := make([]string, 0, len(refusals))
		for _, r := range refusals {
			line := r.String()
			if strings.Contains(r.Reason, "presence-only") {
				line += " (" + machineRevealOptIn + ")"
			}
			lines = append(lines, line)
		}
		return filesync.Result{}, failf(ExitRefused, "hikyo file-sync render refused; nothing written, the last generation stands:\n  %s", strings.Join(lines, "\n  "))
	}
	defer func() {
		for _, f := range files {
			crypto.Zero(f.Content)
		}
	}()
	plan := filesync.Plan{Target: s.cfg.Target, Files: files, Policy: s.policy}
	plan.Stamp = filesync.GenerationStamp(s.keys, s.cfg.Target, s.policy, files)
	res, err := dest.Publish(ctx, plan, nil)
	if err != nil {
		code := ExitInternal
		for _, refusal := range []error{filesync.ErrForeign, filesync.ErrRemoved, filesync.ErrBinding} {
			if errors.Is(err, refusal) {
				code = ExitRefused
			}
		}
		if res.Changed {
			return res, failf(code, "file-sync: generation %s is current, but finishing it failed: %v", res.Generation, err)
		}
		return res, failf(code, "file-sync: nothing changed, the last generation stands: %v", err)
	}
	return res, nil
}

func (s *fileSyncSession) printResult(ios IO, res filesync.Result, source string) {
	if res.Changed {
		fmt.Fprintf(ios.Stderr, "rendered %s generation %s into %s\n", source, res.Stamp, s.cfg.Destination.Directory)
	} else {
		fmt.Fprintf(ios.Stderr, "unchanged %s generation %s\n", source, res.Stamp)
	}
	for _, name := range res.Retained {
		fmt.Fprintf(ios.Stderr, "retained %s as an unmanaged file\n", name)
	}
	for _, name := range res.Pruned {
		fmt.Fprintf(ios.Stderr, "pruned %s\n", name)
	}
}

func (s *fileSyncSession) fetch(ctx context.Context, cursor string) (apigen.DeliveryResponse, error) {
	q := url.Values{}
	q.Set("target", s.cfg.Target)
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var ack []string
	for _, f := range s.cfg.Files {
		ack = append(ack, f.AcknowledgeLoaderControl...)
	}
	if len(ack) > 0 {
		q.Set("acknowledged_keys", strings.Join(ack, ","))
	}
	var resp apigen.DeliveryResponse
	err := s.client.Do(ctx, http.MethodGet, deliveryPath(s.cfg.Org, s.cfg.Project, s.cfg.Environment)+"?"+q.Encode(), nil, &resp)
	return resp, err
}

func (s *fileSyncSession) snapshotBinding() (crypto.SnapshotBinding, error) {
	b, err := crypto.NewSnapshotBinding(crypto.SnapshotBindingScope{
		StorageDir: s.stateDir, InstanceOrigin: s.origin,
		OrgID: s.cfg.Org, ProjectID: s.cfg.Project, EnvironmentID: s.cfg.Environment,
		CredentialFingerprint: credentialFingerprint(s.token), TargetNames: []string{fileSyncSnapshotTarget + ":" + s.cfg.Target},
	})
	if err != nil {
		return crypto.SnapshotBinding{}, failf(ExitRefused, "file-sync: snapshot binding: %v", err)
	}
	return b, nil
}

// offline serves the last snapshot when the server is unreachable and the
// binding opted in. It never claims "current" and it records one durable
// disclosure record per served value before any plaintext is written.
func (s *fileSyncSession) offline(ctx context.Context, ios IO, dest *filesync.Destination, fetchErr error) (fileSyncOutcome, error) {
	if !isUnavailable(fetchErr) {
		return 0, fetchErr
	}
	if !s.cfg.Snapshot.OfflineServe {
		fmt.Fprintln(ios.Stderr, "hikyo file-sync: the server is unavailable; the last generation stands (set snapshot.offline_serve: true to render from the encrypted snapshot)")
		return 0, fetchErr
	}
	binding, err := s.snapshotBinding()
	if err != nil {
		return 0, err
	}
	payload, stored, err := compose.LoadSnapshot(s.keys, binding, ios.now(), s.cfg.SnapshotMaxAge())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, failf(ExitRefused, "file-sync: offline serve is enabled but no snapshot has been saved yet")
		}
		return 0, failf(ExitRefused, "file-sync: offline serve refused: %v", err)
	}
	aad, err := stored.AAD()
	if err != nil {
		return 0, failf(ExitInternal, "file-sync: reading offline snapshot binding: %v", err)
	}
	rows := make([]filesync.Row, 0, len(payload.Rows))
	for _, r := range payload.Rows {
		value := r.Value
		rows = append(rows, filesync.Row{KeyID: r.KeyID, Name: r.Name, Classification: r.Classification, Value: &value})
	}
	files, refusals := filesync.RenderAll(s.cfg, rows)
	if len(refusals) > 0 {
		lines := make([]string, 0, len(refusals))
		for _, r := range refusals {
			lines = append(lines, r.String())
		}
		return 0, failf(ExitRefused, "hikyo file-sync render (offline) refused; nothing written:\n  %s", strings.Join(lines, "\n  "))
	}
	stamp := filesync.GenerationStamp(s.keys, s.cfg.Target, s.policy, files)
	served := map[string]bool{}
	for _, f := range s.cfg.Files {
		for _, k := range f.Keys {
			served[k] = true
		}
	}
	var records []compose.OfflineRecord
	for _, r := range payload.Rows {
		if !served[r.Name] {
			continue
		}
		id, err := compose.NewRecordID()
		if err != nil {
			return 0, failf(ExitInternal, "file-sync: record id: %v", err)
		}
		records = append(records, compose.OfflineRecord{
			RecordID: id, KeyID: r.KeyID, KeyName: r.Name, Classification: r.Classification,
			OccurredAt: ios.now().UTC().Format(time.RFC3339), CredentialID: aad.CredentialID,
			Generation: stamp, ServedFrom: aad.IssuedAt,
		})
	}
	if err := compose.Append(s.stateDir, records); err != nil {
		return 0, failf(ExitInternal, "file-sync: recording offline disclosure: %v", err)
	}
	res, err := dest.Publish(ctx, filesync.Plan{Target: s.cfg.Target, Stamp: stamp, Files: files, Policy: s.policy}, nil)
	for _, f := range files {
		crypto.Zero(f.Content)
	}
	if err != nil {
		return 0, failf(ExitInternal, "file-sync: offline publish: %v", err)
	}
	fmt.Fprintf(ios.Stderr, "serving stale from %s\n", aad.IssuedAt)
	s.printResult(ios, res, "offline snapshot")
	return fileSyncOffline, nil
}

// report tells the server what this client reached. It is best-effort: the
// rendered files are the product, and a report that cannot be delivered is
// said on stderr, never turned into a failed render.
func (s *fileSyncSession) report(ctx context.Context, ios IO, state string, revision, generation int64, stamp string) {
	if generation <= 0 {
		fmt.Fprintln(ios.Stderr, "file-sync: the server did not name the target generation; no report sent (server older than API revision 7?)")
		return
	}
	body := apigen.FileTargetReportRequest{State: apigen.FileTargetState(state), Revision: revision, Generation: generation, ReportedAt: ios.now().UTC()}
	if stamp != "" {
		body.Stamp = &stamp
	}
	path := deliveryPath(s.cfg.Org, s.cfg.Project, s.cfg.Environment)
	path = strings.TrimSuffix(path, "/delivery") + "/file-targets/" + url.PathEscape(s.cfg.Target) + "/report"
	if err := s.client.Do(ctx, http.MethodPost, path, body, nil); err != nil {
		fmt.Fprintf(ios.Stderr, "file-sync: report not delivered (needs report-delivery-status on the environment): %v\n", err)
	}
}

// fileSyncDoctor inspects the local side only: no network, no credential.
func fileSyncDoctor(ios IO, f Format, cfg *filesync.Config, policy filesync.Policy, stateDir string) error {
	var findings []composeFinding
	add := func(status, check, msg string) {
		findings = append(findings, composeFinding{Status: status, Check: check, Message: msg})
	}
	add("ok", "config", fmt.Sprintf("target %s, %d file(s) into %s", cfg.Target, len(cfg.Files), cfg.Destination.Directory))
	switch ok, err := filesync.OnTmpfs(cfg.Destination.Directory); {
	case err != nil && cfg.Destination.RequireTmpfs:
		add("error", "tmpfs", err.Error())
	case err != nil:
		add("warn", "tmpfs", "cannot verify the destination filesystem: "+err.Error())
	case !ok && cfg.Destination.RequireTmpfs:
		add("error", "tmpfs", "require_tmpfs is set but the destination is not on tmpfs")
	case !ok:
		add("warn", "tmpfs", "the destination is not on tmpfs; plaintext is on persistent storage")
	default:
		add("ok", "tmpfs", "the destination is on tmpfs")
	}
	dest, err := filesync.OpenDestination(cfg.Destination.Directory, false)
	switch {
	case errors.Is(err, filesync.ErrBusy):
		add("warn", "destination", "another hikyo file-sync process holds the destination")
	case err != nil:
		add("error", "destination", err.Error())
	default:
		defer dest.Close()
		names := make([]string, 0, len(cfg.Files))
		for _, file := range cfg.Files {
			names = append(names, file.Name)
		}
		var keys *crypto.LocalKeys
		if _, serr := os.Stat(stateDir); serr == nil {
			keys, err = crypto.LoadOrCreateLocalKey(stateDir)
			if err != nil {
				add("error", "state", err.Error())
			}
		}
		switch {
		case keys == nil && err == nil:
			add("warn", "generation", "never rendered on this host (no local state)")
		case keys != nil:
			st, ok, ierr := dest.Intact(keys, cfg.Target, policy, names)
			switch {
			case ierr != nil:
				add("error", "generation", ierr.Error())
			case st.Generation == "":
				add("warn", "generation", "never rendered into this directory")
			case st.Target != cfg.Target:
				add("error", "generation", "the directory is bound to "+st.Target)
			case !ok:
				add("error", "generation", "generation "+st.Stamp+" is not intact (edited, wiped, or the policy changed); render again")
			default:
				add("ok", "generation", "generation "+st.Stamp+" is intact")
			}
		}
	}
	s := &fileSyncSession{cfg: cfg, policy: policy, stateDir: stateDir}
	if c := s.loadCursor(); c != nil {
		add("ok", "applied", fmt.Sprintf("revision %d (target generation %d) at %s", c.Revision, c.TargetGeneration, c.AppliedAt))
	}
	if fi, err := os.Stat(filepath.Join(stateDir, "snapshot.bin")); err == nil {
		age := ios.now().Sub(fi.ModTime()).Round(time.Second)
		switch {
		case !cfg.Snapshot.OfflineServe:
			add("ok", "snapshot", fmt.Sprintf("saved %s ago; offline serve is off", age))
		case age > cfg.SnapshotMaxAge():
			add("warn", "snapshot", fmt.Sprintf("saved %s ago, past the %s maximum: offline serve will refuse", age, cfg.SnapshotMaxAge()))
		default:
			add("ok", "snapshot", fmt.Sprintf("saved %s ago", age))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		add("error", "snapshot", err.Error())
	}
	report := composeDoctorReport{Status: "ok", Findings: findings}
	rows := make([][]string, 0, len(findings))
	for _, fd := range findings {
		rows = append(rows, []string{fd.Status, fd.Check, fd.Message})
		switch {
		case fd.Status == "error":
			report.Status = "error"
		case fd.Status == "warn" && report.Status == "ok":
			report.Status = "warning"
		}
	}
	if err := Render(ios.Stdout, f, Table{Columns: []string{"STATUS", "CHECK", "MESSAGE"}, Rows: rows, JSON: report}); err != nil {
		return err
	}
	if report.Status == "error" {
		return failf(ExitRefused, "file-sync doctor found errors")
	}
	return nil
}
