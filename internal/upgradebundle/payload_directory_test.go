//go:build !windows

package upgradebundle

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"testing"
	"time"
)

func TestPayloadDirectoryFIFORefusesWithoutExternalWriter(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(dir+"/payloads", 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	reader := documentReader{ctx: ctx, root: root}
	done := make(chan error, 1)
	go func() { _, _, err := reader.payloads("payloads"); done <- err }()
	defer cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO admitted as payload directory")
		}
		return
	case <-time.After(150 * time.Millisecond):
	}
	// Supply a writer solely to release the blocked test goroutine.
	fd, err := unix.Open(dir+"/payloads", unix.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(fd)
	<-done
	t.Fatal("FIFO payload directory ignored cancellation and blocked until an external writer opened")
}
