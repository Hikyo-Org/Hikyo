package app

// `hikyo backup` and `hikyo restore` — the operator lifecycle (#76).
//
// Both are CLIENT VERBS OF THE SAME BINARY EXECUTED ON THE SERVER HOST, like
// `hikyo admin`, and neither has a network route. For `backup` that is a
// convenience argument; for `restore` and its reconciliation it is closer to
// forced. A restore invalidates every authentication artifact in the restored
// state and leaves every grant inert, so at the moment reconciliation has to
// happen there is no principal in existence who could authenticate, let alone
// authorize. The gate is host access — which the threat model already treats
// as operator-equivalent — and `cli:backup` / `cli:restore` are ClassSystem,
// whose probe contract the classification-totality invariant asserts by
// finding no HTTP route for them.
//
// Neither verb reads the root key. An export writes ciphertext it never
// decrypts; a restore replaces ciphertext it never decrypts. That is what
// keeps the backup identity and the root key in two custody stores with two
// failure domains: the drill proves the artifact is undecryptable with the
// root key alone, and unbootable with the age identity alone.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

// MinRestoreSchemaVersion is the first schema that carries the restore state
// a restore has to write (migration 00016, pinned to the migration file by
// TestMinRestoreSchemaVersionMatchesTheMigration). An archive below it is
// refused by name rather than failing deep inside the load transaction with a
// missing column — the operator's fix is "restore it with the binary that
// took it", and an error has to be able to say so.
const MinRestoreSchemaVersion = 16

// BackupUsage is the frozen help text for the export verb group.
func BackupUsage(w io.Writer) {
	fmt.Fprint(w, `hikyo backup - age-encrypted instance export (server host only)

  hikyo backup export [--out DIR] [--recipient AGE-RECIPIENT]...
                      [--passphrase-file PATH]
  hikyo backup keygen [--output-file PATH | --dangerously-print]

export writes ONE age-encrypted archive holding a consistent snapshot of the
datastore. Every sensitive field inside it is already envelope ciphertext and
the wrapped key hierarchy travels with it, so reading a value out of a backup
needs BOTH the backup identity and the root key. Export itself reads neither.

Recipients come from --recipient, or from HIKYO_BACKUP_RECIPIENTS; the
destination from --out or HIKYO_BACKUP_DIR. An export with no recipients is
refused, never written unencrypted. A passphrase (age's scrypt recipient) is
mutually exclusive with public recipients: the age specification requires an
scrypt stanza to be the only stanza in its container, so a container carrying
both would have two doors of unequal strength.

keygen mints a backup identity and prints it once. Store the PRIVATE half in a
custody store SEPARATE from the root key's - two keys in one password manager
is one failure domain wearing two names, and the escrow runbook requires two.
Delivery follows the print triad, as with every other display-once value.
`)
}

// RestoreUsage is the frozen help text for the restore verb group.
func RestoreUsage(w io.Writer) {
	fmt.Fprint(w, `hikyo restore - reconstruct an instance from an export (server host only)

  hikyo restore run --from ARCHIVE
                    (--identity-file PATH | --passphrase-file PATH)
  hikyo restore status
  hikyo restore reconcile --principal ID
  hikyo restore drill --from ARCHIVE
                      (--identity-file PATH | --passphrase-file PATH)
                      --root-key-file PATH --principal ID --project ORG/PROJECT
                      (--target-sqlite PATH | --target-postgres-dsn-file PATH)
                      [--cleanup] [-o json]

run refuses anything that is not a complete archive for THIS engine, and it
refuses it BEFORE touching the target: the container is decrypted in full and
authenticated through to its final chunk first, so a truncated backup can
never commit a prefix of itself.

A restore is a fail-closed security event. It advances the instance credential
epoch in the same act that loads the data, which makes every restored
authenticator inert at once - password verifiers, TOTP seeds, recovery-code
batches, passkeys, browser and CLI sessions, machine bearer credentials, OIDC
links and every single-use artifact. Machine bearer credentials are never
re-activated: re-mint and redistribute them. Until that redistribution
completes, workloads holding bearer credentials are down.

Restored grants are inert until you reconcile their principal. reconcile takes
exactly ONE principal and there is no form of it that takes a set: a restore
rewinds the authorization state, so it can resurrect a since-revoked role or a
removed member, and each identity is an informed decision. status lists who is
still waiting.

drill rehearses the WHOLE recovery against an EMPTY scratch target and never
touches the live datastore except to record the result. It restores the
archive, boots the restored data with the separately supplied root key, proves
one stored secret decrypts, reconciles one approved human, mints then revokes a
machine credential, and records the archive identity, versions, elapsed time
and RTO verdict on the live instance's trail. The root key and backup identity
are read from files, used for the drill's duration only, and never persisted or
logged. The scratch target is left for inspection unless --cleanup.
`)
}

