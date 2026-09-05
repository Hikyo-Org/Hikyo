package service

import (
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type DiagnosticFinding struct{ Code, Severity, Message string }

// Diagnostics contains only boot-validated policy and an authoritative local
// filesystem reader. A nil reader or policy reports unknown, never defaults.
type Diagnostics struct {
	Passwords *crypto.PasswordParams
	Volume    func() (storagehealth.Capacity, error)
}

type DiagnosticHealth struct {
	Findings      []DiagnosticFinding
	Volume        storagehealth.Health
	EscrowCurrent bool
	Metadata      store.OpsMetadata
}

func (s *Retention) diagnosticHealth(meta store.OpsMetadata, instance, incarnation string) DiagnosticHealth {
	h := DiagnosticHealth{Metadata: meta}
	add := func(code, severity, message string) {
		h.Findings = append(h.Findings, DiagnosticFinding{code, severity, message})
	}
	if s.Diagnostics != nil && s.Diagnostics.Volume != nil {
		if capacity, err := s.Diagnostics.Volume(); err == nil {
			h.Volume = storagehealth.FromCapacity(capacity)
		}
	}
	if !h.Volume.Known {
		message := "Datastore filesystem capacity is unavailable; verify capacity with the host storage monitor"
		if s.DB.Engine() == store.EnginePostgres {
			message = "PostgreSQL volume capacity is not measurable from the application host; configure monitoring on the database storage host"
		}
		add("data-volume", "unknown", message)
	} else {
		severity := "ok"
		threshold := ""
		if h.Volume.UsedPercent >= 90 {
			severity = "error"
			threshold = "; at or above the 90% critical storage threshold"
		} else if h.Volume.UsedPercent >= 80 {
			severity = "warn"
			threshold = "; at or above the 80% storage warning threshold"
		}
		add("data-volume", severity, fmt.Sprintf("Datastore volume %.1f%% used; %d bytes available%s", h.Volume.UsedPercent, h.Volume.AvailableBytes, threshold))
	}
	h.EscrowCurrent = meta.RootWrappers == 1 && !meta.EscrowVerifiedAt.IsZero() && !meta.EscrowVerifiedAt.After(s.now()) && meta.EscrowInstanceID == instance && meta.EscrowIncarnation == incarnation && meta.EscrowRootEpoch == meta.RootEpoch
	if h.EscrowCurrent {
		add("root-escrow", "ok", "Current root escrow unwrap verified "+meta.EscrowVerifiedAt.UTC().Format(time.RFC3339)+"; separate offline custody is operator-asserted")
	} else {
		add("root-escrow", "warn", "Current root escrow has not been verified for this recovery incarnation; recover the separate custody copy and run hikyo escrow verify")
	}
	severity := "ok"
	if meta.PinsExpired+meta.PinsDay+meta.PinsWeek+meta.PinsMonth > 0 {
		severity = "warn"
	}
	add("pin-expiry", severity, fmt.Sprintf("Pins: %d expired, %d expire within 1 day, %d within 7 days, %d within 30 days (disjoint tiers); expired pins deliver only while their payload survives", meta.PinsExpired, meta.PinsDay, meta.PinsWeek, meta.PinsMonth))
	if meta.RootWrappers == 1 {
		add("root-rotation", "ok", fmt.Sprintf("One active root wrapper at epoch %d", meta.RootEpoch))
	} else if meta.RootWrappers > 1 {
		add("root-rotation", "warn", "Root key remains dual-wrapped; complete the verified root rotation")
	} else {
		add("root-rotation", "error", "No active root wrapper is recorded")
	}
	last := "no successful reencrypt completion recorded"
	if !meta.LastReencryptSuccess.IsZero() {
		last = "last successful scope completion " + meta.LastReencryptSuccess.UTC().Format(time.RFC3339)
	}
	severity = "ok"
	if meta.RetiringScopes > 0 {
		severity = "warn"
	}
	add("reencrypt", severity, fmt.Sprintf("%d key scopes have retiring versions; %s", meta.RetiringScopes, last))
	if s.DB.DurabilityVerified() {
		message := "SQLite boot verified WAL, synchronous=FULL, foreign_keys=on and read_uncommitted=off"
		if s.DB.Engine() == store.EnginePostgres {
			message = "PostgreSQL boot verified fsync=on and synchronous_commit=on"
		}
		add("database-durability", "ok", message)
	} else {
		add("database-durability", "unknown", "Database boot durability verification is unavailable")
	}
	if s.Diagnostics != nil && s.Diagnostics.Passwords != nil && s.Diagnostics.Passwords.CheckFloor() == nil {
		p := s.Diagnostics.Passwords
		add("argon2-floor", "ok", fmt.Sprintf("Boot verified Argon2id memory=%d KiB, time=%d, parallelism=%d", p.MemoryKiB, p.Time, p.Parallelism))
	} else {
		add("argon2-floor", "unknown", "Boot-validated Argon2id policy is unavailable")
	}
	return h
}
