package isolation

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	dynamicpg "github.com/Hikyo-Org/hikyo/internal/dynamic/postgres"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The executor against a real PostgreSQL target (the same separate TLS target the
// provider integration suite uses): a candidate role is created and can log in
// with the delivered credential, the superseded role is dropped after the
// overlap, and a role that owns objects is refused loudly until the objects move.
// Skipped unless HIKYO_TEST_DYNAMIC_PG_* is set, and required where
// HIKYO_DYNAMIC_PG_REQUIRED is.
func TestRotationExecutorRealPostgres(t *testing.T) {
	origin := os.Getenv("HIKYO_TEST_DYNAMIC_PG_DSN")
	if origin == "" {
		if os.Getenv("HIKYO_DYNAMIC_PG_REQUIRED") != "" {
			t.Fatal("HIKYO_TEST_DYNAMIC_PG_DSN required")
		}
		t.Skip("set HIKYO_TEST_DYNAMIC_PG_DSN (+ _PASSWORD, _CA, _GRANT_ROLE) for the real provider proof")
	}
	password := os.Getenv("HIKYO_TEST_DYNAMIC_PG_PASSWORD")
	grantRole := os.Getenv("HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE")
	caPath := os.Getenv("HIKYO_TEST_DYNAMIC_PG_CA")
	if password == "" || grantRole == "" {
		t.Fatal("dynamic PostgreSQL password and grant role required")
	}
	var roots *x509.CertPool
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			t.Fatal(err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("provider CA has no certificates")
		}
	}
	cidrs := os.Getenv("HIKYO_TEST_DYNAMIC_PG_ALLOW_CIDRS")
	if cidrs == "" {
		cidrs = "127.0.0.1/32,::1/128"
	}
	var allowed []netip.Prefix
	for _, raw := range strings.Split(cidrs, ",") {
		cidr, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			t.Fatal(err)
		}
		allowed = append(allowed, cidr)
	}
	factory := func(_ dynamic.Kind, origin, _ string, password string) (dynamic.Provider, error) {
		return dynamicpg.New(dynamicpg.Config{Origin: origin, Password: password, RootCAs: roots, AllowedCIDRs: allowed, Deadline: 5 * time.Second})
	}
	target := realPGTarget{origin: origin, password: password, caPath: caPath, factory: factory}
	forEngines(t, func(t *testing.T, db *store.DB) { runRotationRealPostgres(t, db, target, grantRole) })
}

type realPGTarget struct {
	origin, password, caPath string
	factory                  dynamic.Factory
}

// login opens a verified TLS session as the delivered credential and reports the
// failure, if any.
func (r realPGTarget) login(ctx context.Context, user, pass string) error {
	u, err := url.Parse(r.origin)
	if err != nil {
		return err
	}
	u.User = url.UserPassword(user, pass)
	q := u.Query()
	q.Set("sslmode", "verify-full")
	if r.caPath != "" {
		q.Set("sslrootcert", r.caPath)
	}
	u.RawQuery = q.Encode()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return err
	}
	return conn.Close(ctx)
}

func (r realPGTarget) admin(ctx context.Context, t *testing.T, sql string) {
	t.Helper()
	conn, err := r.adminConn(ctx)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("admin %q: %v", sql, err)
	}
}

func (r realPGTarget) adminConn(ctx context.Context) (*pgx.Conn, error) {
	u, err := url.Parse(r.origin)
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword(u.User.Username(), r.password)
	q := u.Query()
	q.Set("sslmode", "verify-full")
	if r.caPath != "" {
		q.Set("sslrootcert", r.caPath)
	}
	u.RawQuery = q.Encode()
	return pgx.Connect(ctx, u.String())
}

// persistedCandidates lists the candidate role of every run the rotation ever
// persisted. It reads through the store handle with its own context, because the
// test context is already canceled when cleanups run; a failed read is reported,
// since it could hide a role left on the target.
func persistedCandidates(ctx context.Context, t *testing.T, db *store.DB, rotationID string) []string {
	t.Helper()
	const q = "SELECT candidate_handle FROM rotation_runs WHERE rotation_id=%s AND candidate_handle<>''"
	var out []string
	if db.Engine() == store.EnginePostgres {
		rows, err := db.PG().Query(ctx, fmt.Sprintf(q, "$1"), rotationID)
		if err != nil {
			t.Errorf("list candidate roles: %v", err)
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				t.Errorf("list candidate roles: %v", err)
				return out
			}
			out = append(out, h)
		}
		if err := rows.Err(); err != nil {
			t.Errorf("list candidate roles: %v", err)
		}
		return out
	}
	rows, err := db.SQLiteRead().QueryContext(ctx, fmt.Sprintf(q, "?"), rotationID)
	if err != nil {
		t.Errorf("list candidate roles: %v", err)
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Errorf("list candidate roles: %v", err)
			return out
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		t.Errorf("list candidate roles: %v", err)
	}
	return out
}

