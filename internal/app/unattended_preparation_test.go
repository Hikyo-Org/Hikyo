//go:build darwin || linux

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func preparationIdentity(version string, sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte(version + "c")), ManifestSHA256: releaseidentity.Hash([]byte(version + "m"))}
}

func TestPreparationNeverWroteOnlyForTheUntouchedSource(t *testing.T) {
	source, target := preparationIdentity("1.0.0", 1), preparationIdentity("1.0.1", 2)
	manifest := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	schema, route := releaseidentity.Hash([]byte("schema")), releaseidentity.Hash([]byte("route"))
	journal := func() *automaticJournal {
		return &automaticJournal{Phase: "preparing", Target: target, Route: route, Instance: "installation", Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: source}, Migrations: manifest, SchemaSHA256: schema}}
	}
	healthy := func() upgrade.State {
		return upgrade.State{InstanceID: "installation", Applied: releaseidentity.Source{Release: source}, SchemaDigest: schema, MigrationDigest: digest, Pending: &upgrade.Operation{Phase: upgrade.Healthy}}
	}
	frozen := func() upgrade.State {
		state := healthy()
		state.Maintenance = true
		state.Pending.Phase = upgrade.BackupPreparing
		state.Pending.Preparation = &upgrade.BackupPreparation{Target: target, RouteDigest: route}
		return state
	}
	for _, test := range []struct {
		name   string
		change func(*automaticJournal, *upgrade.State)
		err    error
		want   bool
	}{
		{name: "never fenced", change: func(*automaticJournal, *upgrade.State) {}, want: true},
		{name: "fenced for this journal", change: func(_ *automaticJournal, s *upgrade.State) { *s = frozen() }, want: true},
		{name: "fenced for another target", change: func(_ *automaticJournal, s *upgrade.State) {
			*s = frozen()
			s.Pending.Preparation.Target = preparationIdentity("1.0.2", 3)
		}},
		{name: "fenced for another route", change: func(_ *automaticJournal, s *upgrade.State) {
			*s = frozen()
			s.Pending.Preparation.RouteDigest = releaseidentity.Hash([]byte("other route"))
		}},
		{name: "fenced without its preparation", change: func(_ *automaticJournal, s *upgrade.State) { *s = frozen(); s.Pending.Preparation = nil }},
		{name: "journal at write-intent", change: func(j *automaticJournal, s *upgrade.State) { *s = frozen(); j.Phase = "write-intent" }},
		{name: "journal at schema-applied", change: func(j *automaticJournal, _ *upgrade.State) { j.Phase = "schema-applied" }},
		{name: "journal at rolling-back", change: func(j *automaticJournal, _ *upgrade.State) { j.Phase = "rolling-back" }},
		{name: "journal at restore-required", change: func(j *automaticJournal, _ *upgrade.State) { j.Phase = "restore-required" }},
		{name: "operation prepared", change: func(_ *automaticJournal, s *upgrade.State) { s.Pending.Phase, s.Maintenance = upgrade.Prepared, true }},
		{name: "schema write started", change: func(_ *automaticJournal, s *upgrade.State) {
			s.Pending.Phase, s.Maintenance = upgrade.SchemaWriteStarted, true
		}},
		{name: "restore required", change: func(_ *automaticJournal, s *upgrade.State) {
			s.Pending.Phase, s.Maintenance = upgrade.RestoreRequired, true
		}},
		{name: "intermediate hop under maintenance", change: func(_ *automaticJournal, s *upgrade.State) { s.Maintenance = true }},
		{name: "invalidated", change: func(_ *automaticJournal, s *upgrade.State) { *s = frozen(); s.Pending.Invalidated = true }},
		{name: "database moved to another release", change: func(_ *automaticJournal, s *upgrade.State) { s.Applied = releaseidentity.Source{Release: target} }},
		{name: "another installation", change: func(_ *automaticJournal, s *upgrade.State) { s.InstanceID = "other" }},
		{name: "another schema", change: func(_ *automaticJournal, s *upgrade.State) { s.SchemaDigest = releaseidentity.Hash([]byte("other")) }},
		{name: "another migration history", change: func(_ *automaticJournal, s *upgrade.State) { s.MigrationDigest = releaseidentity.Hash([]byte("other")) }},
		{name: "control state unreadable", err: errors.New("unreadable"), change: func(*automaticJournal, *upgrade.State) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			j, state := journal(), healthy()
			test.change(j, &state)
			if got := j.preparationNeverWrote(state, test.err); got != test.want {
				t.Fatalf("preparationNeverWrote = %t", got)
			}
		})
	}
	if j := journal(); !j.sourceIs(source) || j.sourceIs(target) || j.sourceIs(preparationIdentity("1.0.2", 3)) {
		t.Fatal("only the journal's own source image may resume")
	}
}

