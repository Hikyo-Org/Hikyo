// Package postgres implements the dynamic-secret Provider seam over a
// PostgreSQL engine. It mints a login role IN ROLE the operator's grant role,
// VALID UNTIL the lease expiry (so the engine enforces expiry even if Hikyo is
// down), extends, drops (idempotently), and probes role status. Every outbound
// connection is TLS verify-full and dials only a policy-approved public address
// (or an operator-allowed CIDR); there is no arbitrary-SQL entry point and no
// statement ever reads a secret back out.
package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

// Config constructs a provider. Origin is a postgres URL carrying the admin
// user and target database but NOT the password (postgres://user@host:port/db);
// Password is the sealed admin credential, held only for the provider's
// lifetime. AllowedCIDRs are operator egress exceptions; RootCAs overrides the
// system trust store when set. Deadline bounds every operation and MUST be
// shorter than the caller's row-claim lease.
type Config struct {
	Origin       string
	Password     string
	AllowedCIDRs []netip.Prefix
	RootCAs      *x509.CertPool
	Deadline     time.Duration
}

// Provider is one configured PostgreSQL target. It opens a fresh connection per
// operation (operator provider actions are rare and a lease mint is one request
// each), so there is no pool to leak; Close forgets the admin credential.
type Provider struct {
	connConfig *pgx.ConnConfig
	deadline   time.Duration
	password   string
}

var _ dynamic.Provider = (*Provider)(nil)

// New validates the config and builds the base connection template. It does not
// connect: the first connection happens on the first operation, under that
// operation's deadline.
func New(cfg Config) (*Provider, error) {
	return newWithDialer(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Deadline})
}

func newWithDialer(cfg Config, resolver netpolicy.Resolver, dialer netpolicy.Dialer) (*Provider, error) {
	if cfg.Deadline <= 0 {
		return nil, errors.New("postgres: an operation deadline is required")
	}
	if cfg.Password == "" {
		return nil, errors.New("postgres: an admin credential is required")
	}
	dsn, host, err := canonicalDSN(cfg.Origin, cfg.Password)
	if err != nil {
		return nil, err
	}
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse origin: %w", err)
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("postgres: egress policy: %w", err)
	}
	connConfig.DialFunc = publicDialer.DialContext
	// verify-full: sslmode in the DSN already made pgx build a verifying
	// tls.Config with the right ServerName; only the trust root is overridden
	// here when the operator supplied a bundle. A nil TLSConfig would mean the
	// DSN did not request TLS, which canonicalDSN forbids.
	if connConfig.TLSConfig == nil {
		return nil, errors.New("postgres: TLS is required (sslmode=verify-full)")
	}
	connConfig.TLSConfig.MinVersion = tls.VersionTLS12
	connConfig.TLSConfig.ServerName = host
	if cfg.RootCAs != nil {
		connConfig.TLSConfig.RootCAs = cfg.RootCAs
	}
	return &Provider{connConfig: connConfig, deadline: cfg.Deadline, password: cfg.Password}, nil
}

// canonicalDSN rejects anything but an exact postgres URL with a user, host and
// database and no embedded password, then returns a DSN with the password and
// sslmode=verify-full applied. The returned host is the TLS ServerName.
func canonicalDSN(origin, password string) (string, string, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return "", "", fmt.Errorf("postgres: origin %q: %w", origin, err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", "", fmt.Errorf("postgres: origin %q must be a postgres:// URL", origin)
	}
	if u.User == nil || u.User.Username() == "" {
		return "", "", fmt.Errorf("postgres: origin %q must carry the admin username", origin)
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return "", "", fmt.Errorf("postgres: origin %q must not embed a password", origin)
	}
	host := u.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("postgres: origin %q must carry a host", origin)
	}
	if strings.Trim(u.Path, "/") == "" {
		return "", "", fmt.Errorf("postgres: origin %q must name a database", origin)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		// A query string could smuggle password=, host=, sslmode= or file paths
		// that override the verify-full posture. Only Hikyo sets connection
		// parameters (below); the operator's origin carries none.
		return "", "", fmt.Errorf("postgres: origin %q must not carry query parameters or a fragment", origin)
	}
	u.User = url.UserPassword(u.User.Username(), password)
	q := u.Query()
	q.Set("sslmode", "verify-full")
	u.RawQuery = q.Encode()
	return u.String(), host, nil
}

func (p *Provider) connect(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, p.connConfig)
	if err != nil {
		// A failed connect or authentication ran no statement: definite
		// failure, nothing durable changed at the provider.
		return nil, fmt.Errorf("%w: %v", dynamic.ErrUnreachable, err)
	}
	return conn, nil
}

