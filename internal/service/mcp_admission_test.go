package service

import (
	"context"
	"testing"
	"time"
)

func TestMCPCleanupContextSurvivesRequestCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	cancelRequest()

	cleanupCtx, cancelCleanup := mcpCleanupContext(requestCtx)
	defer cancelCleanup()

	select {
	case <-cleanupCtx.Done():
		t.Fatalf("cleanup context inherited request cancellation: %v", cleanupCtx.Err())
	default:
	}
	deadline, ok := cleanupCtx.Deadline()
	if !ok || time.Until(deadline) > mcpAdmissionCleanupTimeout {
		t.Fatalf("cleanup context deadline = %v, want at most %v", deadline, mcpAdmissionCleanupTimeout)
	}
}
