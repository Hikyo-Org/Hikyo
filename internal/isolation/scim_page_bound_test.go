package isolation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	"modernc.org/sqlite"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/scimproto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Count rows delivered by the real SQLite driver, before sqlc or repository
// conversion can slice or discard them. The counter belongs to this fixture.
type scimRowsDriver struct {
	driver.Driver
	rows atomic.Int64
}
type scimRowsConn struct {
	driver.Conn
	counter *atomic.Int64
}
type scimCountedRows struct {
	driver.Rows
	counter *atomic.Int64
}

var scimDriverSequence atomic.Uint64

func (d *scimRowsDriver) Open(name string) (driver.Conn, error) {
	c, e := d.Driver.Open(name)
	if e != nil {
		return nil, e
	}
	return &scimRowsConn{c, &d.rows}, nil
}
func (c *scimRowsConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *scimRowsConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}
func (c *scimRowsConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	r, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
	if e != nil {
		return nil, e
	}
	cols := strings.Join(r.Columns(), " ")
	if strings.Contains(cols, "user_name_lower") || strings.Contains(cols, "display_name_lower") {
		return &scimCountedRows{r, c.counter}, nil
	}
	return r, nil
}
func (r *scimCountedRows) Next(v []driver.Value) error {
	e := r.Rows.Next(v)
	if e == nil {
		r.counter.Add(1)
	}
	return e
}

func openSCIMCountedSQLite(t *testing.T) (*store.DB, *atomic.Int64) {
	t.Helper()
	d := &scimRowsDriver{Driver: &sqlite.Driver{}}
	name := fmt.Sprintf("hikyo-scim-rows-%d", scimDriverSequence.Add(1))
	sql.Register(name, d)
	db := seededDB(t, func(t *testing.T) *store.DB {
		db, e := openIsolationFixture(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "scim.db"), SQLiteDriver: name})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { db.Close() })
		return db
	})
	return db, &d.rows
}

// Raw synthetic directory rows isolate list cost from 5000 provisioning
// ceremonies; principal/account identities are distinct and all FK/check
// constraints remain enabled. Binding and credential are provisioned normally.
func seedSCIMPageDirectory(t *testing.T, db *store.DB, binding, prefix string, n int) {
	t.Helper()
	var principals, accounts, users, groups []string
	active := "1"
	if db.PG() != nil {
		active = "true"
	}
	for i := range n {
		id := fmt.Sprintf("%s%06d", prefix, i)
		principals = append(principals, fmt.Sprintf("('%s','human',%s)", id, ts))
		accounts = append(accounts, fmt.Sprintf("('%s','%s','%s@example.test','Paging',%s)", id, id, id, ts))
		users = append(users, fmt.Sprintf("('%s','%s','%s','%s','%s@example.test','%s@example.test','SameCase','%s',%s,'{}',%s,%s)", id, orgA, binding, id, id, id, id, active, ts, ts))
		groups = append(groups, fmt.Sprintf("('%s','%s','%s','GrÜppe','grüppe','SameCase',%s,%s)", id, orgA, binding, ts, ts))
	}
	execRaw(t, db, "INSERT INTO principals(id,kind,created_at) VALUES "+strings.Join(principals, ","))
	execRaw(t, db, "INSERT INTO accounts(id,principal_id,username,display_name,created_at) VALUES "+strings.Join(accounts, ","))
	execRaw(t, db, "INSERT INTO scim_users(id,org_id,binding_id,account_id,user_name,user_name_lower,external_id,subject,active,attributes,created_at,updated_at) VALUES "+strings.Join(users, ","))
	execRaw(t, db, "INSERT INTO scim_groups(id,org_id,binding_id,display_name,display_name_lower,external_id,created_at,updated_at) VALUES "+strings.Join(groups, ","))
}

// PostgreSQL's test-only protocol observer counts DataRows whose actual
// RowDescription is a directory resource. It discards all message bytes and
// never stores or logs row values. Simple protocol ensures every result has its
// own descriptor; a one-connection fixture prevents unobserved spare sessions.
type scimPGRowCounter struct {
	mu       sync.Mutex
	resource bool
	rows     atomic.Int64
}

