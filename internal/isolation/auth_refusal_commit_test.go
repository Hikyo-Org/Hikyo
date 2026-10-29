package isolation

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/jackc/pgx/v5"
)

func TestRequestRefusalSnapshotWaitsForPostgresSettlement(t *testing.T) {
	db := seededDB(t, openPostgres)
	window := audit.NewRefusalWindow()
	if err := window.Add(refusalStorageSample(t)); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(window)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(t.Context())
	if _, err := writer.Exec(t.Context(), "UPDATE auth_refusal_window SET window_start=(EXTRACT(EPOCH FROM clock_timestamp())::bigint/60)*60, state=$1 WHERE id=1", string(state)); err != nil {
		t.Fatal(err)
	}
	type snapshot struct {
		state  string
		sealed int64
		err    error
	}
	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan snapshot, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		state, sealed, err := readPostgresRefusalAccounting(ctx, db)
		results <- snapshot{state, sealed, err}
	}()
	defer func() { _ = writer.Rollback(t.Context()); cancel(); <-done }()
	var result snapshot
	returned := false
	select {
	case result = <-results:
		returned = true
		t.Error("accounting snapshot returned before the outstanding writer settled")
	case <-time.After(100 * time.Millisecond):
	}
	if err := writer.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !returned {
		result = <-results
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := json.Unmarshal([]byte(result.state), &window); err != nil {
		t.Fatal(err)
	}
	if got := window.Counts[0] + result.sealed; got != 1 {
		t.Fatalf("snapshot missed the settled commit: %d", got)
	}
}

// A COMMIT acknowledgement can be lost after PostgreSQL durably applies the
// update. The failure must remain visible without discarding committed evidence.
func TestRequestRefusalLostPostgresCommitReceiptRetainsEvidence(t *testing.T) {
	u, err := url.Parse(postgresTestDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	address, loseNext, dropped := refusalCommitReceiptProxy(t, u.Host)
	u.Host = address
	t.Setenv("HIKYO_TEST_POSTGRES_DSN", u.String())
	db := seededDB(t, openPostgres)
	signals := service.NewRefusalSignals(db)
	sample := refusalStorageSample(t)
	loseNext.Store(true)
	if err := signals.Record(t.Context(), sample); err == nil {
		t.Fatal("lost commit receipt was reported as confirmed accounting")
	}
	select {
	case <-dropped:
	default:
		t.Fatal("fixture did not observe the committed transaction")
	}
	if health := signals.RefusalHealth(); health.Healthy || health.Failures != 1 {
		t.Fatalf("lost receipt hid accounting uncertainty: %+v", health)
	}
	committed := assertRefusalCommittedAccounting(t, db, 0, 1)
	if committed != 1 {
		t.Fatalf("lost acknowledgement discarded committed evidence: %d", committed)
	}
	expireRefusalWindow(t, db)
	waitForRefusalSeal(t, db, service.NewRefusalSignals(db), committed)
	assertRetainedRefusalRequest(t, db, sample.RequestID, sample.SourceIP)
}

func TestRequestRefusalCommitProxyPreservesTLSRequirement(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		name := "URL"
		if inherited {
			name = "environment"
		}
		t.Run(name, func(t *testing.T) {
			u, err := url.Parse(postgresTestDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			address, _, _ := refusalCommitReceiptProxy(t, u.Host)
			u.Host = address
			q := u.Query()
			if inherited {
				q.Del("sslmode")
				t.Setenv("PGSSLMODE", "require")
			} else {
				q.Set("sslmode", "require")
			}
			u.RawQuery = q.Encode()
			conn, err := pgx.Connect(t.Context(), u.String())
			if conn != nil {
				_ = conn.Close(t.Context())
			}
			if err == nil {
				t.Fatal("proxy downgraded a TLS-required connection to the plaintext fixture")
			}
		})
	}
}

// Forward PostgreSQL bytes unchanged, except for one armed COMMIT receipt.
// Drop that receipt only after receiving the server's CommandComplete, proving
// actual commit rather than guessing from a write attempt or elapsed time.
func refusalCommitReceiptProxy(t *testing.T, target string) (string, *atomic.Bool, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	loseNext := &atomic.Bool{}
	dropped := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
			if err != nil {
				_ = client.Close()
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				closed := make(chan struct{})
				stop := context.AfterFunc(ctx, func() {
					defer close(closed)
					_ = client.Close()
					_ = upstream.Close()
				})
				copied := make(chan struct{})
				defer func() {
					_ = client.Close()
					_ = upstream.Close()
					<-copied
					if !stop() {
						<-closed
					}
				}()
				if err := forwardRefusalPostgresStartup(client, upstream); err != nil {
					close(copied)
					return
				}
				go func() { defer close(copied); _, _ = io.Copy(upstream, client) }()
				for {
					var header [5]byte
					if _, err := io.ReadFull(upstream, header[:]); err != nil {
						return
					}
					size := binary.BigEndian.Uint32(header[1:])
					if size < 4 || size > 1<<20 {
						return
					}
					body := make([]byte, size-4)
					if _, err := io.ReadFull(upstream, body); err != nil {
						return
					}
					if header[0] == 'C' && string(body) == "COMMIT\x00" && loseNext.CompareAndSwap(true, false) {
						close(dropped)
						return
					}
					buffers := net.Buffers{header[:], body}
					if _, err := buffers.WriteTo(client); err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { cancel(); _ = listener.Close(); workers.Wait() })
	return listener.Addr().String(), loseNext, dropped
}

// Preserve the client's SSL/GSS negotiation. CI's unqualified URL uses the
// normal plaintext fallback after the disposable server replies N; a client
// requiring TLS must still refuse that response. Encrypted transports cannot
// enter the plaintext backend-message observer.
func forwardRefusalPostgresStartup(client, upstream net.Conn) error {
	for {
		var prefix [8]byte
		if _, err := io.ReadFull(client, prefix[:]); err != nil {
			return err
		}
		size := binary.BigEndian.Uint32(prefix[:4])
		if size < 8 || size > 1<<20 {
			return errors.New("invalid PostgreSQL fixture startup length")
		}
		buffers := net.Buffers{prefix[:]}
		if _, err := buffers.WriteTo(upstream); err != nil {
			return err
		}
		request := binary.BigEndian.Uint32(prefix[4:])
		if size != 8 || (request != 80877103 && request != 80877104) {
			return nil
		}
		var response [1]byte
		if _, err := io.ReadFull(upstream, response[:]); err != nil {
			return err
		}
		buffers = net.Buffers{response[:]}
		if _, err := buffers.WriteTo(client); err != nil {
			return err
		}
		if response[0] != 'N' {
			return errors.New("encrypted PostgreSQL transport cannot enter the commit-receipt observer")
		}
	}
}
