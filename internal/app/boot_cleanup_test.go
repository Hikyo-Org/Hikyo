package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestBootLogsEffectiveSQLitePoolSizes(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewTextHandler(&output, nil))
	resources := recordingBootResources(&bootResourceRecord{})
	injected := errors.New("stop after datastore startup")
	resources.listen = func(string, string) (net.Listener, error) { return nil, injected }

	_, err := boot(t.Context(), devConfig(t), log, resources)
	if !errors.Is(err, injected) {
		t.Fatalf("boot error = %v, want injected listener error", err)
	}
	logged := output.String()
	for _, want := range []string{
		"msg=\"datastore connection pools configured\"",
		"engine=sqlite",
		"write_max_connections=1",
		"read_max_connections=",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("startup log missing %q:\n%s", want, logged)
		}
	}
}

type bootResourceRecord struct {
	database       *store.DB
	listeners      []net.Listener
	databaseCloses int
	listenerCloses int
	closeOrder     []string
}

func recordingBootResources(record *bootResourceRecord) bootResources {
	resources := defaultBootResources()
	resources.openDatabase = func(ctx context.Context, cfg store.Config) (*store.DB, error) {
		db, err := store.Open(ctx, cfg)
		record.database = db
		return db, err
	}
	resources.closeDatabase = func(db *store.DB) error {
		record.databaseCloses++
		record.closeOrder = append(record.closeOrder, "database")
		return db.Close()
	}
	resources.listen = func(network, address string) (net.Listener, error) {
		ln, err := net.Listen(network, address)
		record.listeners = append(record.listeners, ln)
		return ln, err
	}
	resources.closeListener = func(ln net.Listener) error {
		record.listenerCloses++
		record.closeOrder = append(record.closeOrder, "listener")
		return ln.Close()
	}
	return resources
}

func TestBootResourceOwnershipOnFailure(t *testing.T) {
	injected := errors.New("injected constructor failure")

	t.Run("before database acquisition closes nothing", func(t *testing.T) {
		record := &bootResourceRecord{}
		resources := recordingBootResources(record)
		resources.openDatabase = func(context.Context, store.Config) (*store.DB, error) {
			return nil, injected
		}

		_, err := boot(t.Context(), devConfig(t), testLogger(), resources)
		if !errors.Is(err, injected) {
			t.Fatalf("boot error = %v, want injected database-open error", err)
		}
		if record.databaseCloses != 0 || record.listenerCloses != 0 {
			t.Fatalf("close counts = database %d, listener %d; want 0, 0", record.databaseCloses, record.listenerCloses)
		}
	})

	t.Run("after database acquisition closes only database", func(t *testing.T) {
		record := &bootResourceRecord{}
		resources := recordingBootResources(record)
		resources.listen = func(string, string) (net.Listener, error) {
			return nil, injected
		}

		_, err := boot(t.Context(), devConfig(t), testLogger(), resources)
		if !errors.Is(err, injected) {
			t.Fatalf("boot error = %v, want injected listener error", err)
		}
		if record.databaseCloses != 1 || record.listenerCloses != 0 {
			t.Fatalf("close counts = database %d, listener %d; want 1, 0", record.databaseCloses, record.listenerCloses)
		}
		if want := []string{"database"}; !slices.Equal(record.closeOrder, want) {
			t.Fatalf("close order = %v, want %v", record.closeOrder, want)
		}
	})

	t.Run("second listener failure closes public listener then database", func(t *testing.T) {
		record := &bootResourceRecord{}
		resources := recordingBootResources(record)
		listen := resources.listen
		calls := 0
		resources.listen = func(network, address string) (net.Listener, error) {
			calls++
			if calls == 2 {
				return nil, injected
			}
			return listen(network, address)
		}

		_, err := boot(t.Context(), devConfig(t), testLogger(), resources)
		if !errors.Is(err, injected) {
			t.Fatalf("boot error = %v, want injected operational-listener error", err)
		}
		if record.databaseCloses != 1 || record.listenerCloses != 1 {
			t.Fatalf("close counts = database %d, listener %d; want 1, 1", record.databaseCloses, record.listenerCloses)
		}
		if want := []string{"listener", "database"}; !slices.Equal(record.closeOrder, want) {
			t.Fatalf("close order = %v, want %v", record.closeOrder, want)
		}
	})

	t.Run("after listener acquisition closes listener then database", func(t *testing.T) {
		record := &bootResourceRecord{}
		resources := recordingBootResources(record)
		resources.newDirectoryClient = func(remotefetch.Config) (*remotefetch.Client, error) {
			return nil, injected
		}

		_, err := boot(t.Context(), devConfig(t), testLogger(), resources)
		if !errors.Is(err, injected) {
			t.Fatalf("boot error = %v, want injected directory-client error", err)
		}
		if record.databaseCloses != 1 || record.listenerCloses != 2 {
			t.Fatalf("close counts = database %d, listener %d; want 1, 2", record.databaseCloses, record.listenerCloses)
		}
		if want := []string{"listener", "listener", "database"}; !slices.Equal(record.closeOrder, want) {
			t.Fatalf("close order = %v, want %v", record.closeOrder, want)
		}
	})
}