func (c *scimPGRowCounter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := strings.SplitN(string(p), "\t", 4)
	if len(parts) >= 3 && parts[0] == "B" {
		switch parts[1] {
		case "RowDescription":
			c.resource = strings.Contains(string(p), `"user_name_lower"`) || strings.Contains(string(p), `"display_name_lower"`)
		case "DataRow":
			if c.resource {
				c.rows.Add(1)
			}
		case "CommandComplete", "ReadyForQuery":
			c.resource = false
		}
	}
	return len(p), nil
}
func openSCIMCountedPostgres(t *testing.T) (*store.DB, *atomic.Int64) {
	t.Helper()
	u, e := url.Parse(postgresTestDSN(t))
	if e != nil {
		t.Fatal("invalid test DSN")
	}
	q := u.Query()
	q.Set("default_query_exec_mode", "simple_protocol")
	u.RawQuery = q.Encode()
	t.Setenv("HIKYO_TEST_POSTGRES_DSN", u.String())
	db := seededDB(t, openPostgres)
	// Hold every spare pool slot so the observed physical connection is the
	// only one available to the real service call.
	var spares []*pgxpool.Conn
	t.Cleanup(func() {
		for _, held := range spares {
			held.Release()
		}
	})
	for i := int32(1); i < db.PG().Config().MaxConns; i++ {
		held, e := db.PG().Acquire(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		spares = append(spares, held)
	}
	c := &scimPGRowCounter{}
	held, e := db.PG().Acquire(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	held.Conn().PgConn().Frontend().Trace(c, pgproto3.TracerOptions{SuppressTimestamps: true})
	held.Release()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		held, e := db.PG().Acquire(ctx)
		if e != nil {
			t.Error(e)
			return
		}
		held.Conn().PgConn().Frontend().Untrace()
		held.Release()
	})
	return db, &c.rows
}

func TestSCIMWirePageMaterializationBound(t *testing.T) {
	for _, engine := range []struct {
		name string
		open func(*testing.T) (*store.DB, *atomic.Int64)
	}{{"sqlite", openSCIMCountedSQLite}, {"postgres", openSCIMCountedPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			db, counter := engine.open(t)
			binding, token := newSCIMBinding(t, db, "paging")
			other, otherToken := newSCIMBinding(t, db, "other-paging")
			seedSCIMPageDirectory(t, db, binding, "page", 5000)
			seedSCIMPageDirectory(t, db, other, "other", 2)
			wire := service.SCIMCredentialActor(token, binding)
			svc := scimSvc(db)
			for _, resource := range []string{"users", "groups"} {
				t.Run(resource, func(t *testing.T) {
					list := func(actor service.Actor, org domain.OrgID, b string, f scimproto.Filter, p scimproto.Page) ([]string, int, error) {
						ids := []string{}
						if resource == "users" {
							got, n, e := svc.ListUsers(t.Context(), actor, org, b, f, p)
							for _, r := range got {
								ids = append(ids, r.ID)
							}
							return ids, n, e
						}
						got, n, e := svc.ListGroups(t.Context(), actor, org, b, f, p)
						for _, r := range got {
							ids = append(ids, r.ID)
						}
						return ids, n, e
					}
					type pageCase struct {
						name                               string
						filter                             scimproto.Filter
						start, count, total, first, length int
					}
					cases := []pageCase{
						{name: "first", start: 1, count: 10, total: 5000, length: 10},
						{name: "next", start: 11, count: 10, total: 5000, first: 10, length: 10},
						{name: "last", start: 4996, count: 10, total: 5000, first: 4995, length: 5},
						{name: "out-of-range", start: 5001, count: 10, total: 5000},
						{name: "max-offset", start: math.MaxInt, count: 10, total: 5000},
						{name: "zero-count", start: 1, count: 0, total: 5000},
						{name: "negative-count", start: 1, count: -1, total: 5000},
						{name: "negative-start", start: -5, count: 10, total: 5000, length: 10},
						{name: "clamped-count", start: 1, count: math.MaxInt, total: 5000, length: svc.PageBound()},
						{name: "external-id", filter: scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: "SameCase"}, start: 1, count: 10, total: 5000, length: 10},
						{name: "external-id-case-sensitive", filter: scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: "samecase"}, start: 1, count: 10},
						{name: "external-id-empty", filter: scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: ""}, start: 1, count: 10},
						{name: "external-id-late-page", filter: scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: "SameCase"}, start: 4996, count: 10, total: 5000, first: 4995, length: 5},
					}
					if resource == "users" {
						cases = append(cases, pageCase{name: "folded-username", filter: scimproto.Filter{Shape: scimproto.FilterUserNameEq, Value: "PAGE000012@EXAMPLE.TEST"}, start: 1, count: 10, total: 1, first: 12, length: 1}, pageCase{name: "username-zero-count", filter: scimproto.Filter{Shape: scimproto.FilterUserNameEq, Value: "PAGE000012@EXAMPLE.TEST"}, start: 1, count: 0, total: 1})
					} else {
						cases = append(cases, pageCase{name: "folded-unicode-display-name", filter: scimproto.Filter{Shape: scimproto.FilterDisplayNameEq, Value: "GRÜPPE"}, start: 1, count: 10, total: 5000, length: 10}, pageCase{name: "display-name-out-of-range", filter: scimproto.Filter{Shape: scimproto.FilterDisplayNameEq, Value: "GRÜPPE"}, start: 5001, count: 10, total: 5000})
					}
					for _, tc := range cases {
						t.Run(tc.name, func(t *testing.T) {
							f := tc.filter
							if f.Shape == "" {
								f.Shape = scimproto.FilterNone
							}
							counter.Store(0)
							got, total, e := list(wire, orgA, binding, f, scimproto.Page{StartIndex: tc.start, Count: tc.count})
							if e != nil {
								t.Fatal(e)
							}
							want := []string{}
							for i := range tc.length {
								want = append(want, fmt.Sprintf("page%06d", tc.first+i))
							}
							if total != tc.total || !slices.Equal(got, want) {
								t.Fatalf("total=%d/%d, ordered result length=%d/%d", total, tc.total, len(got), len(want))
							}
							if rows := counter.Load(); rows != int64(tc.length) {
								t.Fatalf("materialized %d resource rows for requested page; want exactly %d", rows, tc.length)
							}
						})
					}
					for _, f := range []scimproto.Filter{{Shape: scimproto.FilterNone}, {Shape: scimproto.FilterExternalIDEq, Value: "SameCase"}} {
						counter.Store(0)
						got, total, e := list(service.SCIMCredentialActor(otherToken, other), orgA, other, f, scimproto.Page{StartIndex: 1, Count: 10})
						if e != nil || total != 2 || !slices.Equal(got, []string{"other000000", "other000001"}) || counter.Load() != 2 {
							t.Fatalf("binding-scoped page failed: total%d rows%d err%v", total, counter.Load(), e)
						}
						counter.Store(0)
						if _, _, e := list(service.SCIMCredentialActor(token, other), orgA, other, f, scimproto.Page{StartIndex: 1, Count: 10}); e == nil {
							t.Fatal("credential crossed binding")
						}
						if counter.Load() != 0 {
							t.Fatal("refused credential materialized directory rows")
						}
						if _, _, e := list(wire, orgB, binding, f, scimproto.Page{StartIndex: 1, Count: 10}); e == nil {
							t.Fatal("credential crossed organization")
						}
					}
					unsupported := scimproto.FilterDisplayNameEq
					if resource == "groups" {
						unsupported = scimproto.FilterUserNameEq
					}
					if _, _, e := list(wire, orgA, binding, scimproto.Filter{Shape: unsupported, Value: "SameCase"}, scimproto.Page{StartIndex: 1, Count: 10}); !errors.Is(e, domain.ErrInvalid) {
						t.Fatalf("unsupported filter: %v", e)
					}
				})
			}
		})
	}
}