func TestAbortMarksOnlyAJournalStillPreparing(t *testing.T) {
	for phase, marked := range map[string]bool{"preparing": true, "write-intent": false, "schema-applied": false} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "operation.json")
			journal := &automaticJournal{Phase: phase}
			cause := errors.New("drill failed " + strings.Repeat("x", 2000))
			err := abortUnattendedPreparation(path, journal, cause)
			if !errors.Is(err, cause) {
				t.Fatalf("the cause was hidden: %v", err)
			}
			if marked != (journal.Aborted != "") || len(journal.Aborted) > 512 {
				t.Fatalf("aborted = %q", journal.Aborted)
			}
		})
	}
}

func TestSweepKeepsOnlyWhatTheJournalReferences(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"backup-kept", "evidence-kept", "backup-old", "evidence-old", "scratch-old", ".hikyo-upgrade-evidence-old", "downloads", "custody", "backupnotes"} {
		if err := os.MkdirAll(filepath.Join(directory, name, "inner"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"scratch-ownership.json", "image.json", "backup-file"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A symbolic link named like an attempt is never followed or removed as one.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(directory, "backup-link")); err != nil {
		t.Fatal(err)
	}
	kept := &automaticJournal{}
	kept.Runtime.EvidenceDirectory = filepath.Join(directory, "evidence-kept")
	kept.Runtime.CiphertextPath = filepath.Join(directory, "backup-kept", "inner", "ciphertext.age")
	if err := sweepUnattendedAttempts(directory, kept); err != nil {
		t.Fatal(err)
	}
	remaining := map[string]bool{}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		remaining[entry.Name()] = true
	}
	for _, name := range []string{"backup-kept", "evidence-kept", "downloads", "custody", "backupnotes", "scratch-ownership.json", "image.json", "backup-file", "backup-link"} {
		if !remaining[name] {
			t.Fatalf("%s was removed", name)
		}
	}
	for _, name := range []string{"backup-old", "evidence-old", "scratch-old", ".hikyo-upgrade-evidence-old"} {
		if remaining[name] {
			t.Fatalf("%s survived the sweep", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("the sweep followed a symbolic link")
	}
	// Journal paths that leave the directory retain nothing and remove nothing extra.
	hostile := &automaticJournal{}
	hostile.Runtime.EvidenceDirectory, hostile.Runtime.CiphertextPath = "/etc", filepath.Join(directory, "..", "backup-kept")
	if err := sweepUnattendedAttempts(directory, hostile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "backup-kept")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a journal path outside the directory kept a copy alive")
	}
	if err := sweepUnattendedAttempts(directory, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "evidence-kept")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an abandoned preparation kept a copy")
	}
}

// A failed or abandoned preparation names no copy: everything it made goes.
func TestAbortingAPreparationRemovesItsCopiesAndParksTheRetry(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"backup-1", "evidence-1"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	journalPath := filepath.Join(directory, "operation.json")
	journal := &automaticJournal{Format: "hikyo.container-upgrade/v1", Phase: "preparing", Target: preparationIdentity("1.0.1", 2), Route: releaseidentity.Hash([]byte("route")), Instance: "i",
		Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: preparationIdentity("1.0.0", 1)}, Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
	err := abortUnattendedPreparation(journalPath, journal, errors.New("proof refused"))
	if !errors.Is(err, ErrPreparationAborted) || len(attemptCopies(t, directory)) != 0 {
		t.Fatalf("abort left copies or did not park: %v %v", err, attemptCopies(t, directory))
	}
	// Past "preparing" nothing is claimed and nothing is removed.
	if err := os.Mkdir(filepath.Join(directory, "backup-2"), 0700); err != nil {
		t.Fatal(err)
	}
	journal.Phase = "write-intent"
	if err := abortUnattendedPreparation(journalPath, journal, errors.New("later failure")); errors.Is(err, ErrPreparationAborted) || len(attemptCopies(t, directory)) != 1 {
		t.Fatalf("a journal past preparing was treated as aborted: %v", err)
	}
}

