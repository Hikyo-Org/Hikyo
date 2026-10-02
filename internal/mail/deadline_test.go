package mail

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// go-mail sets fresh per-command deadlines. All refresh paths must preserve
// the earlier absolute operation deadline, including attempts to clear it.
func TestSMTPDeadlineRefreshCannotExtendOperation(t *testing.T) {
	for _, setter := range []string{"all", "read"} {
		for _, clear := range []bool{false, true} {
			t.Run(setter+"/"+map[bool]string{false: "extend", true: "clear"}[clear], func(t *testing.T) {
				client, peer := net.Pipe()
				defer client.Close()
				defer peer.Close()
				guard := time.AfterFunc(time.Second, func() { _ = client.Close() })
				defer guard.Stop()
				conn := &deadlineConn{Conn: client, deadline: time.Now().Add(30 * time.Millisecond)}
				refresh := time.Now().Add(time.Hour)
				if clear {
					refresh = time.Time{}
				}
				var err error
				if setter == "all" {
					err = conn.SetDeadline(refresh)
				} else {
					err = conn.SetReadDeadline(refresh)
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = conn.Read(make([]byte, 1))
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("got %v, want bounded read timeout", err)
				}
			})
		}
	}
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	guard := time.AfterFunc(time.Second, func() { _ = client.Close() })
	defer guard.Stop()
	conn := &deadlineConn{Conn: client, deadline: time.Now().Add(30 * time.Millisecond)}
	if err := conn.SetWriteDeadline(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("stalled write")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("got %v, want bounded write timeout", err)
	}
}