// Fixed-page service benchmark over two directory sizes. It measures actual
// authenticated/audited reads, not a preloaded slice or a query in isolation.
// No timing threshold is asserted on shared CI hosts; correctness and the
// physical row ceiling are mandatory in TestSCIMWirePageMaterializationBound.
func TestSCIMFixedPageBenchmark(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, size := range []int{100, 5000} {
			t.Run(fmt.Sprintf("directory-%d", size), func(t *testing.T) {
				binding, token := newSCIMBinding(t, db, fmt.Sprintf("bench-%d", size))
				seedSCIMPageDirectory(t, db, binding, fmt.Sprintf("bench%d-", size), size)
				actor := service.SCIMCredentialActor(token, binding)
				for _, resource := range []string{"users", "groups"} {
					t.Run(resource, func(t *testing.T) {
						var failed atomic.Bool
						result := testing.Benchmark(func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								var total, length int
								var err error
								if resource == "users" {
									var got []service.SCIMUserResource
									got, total, err = scimSvc(db).ListUsers(b.Context(), actor, orgA, binding, scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: "SameCase"}, scimproto.Page{StartIndex: 1, Count: 10})
									length = len(got)
								} else {
									var got []service.SCIMGroupResource
									got, total, err = scimSvc(db).ListGroups(b.Context(), actor, orgA, binding, scimproto.Filter{Shape: scimproto.FilterExternalIDEq, Value: "SameCase"}, scimproto.Page{StartIndex: 1, Count: 10})
									length = len(got)
								}
								if err != nil || total != size || length != 10 {
									failed.Store(true)
									b.Fatalf("page result total%d length%d err%v", total, length, err)
								}
							}
						})
						t.Logf("fixed count10 directory%d %s: %s %s", size, resource, result.String(), result.MemString())
						if failed.Load() || result.N == 0 {
							t.Fatal("benchmark did not execute")
						}
					})
				}
			})
		}
	})
}
