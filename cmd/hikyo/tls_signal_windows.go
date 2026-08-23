//go:build windows

package main

import "context"

func watchTLSReloadSignal(context.Context, func() error) func() {
	return func() {}
}
