//go:build !windows

package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSIGHUPTriggersTLSReloadWithoutCancellingServeContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reloaded := make(chan struct{}, 1)
	stop := watchTLSReloadSignal(ctx, func() error {
		reloaded <- struct{}{}
		return nil
	})
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("SIGHUP did not trigger TLS reload")
	}
	select {
	case <-ctx.Done():
		t.Fatal("SIGHUP cancelled the serve context")
	default:
	}
}
