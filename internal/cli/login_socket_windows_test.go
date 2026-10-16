package cli

import (
	"net"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLoginListenerUsesExclusiveWindowsSocket(t *testing.T) {
	listener, err := listenLoginLoopback(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		t.Fatal("loopback listener is not TCP")
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Control(func(fd uintptr) {
		for option, expected := range map[int]int{windows.SO_REUSEADDR: 0, ^windows.SO_REUSEADDR: 1} {
			value, err := windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, option)
			if err != nil || value != expected {
				t.Errorf("socket option %d = %d, error %v; want %d", option, value, err, expected)
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
