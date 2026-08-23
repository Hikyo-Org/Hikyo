package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestConsumeReauthEvidenceRefusesZeroValue(t *testing.T) {
	auth := &Auth{}

	err := auth.ConsumeReauthEvidence(context.Background(), nil, ReauthEvidence{}, "")

	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("ConsumeReauthEvidence() error = %v, want %v", err, domain.ErrUnauthenticated)
	}
}
