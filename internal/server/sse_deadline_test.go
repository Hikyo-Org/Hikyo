package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines     []time.Time
	deadlineError error
}

func (w *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return w.deadlineError
}

func TestEventStreamBoundsEveryFrameThroughMiddleware(t *testing.T) {
	events := make(chan service.AdvisoryEvent, 2)
	events <- service.AdvisoryEvent{Type: "revision"}
	events <- service.AdvisoryEvent{Type: "revision"}
	close(events)
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	start := time.Now()
	stream := eventStream{ctx: t.Context(), events: events, retry: advisoryRetryBase}
	if err := stream.VisitWatchProjectEventsResponse(newResponseWriter(newResponseWriter(w))); err != nil {
		t.Fatal(err)
	}
	if len(w.deadlines) != 6 || !w.Flushed {
		t.Fatalf("frame deadlines = %d, flushed = %t", len(w.deadlines), w.Flushed)
	}
	for index, deadline := range w.deadlines {
		if index%2 == 1 {
			if !deadline.IsZero() {
				t.Fatal("completed frame kept an idle deadline")
			}
			continue
		}
		if deadline.Before(start.Add(30*time.Second)) || deadline.After(time.Now().Add(30*time.Second)) {
			t.Fatalf("deadline does not bound the frame to 30s: %s", deadline)
		}
	}
}

func TestEventStreamRefusesUnboundedWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stream := eventStream{ctx: ctx, events: make(chan service.AdvisoryEvent)}
	for _, w := range []http.ResponseWriter{
		httptest.NewRecorder(),
		&deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), deadlineError: errors.New("deadline unavailable")},
	} {
		if err := stream.VisitWatchProjectEventsResponse(w); err == nil {
			t.Fatal("stream accepted a writer without an enforceable deadline")
		}
	}
}

func TestStalledEventStreamDisconnectsAtWriteDeadline(t *testing.T) {
	done := make(chan time.Duration, 1)
	events := make(chan service.AdvisoryEvent, 2048)
	// Repeated metadata fills the socket's buffers without retaining a separate
	// large string for every event. The peer deliberately never reads the body.
	name := strings.Repeat("a", 64<<10)
	for range cap(events) {
		events <- service.AdvisoryEvent{Type: "revision", KeyID: "key", KeyName: name}
	}
	close(events)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		stream := eventStream{ctx: r.Context(), events: events, retry: advisoryRetryBase}
		_ = stream.VisitWatchProjectEventsResponse(newResponseWriter(w))
		done <- time.Since(start)
	}))
	defer srv.Close()
	addr, err := net.ResolveTCPAddr("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(40 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	select {
	case elapsed := <-done:
		if elapsed < 30*time.Second || elapsed > 35*time.Second {
			t.Fatalf("stalled peer release = %s, want 30s deadline plus scheduling tolerance", elapsed)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("stalled peer retained its handler past the write deadline")
	}
}

func TestHTTP2EventStreamSurvivesIdleHeartbeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream := eventStream{ctx: r.Context(), events: make(chan service.AdvisoryEvent), retry: advisoryRetryBase}
		_ = stream.VisitWatchProjectEventsResponse(newResponseWriter(w))
	}))
	srv.EnableHTTP2 = true
	srv.Config.WriteTimeout = ResponseWriteTimeout
	srv.StartTLS()
	defer srv.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("fixture did not negotiate HTTP/2: %s", resp.Proto)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("healthy HTTP/2 stream ended before heartbeat: %v", err)
		}
		if line == ": heartbeat\n" {
			return
		}
	}
}
