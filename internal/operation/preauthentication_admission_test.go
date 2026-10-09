package operation

import (
	"context"
	"errors"
	"testing"
)

func TestPreauthenticationAdmissionRetainsOneRefusalAcrossNestedReads(t *testing.T) {
	refusal := errors.New("overloaded")
	charges := 0
	ctx := WithPreauthenticationAdmission(context.Background(), func() error { charges++; return refusal })
	for range 3 {
		if !errors.Is(AdmitPreauthentication(ctx), refusal) {
			t.Fatal("nested resolution lost refusal")
		}
	}
	if charges != 1 {
		t.Fatalf("charged %d times", charges)
	}
}
