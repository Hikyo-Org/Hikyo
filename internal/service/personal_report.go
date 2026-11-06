package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// errPersonalReportPending marks the contract stubs below until they are
// implemented.
var errPersonalReportPending = errors.New("service: not implemented")

// PersonalCount is one key's matrix mark: counts, never values.
type PersonalCount struct {
	KeyID    string
	Name     string
	Holders  int
	Notices  int
	Required bool
}

// PersonalCounts is the matrix marks for one environment. Eligible is the
// denominator: humans currently holding both read and reveal there.
type PersonalCounts struct {
	Eligible int
	Items    []PersonalCount
}

// OffboardingReceipt is one shared key a person is recorded as receiving.
type OffboardingReceipt struct {
	EnvironmentID   domain.EnvID
	EnvironmentName string
	KeyName         string
	LastReceivedAt  time.Time
	ChangedSince    bool
}

// OffboardingServiceAccount is a service account the person created.
type OffboardingServiceAccount struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// OffboardingReport lists what to rotate after someone leaves (ADR,
// Offboarding 3). Names only.
type OffboardingReport struct {
	Principal       domain.PrincipalID
	Username        string
	EvidenceFrom    time.Time
	GeneratedAt     time.Time
	Received        []OffboardingReceipt
	ServiceAccounts []OffboardingServiceAccount
	Limits          []string
}

// Counts returns per-key holder and notice counts for the matrix marks.
func (s *PersonalValues) Counts(ctx context.Context, actor Actor, scope domain.Scope) (PersonalCounts, error) {
	return PersonalCounts{}, errPersonalReportPending
}

// OffboardingReport builds the offboarding report for one member of a project.
func (s *PersonalValues) OffboardingReport(ctx context.Context, actor Actor, scope domain.Scope, member string) (OffboardingReport, error) {
	return OffboardingReport{}, errPersonalReportPending
}
