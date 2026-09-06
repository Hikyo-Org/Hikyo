package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/server"
)

type stalledReadyChecker struct {
	started  chan struct{}
	finished chan error
}

func (s stalledReadyChecker) Ready(ctx context.Context) error {
	close(s.started)
	select {
	case <-ctx.Done():
		s.finished <- ctx.Err()
		return ctx.Err()
	case <-time.After(4 * time.Second):
		return errors.New("datastore connection attempt failed")
	}
}

func TestReadinessStalledDatastoreReturns503BeforeWriteDeadline(t *testing.T) {
	checker := stalledReadyChecker{started: make(chan struct{}), finished: make(chan error, 1)}
	srv := httptest.NewUnstartedServer(server.NewOperational(checker, nil, nil))
	// Compress the production socket deadline while preserving its ordering:
	// an unbounded datastore attempt finishes after the response deadline.
	srv.Config.WriteTimeout = 3 * time.Second
	srv.Start()
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.Timeout = 5 * time.Second
	healthResult := make(chan error, 1)
	go func() {
		select {
		case <-checker.started:
		case <-t.Context().Done():
			healthResult <- t.Context().Err()
			return
		}
		response, err := client.Get(srv.URL + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = errors.New("liveness failed during stalled readiness")
			}
		}
		healthResult <- err
	}()
	response, err := client.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("readiness dropped connection instead of returning 503: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness status=%d, want503", response.StatusCode)
	}
	if err := <-checker.finished; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readiness cancellation=%v, want deadline exceeded", err)
	}
	if err := <-healthResult; err != nil {
		t.Fatal(err)
	}
}

func TestReadinessCanceledRequestCannotReportSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	server.NewOperational(stubReady{}, nil, nil).ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("canceled readiness status=%d, want503", response.Code)
	}
}
