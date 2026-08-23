//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func watchTLSReloadSignal(ctx context.Context, reload func() error) func() {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	watchCtx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-hup:
				_ = reload()
			}
		}
	}()
	return func() {
		cancel()
		signal.Stop(hup)
	}
}