// RunBackup dispatches the export verb group.
func RunBackup(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer,
	terminalSession *disclose.TerminalSession, terminalError error,
) error {
	if len(args) == 0 {
		BackupUsage(stderr)
		return errors.New("usage: hikyo backup export | hikyo backup keygen")
	}
	switch args[0] {
	case "upgrade-export", "upgrade-drill":
		trust, err := configuredBackupTrust(ctx, cfg, args[0] == "upgrade-drill")
		if err != nil {
			return err
		}
		return RunUpgradeBackup(ctx, cfg, args, stderr, trust)
	case "export":
		return runBackupExport(ctx, cfg, log, args, stderr)
	case "keygen":
		return runBackupKeygen(args, stderr, terminalSession, terminalError)
	default:
		BackupUsage(stderr)
		return fmt.Errorf("hikyo backup: unknown subcommand %q", args[0])
	}
}

type recipientList []string

func (r *recipientList) String() string     { return strings.Join(*r, ",") }
func (r *recipientList) Set(v string) error { *r = append(*r, strings.TrimSpace(v)); return nil }

func runBackupExport(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("backup export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "directory to publish the archive into (default HIKYO_BACKUP_DIR)")
	passphraseFile := fs.String("passphrase-file", "", "read the container passphrase from this file (age scrypt recipient)")
	var recipients recipientList
	fs.Var(&recipients, "recipient", "age public recipient; repeatable (default HIKYO_BACKUP_RECIPIENTS)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	options, err := exportOptions(cfg, recipients, *passphraseFile)
	if err != nil {
		return err
	}
	dir := *out
	if dir == "" {
		dir = cfg.BackupDir
	}
	if dir == "" {
		return errors.New("no destination: pass --out DIR or set HIKYO_BACKUP_DIR")
	}

	db, err := openBackupRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	svc := &service.Backup{DB: db, Options: options}
	result, err := svc.Export(ctx, dir)
	if err != nil {
		return err
	}
	if err := svc.RecordExport(ctx, service.TriggerManual, result); err != nil {
		// The artifact exists and is complete; only its record failed. Say
		// exactly that rather than implying the backup did not happen.
		return fmt.Errorf("the archive was published to %s, but its audit record could not be written: %w", result.Path, err)
	}
	log.Info("backup exported", "path", result.Path, "bytes", result.Bytes,
		"engine", result.Manifest.Engine, "schema_version", result.Manifest.SchemaVersion)
	fmt.Fprintf(stderr, "wrote %s (%d bytes, %s schema %d)\n",
		result.Path, result.Bytes, result.Manifest.Engine, result.Manifest.SchemaVersion)
	return nil
}

// exportOptions resolves the recipient policy from flags then configuration.
// Flags win entirely rather than merging: a half-overridden recipient set is
// how an operator ends up with an archive only the recipient they meant to
// replace can open.
func exportOptions(cfg *config.Config, recipients recipientList, passphraseFile string) (backup.Options, error) {
	o := backup.Options{Recipients: recipients}
	if len(o.Recipients) == 0 && passphraseFile == "" {
		o.Recipients = cfg.BackupRecipients
	}
	if passphraseFile != "" {
		pass, err := readSecretFile(passphraseFile)
		if err != nil {
			return backup.Options{}, err
		}
		o.Passphrase = pass
	}
	if err := o.Validate(); err != nil {
		return backup.Options{}, err
	}
	return o, nil
}

// readSecretFile reads a one-line secret from a file, refusing an empty one.
// Trailing newlines are stripped because every editor adds one and a
// passphrase that silently included it would be unreproducible by hand.
func readSecretFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	v := strings.TrimRight(string(raw), "\r\n")
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return v, nil
}

func runBackupKeygen(args []string, stderr io.Writer, terminalSession *disclose.TerminalSession, terminalError error) (returnErr error) {
	fs := flag.NewFlagSet("backup keygen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outputFile := fs.String("output-file", "", "write the identity to a file this command creates (0600)")
	dangerous := fs.Bool("dangerously-print", false, "write the identity to stdout, and to whatever collects it")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *outputFile != "" && *dangerous {
		return errors.New("--output-file and --dangerously-print name two destinations; choose one")
	}
	opts := disclose.Options{OutputFile: *outputFile, DangerouslyPrint: *dangerous}
	// Reserve the destination BEFORE the identity exists: minting a key that
	// has nowhere to go is minting a key nobody has.
	sink, err := prepareDisclosure(opts, terminalSession, terminalError)
	if err != nil {
		return err
	}
	defer sink.AbortOnReturn(&returnErr)
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		return err
	}
	dest, err := sink.WriteOnce("Backup identity (PRIVATE - escrow separately from the root key)", identity)
	if err != nil {
		return fmt.Errorf("the identity was generated but delivery failed and the value is now unrecoverable; run this again: %w", err)
	}
	fmt.Fprintf(stderr, "identity delivered to the %s\n", dest)
	fmt.Fprintf(stderr, "public recipient (safe to configure): %s\n", recipient)
	fmt.Fprintf(stderr, "set HIKYO_BACKUP_RECIPIENTS=%s and HIKYO_BACKUP_DIR=<path> to enable automatic pre-migration exports\n", recipient)
	return nil
}

// RunRestore dispatches the restore verb group.
func RunRestore(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer,
	_ *disclose.TerminalSession, _ error,
) error {
	if len(args) == 0 {
		RestoreUsage(stderr)
		return errors.New("usage: hikyo restore run --from ARCHIVE | hikyo restore status | hikyo restore reconcile --principal ID")
	}
	switch args[0] {
	case "run":
		return runRestoreRun(ctx, cfg, log, args, stderr)
	case "status":
		return runRestoreStatus(ctx, cfg, stderr)
	case "reconcile":
		return runRestoreReconcile(ctx, cfg, log, args, stderr)
	case "drill":
		return runRestoreDrill(ctx, cfg, log, args, stderr)
	default:
		RestoreUsage(stderr)
		return fmt.Errorf("hikyo restore: unknown subcommand %q", args[0])
	}
}

func runRestoreRun(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("restore run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "the age-encrypted archive to restore")
	identityFile := fs.String("identity-file", "", "file holding the backup age identity")
	passphraseFile := fs.String("passphrase-file", "", "file holding the container passphrase")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("--from ARCHIVE is required")
	}
	unlock, err := restoreUnlock(*identityFile, *passphraseFile)
	if err != nil {
		return err
	}

	// Decrypt the WHOLE container first, to a scratch file beside the
	// archive's destination. ExtractTo returns only after the container has
	// authenticated through to its final chunk, so everything below this line
	// runs on an archive already known to be complete — which is what makes
	// "a truncated backup is refused before any state is committed" a
	// property of the code path rather than of the operator's luck.
	work, err := os.MkdirTemp("", "hikyo-restore-")
	if err != nil {
		return fmt.Errorf("restore staging directory: %w", err)
	}
	defer os.RemoveAll(work)
	plain, err := decryptArchive(*from, filepath.Join(work, "archive.tar"), unlock)
	if err != nil {
		return err
	}
	defer plain.Close()

	manifest, err := store.ReadManifest(plain)
	if err != nil {
		return err
	}
	sc := storeConfig(cfg)
	if err := checkRestorable(ctx, sc, manifest); err != nil {
		return err
	}
	if _, err := plain.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}

	now := time.Now()
	switch sc.Engine {
	case store.EngineSQLite:
		if err := restoreOrdinarySQLite(ctx, cfg, sc, plain, manifest, now); err != nil {
			return err
		}
	case store.EnginePostgres:
		if err := restoreOrdinaryPostgres(ctx, cfg, sc, plain, manifest, now); err != nil {
			return err
		}

	default:
		return fmt.Errorf("restore: unknown engine %q", sc.Engine)
	}

	// Data publication never migrates or admits serving. A new current-incarnation
	// export/drill must pass the upgrade gate before any later schema writes.
	var status service.Status
	err = withReconciliation(ctx, cfg, func(s reconciliationService) error { var err error; status, err = s.Status(ctx); return err })
	if err != nil {
		return err
	}
	log.Warn("restore complete: every pre-restore authentication artifact is now inert",
		"engine", manifest.Engine, "credential_epoch", status.State.CredentialEpoch,
		"restore_epoch", status.State.RestoreEpoch, "pending_principals", len(status.Pending))
	fmt.Fprintf(stderr, "restored %s (schema %d); credential epoch is now %d\n",
		manifest.Engine, manifest.SchemaVersion, status.State.CredentialEpoch)
	printPending(stderr, status)
	return nil
}