// The last completed backup is the only copy on the volume until a replacement is
// journaled, so neither a sweep, an abort nor an abandonment may remove it.
func TestLastBackupSurvivesFailedAttemptsUntilItsReplacementIsJournaled(t *testing.T) {
	directory := t.TempDir()
	mk := func(names ...string) {
		for _, name := range names {
			if err := os.MkdirAll(filepath.Join(directory, name, "inner"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	remaining := func() string { return strings.Join(attemptCopies(t, directory), ",") }
	completed := &automaticJournal{Phase: "complete"}
	completed.Runtime.EvidenceDirectory = filepath.Join(directory, "evidence-last")
	completed.Runtime.CiphertextPath = filepath.Join(directory, "backup-last", "inner", "ciphertext.age")
	mk("backup-last", "evidence-last", "backup-failed", "scratch-failed")
	if err := retainLastBackup(directory, completed.Runtime); err != nil {
		t.Fatal(err)
	}
	preparing := &automaticJournal{Format: "hikyo.container-upgrade/v1", Phase: "preparing", Target: preparationIdentity("1.0.1", 2), Route: releaseidentity.Hash([]byte("route")), Instance: "i",
		Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: preparationIdentity("1.0.0", 1)}, Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
	if err := sweepUnattendedAttempts(directory, preparing); err != nil || remaining() != "backup-last,evidence-last" {
		t.Fatalf("the sweep did not spare exactly the completed backup: %q %v", remaining(), err)
	}
	// The attempt's own copies go on abort, and the journal is deleted on abandonment.
	mk("backup-attempt", "evidence-attempt")
	err := abortUnattendedPreparation(filepath.Join(directory, "operation.json"), preparing, errors.New("proof refused"))
	if !errors.Is(err, ErrPreparationAborted) || remaining() != "backup-last,evidence-last" {
		t.Fatalf("abort removed the completed backup or kept the attempt's: %q %v", remaining(), err)
	}
	if err := sweepUnattendedAttempts(directory, nil); err != nil || remaining() != "backup-last,evidence-last" {
		t.Fatalf("abandonment removed the completed backup: %q %v", remaining(), err)
	}
	// Once the replacement is journaled and recorded, the old backup is removed.
	mk("backup-new", "evidence-new")
	preparing.Runtime.EvidenceDirectory = filepath.Join(directory, "evidence-new")
	preparing.Runtime.CiphertextPath = filepath.Join(directory, "backup-new", "inner", "ciphertext.age")
	if err := errors.Join(retainLastBackup(directory, preparing.Runtime), sweepUnattendedAttempts(directory, preparing)); err != nil || remaining() != "backup-new,evidence-new" {
		t.Fatalf("the replaced backup was kept or the replacement removed: %q %v", remaining(), err)
	}
	// An unreadable record removes nothing rather than guessing.
	if err := os.WriteFile(filepath.Join(directory, retainedBackupFile), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sweepUnattendedAttempts(directory, nil); err == nil || remaining() != "backup-new,evidence-new" {
		t.Fatalf("an invalid record was ignored: %q %v", remaining(), err)
	}
}

// A target restarted during a rollback must idle (ErrPreparationAborted) before it
// reads or writes the database; a journal at "rolling-back" round-trips strictly.
func TestRestartedTargetIdlesAtRollingBack(t *testing.T) {
	directory := t.TempDir()
	target := preparationIdentity("1.0.1", 2)
	journal := &automaticJournal{Format: "hikyo.container-upgrade/v1", Phase: "rolling-back", Target: target, Route: releaseidentity.Hash([]byte("route")), Instance: "i", RollbackDatabase: "db",
		Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: preparationIdentity("1.0.0", 1)}, Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
	if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), journal); err != nil {
		t.Fatal(err)
	}
	err := runPreparedUnattended(t.Context(), nil, UnattendedUpgradeOptions{}, directory, nil, releasetrust.PinnedTrust{}, nil, nil, selfupdate.PreparedNightly{Identity: target})
	if !errors.Is(err, ErrPreparationAborted) {
		t.Fatalf("target did not idle at rolling-back: %v", err)
	}
}

// A journal that cannot be read is not "no rollback": the target stops instead of touching a database
// a rollback may be rewriting.
func TestRunPreparedUnattendedRefusesAnUnreadableJournal(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "operation.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	err := runPreparedUnattended(t.Context(), nil, UnattendedUpgradeOptions{}, directory, nil, releasetrust.PinnedTrust{}, nil, nil, selfupdate.PreparedNightly{})
	if err == nil || !strings.Contains(err.Error(), "invalid private upgrade journal") {
		t.Fatalf("an unreadable journal was ignored: %v", err)
	}
}