// CreateRole mints the lease role. The role name and grant role are sanitized
// identifiers; the password is validated to the generator charset before it is
// placed in the (engine-escaped) literal, so no value can break out of the DDL.
func (p *Provider) CreateRole(ctx context.Context, req dynamic.CreateRoleRequest) error {
	if !dynamic.ValidRoleName(req.Name) {
		return fmt.Errorf("postgres: refusing malformed role name %q", req.Name)
	}
	if !dynamic.ValidPassword(req.Password) {
		return errors.New("postgres: refusing a password outside the generator charset")
	}
	if req.GrantRole == "" {
		return errors.New("postgres: a grant role is required")
	}
	ctx, cancel := context.WithTimeout(ctx, p.deadline)
	defer cancel()
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(ctx, createRoleSQL(req))
	return classifyExec(err)
}

// ExtendRole moves VALID UNTIL forward (or back). An absent role is a definite
// failure the caller settles by reconcile.
func (p *Provider) ExtendRole(ctx context.Context, name string, validUntil time.Time) error {
	if !dynamic.ValidRoleName(name) {
		return fmt.Errorf("postgres: refusing malformed role name %q", name)
	}
	ctx, cancel := context.WithTimeout(ctx, p.deadline)
	defer cancel()
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(ctx, extendRoleSQL(name, validUntil))
	return classifyExec(err)
}

// DropRole removes the lease role. It is idempotent: a role that is already
// gone is a success, which is what makes revoke safe to retry.
func (p *Provider) DropRole(ctx context.Context, name string) error {
	if !dynamic.ValidRoleName(name) {
		return fmt.Errorf("postgres: refusing malformed role name %q", name)
	}
	ctx, cancel := context.WithTimeout(ctx, p.deadline)
	defer cancel()
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(ctx, dropRoleSQL(name))
	return classifyExec(err)
}

// RoleStatus probes pg_roles so reconcile can settle any transition, including a
// renew (it reads back VALID UNTIL), not just a mint or drop.
func (p *Provider) RoleStatus(ctx context.Context, name string) (dynamic.RoleStatus, error) {
	if !dynamic.ValidRoleName(name) {
		return dynamic.RoleStatus{}, fmt.Errorf("postgres: refusing malformed role name %q", name)
	}
	ctx, cancel := context.WithTimeout(ctx, p.deadline)
	defer cancel()
	conn, err := p.connect(ctx)
	if err != nil {
		return dynamic.RoleStatus{}, err
	}
	defer conn.Close(context.Background())
	var validUntil *time.Time
	err = conn.QueryRow(ctx, "SELECT rolvaliduntil FROM pg_roles WHERE rolname = $1", name).Scan(&validUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return dynamic.RoleStatus{Exists: false}, nil
	}
	if err != nil {
		return dynamic.RoleStatus{}, classifyExec(err)
	}
	out := dynamic.RoleStatus{Exists: true}
	if validUntil != nil {
		out.ValidUntil = validUntil.UTC()
	}
	return out, nil
}

// Close forgets the admin credential. There is no pooled connection to release
// (each operation opens and closes its own).
func (p *Provider) Close() {
	if p == nil {
		return
	}
	p.password = ""
	if p.connConfig != nil {
		p.connConfig.Password = ""
	}
}

// classifyExec turns a driver error into a state decision. A PgError means the
// statement reached the server and the server answered, so the role state is
// known (the caller inspects the SQLSTATE). A context or transport error after
// connect leaves the outcome ambiguous: the statement may have committed and we
// lost the acknowledgement, so it is NEVER reported as success.
func classifyExec(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		// The server answered: the statement ran and errored, so the role state
		// is known. A definite refusal, never ambiguous.
		return fmt.Errorf("%w: %v", dynamic.ErrRefused, err)
	}
	if errors.Is(err, dynamic.ErrUnreachable) {
		return err
	}
	return fmt.Errorf("%w: %v", dynamic.ErrAmbiguous, err)
}

// createRoleSQL renders the mint DDL. Identifiers are sanitized (pgx doubles
// embedded quotes and wraps them); the password is charset-validated by the
// caller and additionally escaped as a literal here; VALID UNTIL enforces
// expiry at the engine; IN ROLE inherits the operator's grant role.
func createRoleSQL(req dynamic.CreateRoleRequest) string {
	return fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD %s VALID UNTIL %s IN ROLE %s",
		pgx.Identifier{req.Name}.Sanitize(),
		quoteLiteral(req.Password),
		quoteLiteral(req.ValidUntil.UTC().Format(time.RFC3339)),
		pgx.Identifier{req.GrantRole}.Sanitize(),
	)
}

func extendRoleSQL(name string, validUntil time.Time) string {
	return fmt.Sprintf("ALTER ROLE %s VALID UNTIL %s",
		pgx.Identifier{name}.Sanitize(), quoteLiteral(validUntil.UTC().Format(time.RFC3339)))
}

func dropRoleSQL(name string) string {
	return fmt.Sprintf("DROP ROLE IF EXISTS %s", pgx.Identifier{name}.Sanitize())
}

// quoteLiteral renders a single-quoted SQL string literal, doubling embedded
// quotes. Values placed through it (a charset-validated password, an RFC3339
// timestamp) contain no quotes, but the escaping is applied unconditionally so
// the safety does not depend on the caller's discipline.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