// checkRestorable makes every shape refusal a restore can make before it
// opens the target, and names each one.
func checkRestorable(ctx context.Context, sc store.Config, m store.Manifest) error {
	if sc.Engine != m.Engine {
		return fmt.Errorf("%w: archive is %s, this instance is configured for %s",
			store.ErrEngineMismatch, m.Engine, sc.Engine)
	}
	if m.SchemaVersion < MinRestoreSchemaVersion {
		return fmt.Errorf("archive schema %d predates restore support (%d): restore it with the binary that took it",
			m.SchemaVersion, MinRestoreSchemaVersion)
	}
	// The emptiness check runs BEFORE anything that opens the datastore, so
	// nothing this function does can be what makes the target non-empty.
	switch sc.Engine {
	case store.EngineSQLite:
		if _, err := os.Stat(sc.Path); err == nil {
			return fmt.Errorf("%w: %s already exists — restore reconstructs an instance, it never merges into one",
				store.ErrTargetNotEmpty, sc.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check restore target: %w", err)
		}
	case store.EnginePostgres:
		actual, err := upgrade.InspectInstalled(ctx, upgrade.Config{Engine: releaseidentity.Postgres, DSN: sc.DSN}, releaseidentity.MigrationManifest{Engine: releaseidentity.Postgres, Entries: []releaseidentity.Migration{}})
		if err != nil {
			return fmt.Errorf("%w: PostgreSQL restore requires an empty schema: %v", store.ErrTargetNotEmpty, err)
		}
		if actual.Source.Genesis != releaseidentity.FreshGenesisV1 {
			return store.ErrTargetNotEmpty
		}

	}
	max, err := migrate.MaxVersion(ctx, sc)
	if err != nil {
		return err
	}
	if m.SchemaVersion > max {
		return fmt.Errorf("archive schema %d is newer than this binary knows (%d): restore it with the binary that took it",
			m.SchemaVersion, max)
	}
	return nil
}

func restoreUnlock(identityFile, passphraseFile string) (backup.Unlock, error) {
	switch {
	case (identityFile == "") == (passphraseFile == ""):
		return backup.Unlock{}, backup.ErrUnlock
	case identityFile != "":
		v, err := readSecretFile(identityFile)
		if err != nil {
			return backup.Unlock{}, err
		}
		return backup.Unlock{Identity: v}, nil
	default:
		v, err := readSecretFile(passphraseFile)
		if err != nil {
			return backup.Unlock{}, err
		}
		return backup.Unlock{Passphrase: v}, nil
	}
}

// decryptArchive writes the fully authenticated plaintext archive to path and
// returns it open at offset zero.
func decryptArchive(source, path string, u backup.Unlock) (*os.File, error) {
	in, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", source, err)
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("stage decrypted archive: %w", err)
	}
	if err := backup.ExtractTo(out, in, u); err != nil {
		out.Close()
		return nil, err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		out.Close()
		return nil, fmt.Errorf("rewind decrypted archive: %w", err)
	}
	return out, nil
}

