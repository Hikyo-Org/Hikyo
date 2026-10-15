package cli

import (
	"context"
	"net"
	"syscall"
)

func listenLoginLoopback(ctx context.Context) (net.Listener, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionError error
		if err := raw.Control(func(fd uintptr) { optionError = loginSocketOptions(fd) }); err != nil {
			return err
		}
		return optionError
	}}
	return config.Listen(ctx, "tcp4", "127.0.0.1:0")
}
