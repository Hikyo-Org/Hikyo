package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type failingPKISweeper struct {
	calls  int
	cancel context.CancelFunc
}

func (s *failingPKISweeper) RunPKISweep(context.Context) (bool, error) {
	s.calls++
	// A second immediate call detects the busy-loop without waiting an hour.
	if s.calls > 1 {
		s.cancel()
	}
	return true, errors.New("persistent CRL failure after progress")
}
func TestPKIWorkerWaitsAfterPartialProgressError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	svc := &failingPKISweeper{cancel: cancel}
	worker := pkiWorker{svc: svc, poll: time.Hour, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	worker.Run(ctx)
	if svc.calls != 1 {
		t.Fatalf("error caused %d immediate attempts", svc.calls)
	}
}