func runRestoreStatus(ctx context.Context, cfg *config.Config, stderr io.Writer) error {
	var status service.Status
	err := withReconciliation(ctx, cfg, func(s reconciliationService) error { var err error; status, err = s.Status(ctx); return err })
	if err != nil {
		return err
	}

	if !status.State.Restored() {
		fmt.Fprintln(stderr, "this instance has never been restored; no reconciliation is outstanding")
		return nil
	}
	fmt.Fprintf(stderr, "restored at %s (credential epoch %d, restore epoch %d)\n",
		status.State.ReactivatedAt.Format(time.RFC3339), status.State.CredentialEpoch, status.State.RestoreEpoch)
	printPending(stderr, status)
	return nil
}

func printPending(w io.Writer, status service.Status) {
	if len(status.Pending) == 0 {
		fmt.Fprintln(w, "every principal has been reconciled; grants are in force")
		return
	}
	fmt.Fprintf(w, "%d principal(s) awaiting reconciliation; their grants do not authorize until each is committed:\n", len(status.Pending))
	for _, p := range status.Pending {
		fmt.Fprintf(w, "  %s (%s)\n", p.ID, p.Kind)
	}
	fmt.Fprintln(w, "reconcile one at a time: hikyo restore reconcile --principal ID")
}

func runRestoreReconcile(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("restore reconcile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// One principal, named explicitly. There is deliberately no --all, no
	// --principals and no filter here, and the drill asserts that the flag set
	// carries nothing of the kind.
	principal := fs.String("principal", "", "the single principal to reconcile")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *principal == "" {
		return errors.New("--principal is required: reconciliation is a per-principal assertion, and there is no bulk form")
	}
	var status service.Status
	err := withReconciliation(ctx, cfg, func(s reconciliationService) error {
		var err error
		status, err = s.Reconcile(ctx, domain.PrincipalID(*principal))
		return err
	})
	if err != nil {
		return err
	}

	log.Info("principal reconciled", "principal", *principal, "pending", len(status.Pending))
	fmt.Fprintf(stderr, "reconciled %s\n", *principal)
	printPending(stderr, status)
	return nil
}

// runRestoreDrill is the operator-verifiable disaster-recovery drill (#145,
// ops spec section 11). It runs host-local with the live config, like
// `restore reconcile`, but every restore-and-boot step happens against a
// SEPARATE scratch target: the live instance sees only the recorded verdict.
//
// Custody separation is literal here. The backup identity opens the container;
// the root key, read from its own file, boots the restored data and opens one
// secret. Both are used for the drill's duration and neither is persisted or
// logged: the root key bytes are zeroed after the keyring consumes them, and
// the audit payload has no field that could carry either.
func runRestoreDrill(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("restore drill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "the age-encrypted archive to rehearse")
	identityFile := fs.String("identity-file", "", "file holding the backup age identity")
	passphraseFile := fs.String("passphrase-file", "", "file holding the container passphrase")
	rootKeyFile := fs.String("root-key-file", "", "file holding the original root key (drill duration only, never persisted)")
	principal := fs.String("principal", "", "the single human principal to reconcile in the scratch instance")
	project := fs.String("project", "", "ORG/PROJECT the drill mints a throwaway credential in")
	targetSQLite := fs.String("target-sqlite", "", "empty scratch sqlite path to restore into")
	targetPostgresDSNFile := fs.String("target-postgres-dsn-file", "", "file holding an empty scratch postgres DSN")
	cleanup := fs.Bool("cleanup", false, "remove the scratch target after the drill instead of leaving it for inspection")
	format := fs.String("o", "", "output format: json for a machine-readable report")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch {
	case *from == "":
		return errors.New("--from ARCHIVE is required")
	case *rootKeyFile == "":
		return errors.New("--root-key-file is required: the drill boots the restored data under the original root key")
	case *principal == "":
		return errors.New("--principal is required: the drill reconciles exactly one approved human")
	case *project == "":
		return errors.New("--project ORG/PROJECT is required: the drill mints a throwaway credential there")
	case (*targetSQLite == "") == (*targetPostgresDSNFile == ""):
		return errors.New("choose exactly one scratch target: --target-sqlite or --target-postgres-dsn-file")
	}
	org, prj, ok := strings.Cut(*project, "/")
	if !ok || org == "" || prj == "" {
		return errors.New("--project must be ORG/PROJECT (two ids separated by a slash)")
	}
	scratch, err := drillTargetConfig(*targetSQLite, *targetPostgresDSNFile)
	if err != nil {
		return err
	}
	// The RTO clock covers the WHOLE recovery an operator performs: fetching
	// the identity and root key, hashing the archive, restoring, booting and
	// verifying. Start it before any of that so the recorded elapsed is honest.
	start := time.Now()
	unlock, err := restoreUnlock(*identityFile, *passphraseFile)
	if err != nil {
		return err
	}
	rootKey, err := crypto.ReadRootKey(*rootKeyFile, "")
	if err != nil {
		return err
	}
	// The keyring load below consumes and zeroes the copy it is handed, so a
	// second copy is kept here and wiped on every exit: the root key never
	// outlives the drill.
	defer crypto.Zero(rootKey)

	digest, err := fileDigest(*from)
	if err != nil {
		return err
	}

	report := service.DrillReport{
		Archive: filepath.Base(*from), ArchiveDigest: digest,
		BinaryVersion: Version, RTOTarget: cfg.BackupRTOTarget,
		Principal: *principal,
	}
	scope := domain.Scope{Org: domain.OrgID(org), Project: domain.ProjectID(prj)}

	manifest, drillErr := runDrillSteps(ctx, cfg, scratch, *from, unlock, rootKey, domain.PrincipalID(*principal), scope, &report)
	report.Engine = manifest.Engine
	report.SchemaVersion = manifest.SchemaVersion
	report.Elapsed = time.Since(start)
	// Every functional step passed but the recovery blew the RTO budget: name
	// the failure so the record and the terminal do not say `failed at ""`.
	if drillErr == nil && !report.OK() && report.FailedStep == "" {
		report.FailedStep = "rto"
	}

	// A refused or failed drill does not establish ownership of the target.
	// Preserve it for inspection, including any pre-existing database.
	if *cleanup && drillErr == nil && report.OK() {
		switch scratch.Engine {
		case store.EngineSQLite:
			if err := removeDrillSQLite(scratch.Path); err != nil {
				log.Warn("drill scratch target could not be removed", "err", err)
			}
		case store.EnginePostgres:
			// Dropping a database is a DB-admin act, not a proof-bound store
			// operation, so it is deliberately left to the operator who owns the
			// scratch DSN rather than issued through a raw driver handle here.
			log.Info("drill scratch postgres database left in place; drop it to reclaim space", "note", "host-local operator task")
		}
	}

	// Record the verdict on the LIVE instance regardless of outcome: a failed
	// drill the operator can see beats a silent one. A hard error before a
	// manifest existed (an unreadable archive) has nothing to record and is
	// returned as-is.
	if manifest.Engine != "" {
		live, err := openBackupRuntime(ctx, cfg)
		if err != nil {
			return errors.Join(drillErr, err)
		}
		recErr := (&service.Backup{DB: live}).RecordDrill(ctx, report)
		live.Close()
		if recErr != nil {
			return errors.Join(drillErr, fmt.Errorf("the drill ran but its record could not be written: %w", recErr))
		}
	}

	if err := printDrillReport(stderr, *format, report); err != nil {
		return err
	}
	if drillErr != nil {
		return drillErr
	}
	if !report.OK() {
		return fmt.Errorf("restore drill failed at %q", report.FailedStep)
	}
	log.Info("restore drill passed", "archive", report.Archive, "engine", report.Engine,
		"elapsed", report.Elapsed, "rto_target", report.RTOTarget)
	return nil
}

// runDrillSteps performs the restore-and-verify sequence against the scratch
// target and fills report.FailedStep on the first step that does not pass. It
// returns the manifest as soon as it is known so the caller can record engine
// and schema even for a failed drill.
func runDrillSteps(ctx context.Context, cfg *config.Config, scratch store.Config, from string,
	unlock backup.Unlock, rootKey []byte, principal domain.PrincipalID, scope domain.Scope, report *service.DrillReport,
) (store.Manifest, error) {
	work, err := os.MkdirTemp("", "hikyo-drill-")
	if err != nil {
		report.FailedStep = "staging"
		return store.Manifest{}, fmt.Errorf("drill staging directory: %w", err)
	}
	defer os.RemoveAll(work)
	plain, err := decryptArchive(from, filepath.Join(work, "archive.tar"), unlock)
	if err != nil {
		report.FailedStep = "decrypt"
		return store.Manifest{}, err
	}
	defer plain.Close()
	manifest, err := store.ReadManifest(plain)
	if err != nil {
		report.FailedStep = "manifest"
		return store.Manifest{}, err
	}
	if err := checkRestorable(ctx, scratch, manifest); err != nil {
		report.FailedStep = "preflight"
		return manifest, err
	}
	if _, err := plain.Seek(0, io.SeekStart); err != nil {
		report.FailedStep = "restore"
		return manifest, fmt.Errorf("rewind archive: %w", err)
	}
	now := time.Now()
	switch scratch.Engine {
	case store.EngineSQLite:
		if err := restoreOrdinarySQLite(ctx, cfg, scratch, plain, manifest, now); err != nil {
			report.FailedStep = "restore"
			return manifest, err
		}
	case store.EnginePostgres:
		if err := restoreOrdinaryPostgres(ctx, cfg, scratch, plain, manifest, now); err != nil {
			report.FailedStep = "restore"
			return manifest, err
		}
	}

	err = withDataRecovery(ctx, cfg, scratch, func(scratchDB *store.RecoveryDB) error {
		// Boot the restored data under the escrowed root key. A copy is handed to
		// LoadKeyring (which zeroes it); the caller keeps and wipes the original.
		existing := &keyring.RecoveryStore{DB: scratchDB}
		if err := crypto.VerifyExistingHierarchy(ctx, existing, append([]byte(nil), rootKey...)); err != nil {
			report.FailedStep = "boot"
			return err
		}
		kr, err := crypto.LoadKeyring(ctx, existing, append([]byte(nil), rootKey...))
		if err != nil {
			report.FailedStep = "boot"
			return fmt.Errorf("the restored data did not boot under the supplied root key: %w", err)
		}

		backupSvc := &service.Recovery{DB: scratchDB}
		readable, err := backupSvc.ProveValuesReadable(ctx, kr)
		report.ValuesReadable = readable
		if err != nil {
			report.FailedStep = "prove-values"
			return err
		}

		// Reconcile the one approved human, then mint and immediately revoke a
		// throwaway credential in the named project to prove the recovered
		// instance can issue new machine identity. Both use the reconciled
		// principal's own authority.
		if _, err := (&service.Recovery{DB: scratchDB}).Reconcile(ctx, principal); err != nil {
			report.FailedStep = "reconcile"
			return err
		}
		err = backupSvc.MintAndRevoke(ctx, kr, principal, scope)
		report.Minted = err == nil
		if err != nil {
			report.FailedStep = "mint-credential"
			return err
		}
		return nil
	})
	return manifest, err
}

// drillMintAndRevoke creates a throwaway workload service account in the
// scratch instance, mints one credential and revokes it at once, proving the
// recovered instance issues new machine identity. A fresh workload SA reaches
// no plaintext, so the mint's disclosure conjunct is vacuous and needs no
// reauthentication window - which a host-local operator verb has no way to
// open anyway.
func drillMintAndRevoke(ctx context.Context, db *store.DB, kr *crypto.Keyring, principal domain.PrincipalID, scope domain.Scope) (bool, error) {
	ids := &service.Identities{DB: db, Auth: &service.Auth{DB: db, Keyring: kr}}
	actor := service.LocalPrincipal(principal)
	sa, err := ids.CreateServiceAccount(ctx, actor, scope, "drill-verification", domain.ClassWorkload)
	if err != nil {
		return false, fmt.Errorf("create drill service account: %w", err)
	}
	if _, err := ids.MintCredential(ctx, actor, scope, sa.ID, service.MintRequest{}); err != nil {
		return false, fmt.Errorf("mint drill credential: %w", err)
	}
	// The credential proved issuance; it must not outlive the drill. Deleting
	// the service account atomically revokes its one credential (cascade). A
	// revoke failure is reported as NOT minted-and-revoked: a usable credential
	// left behind is a failed drill, not a passed one, and the returned error
	// keeps the scratch target around (no --cleanup on failure) for cleanup.
	if err := ids.DeleteServiceAccount(ctx, actor, scope, sa.ID); err != nil {
		return false, fmt.Errorf("revoke drill credential: %w", err)
	}
	return true, nil
}

func drillTargetConfig(sqlitePath, postgresDSNFile string) (store.Config, error) {
	if sqlitePath != "" {
		return store.Config{Engine: store.EngineSQLite, Path: sqlitePath}, nil
	}
	dsn, err := readSecretFile(postgresDSNFile)
	if err != nil {
		return store.Config{}, err
	}
	return store.Config{Engine: store.EnginePostgres, DSN: dsn}, nil
}

// removeDrillSQLite deletes the sqlite scratch instance after a --cleanup
// drill. Postgres scratch cleanup is the operator's DB-admin task: dropping a
// database is not a proof-bound store operation and must not reach for a raw
// driver handle from the app layer.
func removeDrillSQLite(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// fileDigest is the archive's content digest for the drill record: it names
// exactly which bytes were rehearsed, without copying the archive anywhere.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open archive %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("digest archive %s: %w", path, err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func printDrillReport(w io.Writer, format string, r service.DrillReport) error {
	if format == "json" {
		return writeDrillJSON(w, r)
	}
	verdict := "PASSED"
	if !r.OK() {
		verdict = "FAILED at " + r.FailedStep
	}
	fmt.Fprintf(w, "restore drill %s\n", verdict)
	fmt.Fprintf(w, "  archive:        %s (%s)\n", r.Archive, r.ArchiveDigest)
	fmt.Fprintf(w, "  engine/schema:  %s / %d\n", r.Engine, r.SchemaVersion)
	fmt.Fprintf(w, "  binary:         %s\n", r.BinaryVersion)
	fmt.Fprintf(w, "  elapsed:        %s (RTO target %s, %s)\n", r.Elapsed.Round(time.Millisecond), r.RTOTarget, rtoVerdict(r))
	fmt.Fprintf(w, "  values read:    %t\n", r.ValuesReadable)
	fmt.Fprintf(w, "  principal:      %s reconciled\n", r.Principal)
	fmt.Fprintf(w, "  credential:     minted and revoked = %t\n", r.Minted)
	return nil
}

func rtoVerdict(r service.DrillReport) string {
	if r.Elapsed <= r.RTOTarget {
		return "met"
	}
	return "EXCEEDED"
}

func writeDrillJSON(w io.Writer, r service.DrillReport) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"archive":         r.Archive,
		"archive_digest":  r.ArchiveDigest,
		"engine":          string(r.Engine),
		"schema_version":  r.SchemaVersion,
		"binary_version":  r.BinaryVersion,
		"elapsed_ms":      r.Elapsed.Milliseconds(),
		"rto_target_ms":   r.RTOTarget.Milliseconds(),
		"rto_met":         r.Elapsed <= r.RTOTarget,
		"values_readable": r.ValuesReadable,
		"principal":       r.Principal,
		"credential":      r.Minted,
		"ok":              r.OK(),
		"failed_step":     r.FailedStep,
	})
}

// Status is service.Status, aliased so the printer's signature does not drag
// the service package into every caller's head.
type Status = service.Status
