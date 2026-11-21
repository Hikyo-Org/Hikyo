package shell

import (
	"io"
	"sync"
	"time"
)

// downloadIdle ends a download whose server stops sending. A download that
// keeps making progress can run as long as it needs.
var downloadIdle = time.Minute

// idleReader calls onIdle if no byte arrives for d. onIdle cancels the request,
// which unblocks the pending Read with an error.
type idleReader struct {
	r     io.Reader
	d     time.Duration
	mu    sync.Mutex
	timer *time.Timer
}

func newIdleReader(r io.Reader, d time.Duration, onIdle func()) *idleReader {
	return &idleReader{r: r, d: d, timer: time.AfterFunc(d, onIdle)}
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	i.mu.Lock()
	defer i.mu.Unlock()
	if err != nil {
		i.timer.Stop()
	} else if n > 0 {
		i.timer.Reset(i.d)
	}
	return n, err
}

// stop ends the watch; call it when the read is over.
func (i *idleReader) stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.timer.Stop()
}