func runRotationRealPostgres(t *testing.T, db *store.DB, target realPGTarget, grantRole string) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	x.exec.ProviderFactory = target.factory
	real, err := x.dyn.Configure(ctx, service.LocalPrincipal(alice), prjScope(), service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: target.origin, GrantRole: grantRole, Credential: []byte(target.password),
	})
	if err != nil {
		t.Fatalf("configure the real provider: %v", err)
	}
	req := x.request(rotPasswordKey, rotUsernameKey)
	req.ProviderID = real.ID
	rot, err := x.svc.Create(ctx, x.actor, x.scope, req)
	if err != nil {
		t.Fatalf("create rotation: %v", err)
	}
	probe, err := target.factory(dynamic.KindPostgres, target.origin, "verify-full", target.password)
	if err != nil {
		t.Fatal(err)
	}
	// The owned-object table is named per invocation and dropped only when this
	// invocation created it, so two runs against one target never delete each
	// other's fixture.
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	ownedTable := pgx.Identifier{"hikyo_rot_owned_" + hex.EncodeToString(suffix)}.Sanitize()
	ownedCreated := false
	var roles []string
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if ownedCreated {
			if conn, err := target.adminConn(cleanup); err == nil {
				_, _ = conn.Exec(cleanup, "DROP TABLE IF EXISTS "+ownedTable)
				_ = conn.Close(cleanup)
			}
		}
		// Every candidate this test's runs ever named, published or not: a run that
		// created its role and then failed to settle never reaches `delivered`.
		for _, name := range persistedCandidates(cleanup, t, db, rot.ID) {
			if !slices.Contains(roles, name) {
				roles = append(roles, name)
			}
		}
		for _, name := range roles {
			if err := probe.DropRole(cleanup, name); err != nil {
				t.Errorf("drop %s: %v", name, err)
			}
		}
		probe.Close()
	})
	parsed, err := url.Parse(target.origin)
	if err != nil {
		t.Fatal(err)
	}
	adminUser := parsed.User.Username()
	auth := authService(t, db)
	auth.ReauthWindow = 15 * time.Minute
	values := &service.Values{DB: db, Keyring: probeKeyring(t, db), Auth: auth}
	delivered := func() (string, string) {
		t.Helper()
		user, err := values.Get(ctx, x.actor, x.scope, "ROT_DB_USER", false)
		if err != nil {
			t.Fatal(err)
		}
		pass, err := values.Get(ctx, x.actor, x.scope, "ROT_DB_PASSWORD", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(roles) == 0 || roles[len(roles)-1] != user.Value {
			roles = append(roles, user.Value) // every delivered role is dropped on cleanup
		}
		return user.Value, pass.Value
	}

	// Cycle 1: a real role, delivered and usable.
	x.advance(24*time.Hour + time.Minute)
	x.drain()
	user1, pass1 := delivered()
	if err := target.login(ctx, user1, pass1); err != nil {
		t.Fatalf("the delivered credential cannot log in: %v", err)
	}
	if st, err := probe.RoleStatus(ctx, user1); err != nil || !st.Exists || st.ValidUntil.IsZero() {
		t.Fatalf("role status %+v, %v", st, err)
	}
	x.advance(time.Hour + time.Minute)
	x.drain()

	// Cycle 2: the new credential works, the superseded one is dropped for real.
	x.advance(24 * time.Hour)
	x.drain()
	user2, pass2 := delivered()
	if user2 == user1 {
		t.Fatal("a second cycle delivered the same role")
	}
	if err := target.login(ctx, user2, pass2); err != nil {
		t.Fatalf("second credential cannot log in: %v", err)
	}
	if err := target.login(ctx, user1, pass1); err != nil {
		t.Fatalf("the superseded credential must keep working through the overlap: %v", err)
	}
	// The superseded role owns an object: the drop is refused and named, not lost.
	target.admin(ctx, t, "CREATE TABLE "+ownedTable+" (id int)")
	ownedCreated = true
	target.admin(ctx, t, "ALTER TABLE "+ownedTable+" OWNER TO "+pgx.Identifier{user1}.Sanitize())
	x.advance(time.Hour + time.Minute)
	x.drain()
	run2 := x.openRunID(rot.ID)
	if got := x.phase(run2); got != "revoke_refused" {
		t.Fatalf("a superseded role that owns objects: %s, want revoke_refused", got)
	}
	if got := x.lastRun(rot.ID, "last_error"); got != "role_owns_objects" {
		t.Fatalf("last_error = %q", got)
	}
	if st, err := probe.RoleStatus(ctx, user1); err != nil || !st.Exists {
		t.Fatalf("the refused revoke lost the role: %+v, %v", st, err)
	}
	// Move the object away; the next retry drops the role.
	target.admin(ctx, t, "ALTER TABLE "+ownedTable+" OWNER TO "+pgx.Identifier{adminUser}.Sanitize())
	x.advance(24 * time.Hour)
	x.drain()
	if got := x.phase(run2); got != "done" {
		t.Fatalf("after the object moved: %s, want done", got)
	}
	if st, err := probe.RoleStatus(ctx, user1); err != nil || st.Exists {
		t.Fatalf("the superseded role survived: %+v, %v", st, err)
	}
	if err := target.login(ctx, user1, pass1); err == nil {
		t.Fatal("the dropped role can still log in")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := target.login(ctx, user2, pass2); err != nil {
		t.Fatalf("the delivered credential stopped working: %v", err)
	}
	delivered() // the next cycle's candidate is dropped on cleanup too
}
