package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestAWSRoutingRefusalIsCallerCorrectableBeforeProvider(t *testing.T) {
	for _, destination := range []AdapterTargetInput{
		{DestinationKind: "json-object", DestinationOwner: "123456789012", DestinationName: "browser/adopt-config"},
		{DestinationKind: "per-key", DestinationOwner: "123456789012", DestinationName: "missing-slash"},
		{DestinationKind: "json-object", DestinationOwner: "not-an-account", DestinationName: "browser/config"},
	} {
		err := normalizeTargetInput(string(adapter.AWSSecretsManagerProvider), &destination)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid AWS routing is not a caller-correctable refusal: %v", err)
		}
		var detail interface{ SafeDetail() string }
		if !errors.As(err, &detail) || detail.SafeDetail() == "" {
			t.Fatal("routing refusal lost its caller-safe explanation")
		}
	}
	valid := AdapterTargetInput{DestinationKind: "json-object", DestinationOwner: "123456789012", DestinationName: "browser/adoption/config"}
	if err := normalizeTargetInput(string(adapter.AWSSecretsManagerProvider), &valid); err != nil {
		t.Fatal(err)
	}
}