func TestBootWarmsOpenAPIBeforeListening(t *testing.T) {
	resources := defaultBootResources()
	warmed := false
	resources.warmOpenAPI = func() error {
		warmed = true
		return nil
	}
	injected := errors.New("stop after OpenAPI warmup")
	resources.listen = func(string, string) (net.Listener, error) {
		if !warmed {
			t.Fatal("listener opened before the OpenAPI document was warm")
		}
		return nil, injected
	}

	_, err := boot(t.Context(), devConfig(t), testLogger(), resources)
	if !errors.Is(err, injected) {
		t.Fatalf("boot error = %v, want injected listener error", err)
	}
}

func TestBootSuccessTransfersResourceOwnershipToServer(t *testing.T) {
	record := &bootResourceRecord{}
	srv, err := boot(t.Context(), devConfig(t), testLogger(), recordingBootResources(record))
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = srv.Close()
		}
	})

	if record.databaseCloses != 0 || record.listenerCloses != 0 {
		t.Fatalf("guard close counts after success = database %d, listener %d; want 0, 0", record.databaseCloses, record.listenerCloses)
	}
	if err := record.database.Ping(t.Context()); err != nil {
		t.Fatalf("database was closed before Server ownership: %v", err)
	}
	if competing, err := net.Listen("tcp", srv.Addr); err == nil {
		competing.Close()
		t.Fatal("listener was closed before Server ownership")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Server.Close: %v", err)
	}
	closed = true
	if err := record.database.Ping(t.Context()); err == nil {
		t.Fatal("database remained open after Server.Close")
	}
	rebound, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatalf("listener remained open after Server.Close: %v", err)
	}
	rebound.Close()
}

func TestBootGuard(t *testing.T) {
	t.Run("cleans in reverse registration order", func(t *testing.T) {
		var order []string
		g := &bootGuard{log: testLogger()}
		g.add(func() error { order = append(order, "db"); return nil })
		g.add(func() error { order = append(order, "ln"); return nil })
		g.cleanup()
		if want := []string{"ln", "db"}; !slices.Equal(order, want) {
			t.Fatalf("cleanup order = %v, want %v (listener before database)", order, want)
		}
	})

	t.Run("cleanup is idempotent", func(t *testing.T) {
		var n int
		g := &bootGuard{log: testLogger()}
		g.add(func() error { n++; return nil })
		g.cleanup()
		g.cleanup()
		if n != 1 {
			t.Fatalf("closer ran %d times across two cleanups, want 1", n)
		}
	})

	t.Run("disarm prevents cleanup", func(t *testing.T) {
		var n int
		g := &bootGuard{log: testLogger()}
		g.add(func() error { n++; return nil })
		g.disarm()
		g.cleanup()
		if n != 0 {
			t.Fatalf("disarmed guard ran %d closers, want 0", n)
		}
	})

	t.Run("a failing closer does not stop the rest", func(t *testing.T) {
		var closed int
		g := &bootGuard{log: testLogger()}
		g.add(func() error { closed++; return nil })
		g.add(func() error { closed++; return errors.New("boom") })
		g.cleanup()
		if closed != 2 {
			t.Fatalf("closed %d resources, want 2 (a cleanup failure must not abort the rest)", closed)
		}
	})
}
