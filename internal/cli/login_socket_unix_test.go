//go:build darwin || linux

package cli

import (
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLoginListenerDisablesReuseBeforeBind(t *testing.T) {
	listener, err := listenLoginLoopback(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		t.Fatal("loopback listener is not TCP")
	}
	address, ok := tcp.Addr().(*net.TCPAddr)
	if !ok || !address.IP.Equal(net.ParseIP("127.0.0.1")) || address.Port == 0 {
		t.Fatalf("listener is not bound to an ephemeral IPv4 loopback literal: %v", tcp.Addr())
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Control(func(fd uintptr) {
		for _, option := range []int{unix.SO_REUSEADDR, unix.SO_REUSEPORT} {
			value, err := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, option)
			if err != nil || value != 0 {
				t.Errorf("socket option %d = %d, error %v; want 0", option, value, err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("tcp4", listener.Addr().String())
	if err == nil {
		second.Close()
		t.Fatal("another listener acquired the login callback's address")
	}
}
