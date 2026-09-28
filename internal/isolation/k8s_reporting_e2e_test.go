//go:build k8se2e

package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Delivery-target condition reporting, controller to browser (#791, the
// k8s-condition-reporting ADR's validation ticket).
//
// Three real parties, none stubbed:
//   - the OPERATOR is the shipped `hikyo operator` process (its own leader
//     election, informers and reporter), run on the host against kind;
//   - the SERVER is the real router over a real datastore, in-process so the
//     delivery clock is the existing service.Delivery.Now seam: staleness and
//     the purge are reached by moving that clock, never by sleeping;
//   - the BROWSER is Chromium under Playwright (web/e2e/reporting), signed in
//     through the login form, reading the embedded bundle this server serves.
//
// Playwright owns the scenario order. Each of its tests first asks this
// process, over the control endpoint, to arrange the cluster and server state
// (one named step), then asserts what the browser shows. The steps run on the
// test goroutine, so every helper here may fail the test directly.
//
// The operator reaches the server through its own listener, the front, which
// records every report and tombstone body it receives. Replays crafted by the
// test go to the browser's listener instead, so the record holds only what the
// real operator put on the wire. The operator is stopped while a crafted
// request's effect is asserted, so no heartbeat can land in between.

const (
	reportingOperatorVersion = "0.0.0-reporting-e2e"
	reportingArtifactsEnv    = "HIKYO_REPORTING_E2E_ARTIFACTS"
	reportingViewer          = "reporting-viewer"
	reportingPassword        = "a perfectly ordinary passphrase"

	// The second tenant, whose environment the cross-tenant CR names.
	e2eOrgB = "org_0192f000-0000-7000-8000-00000000001a"
	e2ePrjB = "prj_0192f000-0000-7000-8000-00000000001b"
	e2eEnvB = "env_0192f000-0000-7000-8000-00000000001c"

	// pastStale is beyond the D5 threshold of a five-minute heartbeat:
	// 2 x 5 min + 5 min.
	pastStale = 16 * time.Minute
)

// offsetClock is the delivery service's clock: real time plus an offset the
// scenarios move.
type offsetClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *offsetClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *offsetClock) offsetNow() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offset
}

func (c *offsetClock) set(offset time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = offset
}

// wireRecord is one delivery-target request the operator sent and the status
// the server answered it with.
type wireRecord struct {
	path   string
	body   []byte
	status int
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// The categories of string no report may carry (ADR D4). The audit refuses to
// pass unless the run observed at least one of each.
const (
	deniedSecretValue   = "secret value"
	deniedConfigValue   = "config value"
	deniedKeyName       = "key name"
	deniedBearer        = "bearer"
	deniedMessage       = "condition message"
	deniedCursor        = "cursor"
	deniedCursorBinding = "cursor binding"
	deniedStamp         = "stamp"
	deniedManagedSecret = "managed Secret UID"
)

// gatedListener is the front's listener. While down it closes every
// connection it accepts before a byte is exchanged, and the port stays held.
type gatedListener struct {
	net.Listener
	down atomic.Bool
}

func (l *gatedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || !l.down.Load() {
			return conn, err
		}
		_ = conn.Close()
	}
}

type reportingWorld struct {
	*opEnv
	kubeconfig string
	artifacts  string
	binary     string
	clock      *offsetClock
	router     http.Handler
	delivery   *service.Delivery
	browser    *httptest.Server
	gate       *gatedListener
	operator   *exec.Cmd
	// halt cancels the running operator's context, which signals it.
	halt  context.CancelFunc
	log   *os.File
	admin service.Actor
	pokes int
	// happyToken is the happy CR's bearer, which the crafted replays present.
	happyToken string
	// doomed is the service account whose credential is revoked and which is
	// then deleted.
	doomed service.ServiceAccountView

	mu   sync.Mutex
	wire []wireRecord
	// denied is everything no report may carry (ADR D4), by category: values,
	// key names, credentials, and every condition message, cursor and stamp a
	// CR held.
	denied map[string]string
}

func TestK8sReportingBrowser(t *testing.T) {
	restCfg := restConfig(t) // skips when the kubeconfig env is unset
	sch := e2eScheme(t)
	ctx := t.Context()

	w := &reportingWorld{
		opEnv:      &opEnv{t: t, ctx: ctx, restCfg: restCfg, scheme: sch},
		kubeconfig: os.Getenv(kubeconfigEnv),
		clock:      &offsetClock{},
		denied: map[string]string{
			cfgKeyOne: deniedKeyName, cfgKeyTwo: deniedKeyName, secKeyOne: deniedKeyName, secKeyTwo: deniedKeyName,
			cfgValOne: deniedConfigValue, cfgValTwo: deniedConfigValue,
			secValOne: deniedSecretValue, secValTwo: deniedSecretValue,
		},
	}
	root := repoRoot(t)
	w.artifacts = os.Getenv(reportingArtifactsEnv)
	if w.artifacts == "" {
		w.artifacts = filepath.Join(root, "web", "test-results", "reporting-e2e")
	}
	must(t, os.MkdirAll(w.artifacts, 0o755))
	// A previous run's screenshots must not vouch for this one.
	stale, err := filepath.Glob(filepath.Join(w.artifacts, "*.png"))
	must(t, err)
	for _, path := range stale {
		must(t, os.Remove(path))
	}
	w.log, err = os.Create(filepath.Join(w.artifacts, "operator.log"))
	must(t, err)
	t.Cleanup(func() { _ = w.log.Close() })
	dist := filepath.Join(root, "internal", "webui", "dist")
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatalf("the SPA has not been built (run scripts/ci/build-spa.sh): %v", err)
	}

	w.cl, err = client.New(restCfg, client.Options{Scheme: sch})
	must(t, err)
	w.cs, err = kubernetes.NewForConfig(restCfg)
	must(t, err)
	applyCRDs(t, ctx, w.cl)

	w.db = seededDB(t, openSQLite)
	identityFixtures(t, w.db)
	seedE2EScope(t, w.db)
	w.seedViewer()

	kr := probeKeyring(t, w.db)
	auth := authService(t, w.db)
	w.browser = httptest.NewUnstartedServer(nil)
	origin := "https://" + w.browser.Listener.Addr().String()
	w.delivery = &service.Delivery{DB: w.db, Keyring: kr, Now: w.clock.Now}
	w.router = server.NewPublic(&service.System{DB: w.db}, &server.API{
		Runtime:      &service.System{DB: w.db},
		Auth:         auth,
		Registration: newRegistration(t, service.RegistrationConfig{DB: w.db, Auth: auth}),
		Orgs:         &service.Orgs{DB: w.db},
		Projects:     &service.Projects{DB: w.db},
		Environments: &service.Environments{DB: w.db, Keyring: kr, Auth: auth},
		Folders:      &service.Folders{DB: w.db},
		Grants:       &service.Grants{DB: w.db, Auth: auth},
		Keys:         &service.Keys{DB: w.db, Keyring: kr},
		Settings:     &service.ProjectSettings{DB: w.db, Auth: auth},
		Identities:   &service.Identities{DB: w.db, Auth: auth},
		Delivery:     w.delivery,
		// The other machine-access tabs list on the same page load.
		Dynamic: &service.Dynamic{DB: w.db, Keyring: kr, Auth: auth, Runtime: store.NewDynamicRuntime(w.db)},
		SSH:     &service.SSH{DB: w.db, Keyring: kr, Auth: auth, Runtime: store.NewSSHRuntime(w.db)},
		PKI:     &service.PKI{DB: w.db, Keyring: kr, Auth: auth, Runtime: store.NewPKIRuntime(w.db)},
		Version: reportingOperatorVersion,
	}, os.DirFS(dist), server.PublicOptions{ExternalOrigin: origin})
	w.browser.Config.Handler = w.router
	w.browser.StartTLS()
	t.Cleanup(w.browser.Close)
	w.startFront()
	t.Cleanup(func() { w.server.Close() })

	w.ns = w.createNamespace()
	w.createInstance(instanceName, "")
	w.binary = filepath.Join(t.TempDir(), "hikyo")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath",
		"-ldflags", "-X main.version="+reportingOperatorVersion, "-o", w.binary, "./cmd/hikyo")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the operator binary: %v\n%s", err, out)
	}
	t.Cleanup(w.stopOperator)
	w.startOperator(true)

	steps := map[string]func() any{
		"facts": func() any {
			return map[string]string{
				"namespace": w.ns, "username": reportingViewer, "password": reportingPassword,
				"org": e2eOrg, "project": e2ePrj, "orgB": e2eOrgB, "projectB": e2ePrjB,
			}
		},
		"happy-path":         w.stepHappyPath,
		"cross-tenant":       w.stepCrossTenant,
		"grant-revoked":      w.stepGrantRevoked,
		"credential-revoked": w.stepCredentialRevoked,
		"principal-deleted":  w.stepPrincipalDeleted,
		"out-of-order":       w.stepOutOfOrder,
		"unknown-reason":     w.stepUnknownReason,
		"operator-stopped":   w.stepOperatorStopped,
		"reporting-disabled": w.stepReportingDisabled,
		"tombstone":          w.stepTombstone,
		"unreachable-delete": w.stepUnreachableDelete,
		"stale-after-delete": w.stepStaleAfterDelete,
		"purge":              w.stepPurge,
		"wire-audit":         w.stepWireAudit,
	}
	w.drive(steps, origin)
}

// drive runs Playwright and serves its step calls on the test goroutine until
// it exits. A step Playwright never asked for is a scenario that did not run.
func (w *reportingWorld) drive(steps map[string]func() any, origin string) {
	t := w.t
	type call struct {
		name  string
		reply chan []byte
	}
	calls := make(chan call)
	quit := make(chan struct{})
	control := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c := call{name: strings.TrimPrefix(r.URL.Path, "/step/"), reply: make(chan []byte, 1)}
		select {
		case calls <- c:
		case <-quit:
			return
		}
		select {
		case out := <-c.reply:
			rw.Header().Set("Content-Type", "application/json")
			_, _ = rw.Write(out)
		case <-quit:
		}
	}))
	t.Cleanup(control.Close)
	t.Cleanup(func() { close(quit) })

	// pnpm starts node, which starts Chromium. They share one process group so
	// that a cancelled test takes all of them, not only pnpm.
	playwright := exec.CommandContext(t.Context(), "pnpm", "exec", "playwright", "test", "--config", "e2e/reporting.config.ts")
	playwright.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	playwright.Cancel = func() error { return syscall.Kill(-playwright.Process.Pid, syscall.SIGKILL) }
	playwright.WaitDelay = 10 * time.Second
	playwright.Dir = filepath.Join(repoRoot(t), "web")
	playwright.Env = append(os.Environ(),
		"HIKYO_REPORTING_E2E_ORIGIN="+origin,
		"HIKYO_REPORTING_E2E_CONTROL="+control.URL,
		"HIKYO_REPORTING_E2E_SCREENSHOTS="+w.artifacts,
	)
	playwright.Stdout, playwright.Stderr = os.Stdout, os.Stderr
	must(t, playwright.Start())
	// A step that fails the test leaves this function before Playwright exits.
	t.Cleanup(func() { _ = syscall.Kill(-playwright.Process.Pid, syscall.SIGKILL) })
	done := make(chan error, 1)
	go func() { done <- playwright.Wait() }()

	ran := map[string]bool{}
	for {
		select {
		case c := <-calls:
			step, ok := steps[c.name]
			if !ok {
				t.Fatalf("Playwright asked for an unknown step %q", c.name)
			}
			t.Logf("step %s", c.name)
			result := step()
			// The browser's clock follows the delivery clock, so the ages it
			// renders are measured against the same instant the states are.
			out, err := json.Marshal(map[string]any{"result": result, "clock_offset_ms": w.clock.offsetNow().Milliseconds()})
			must(t, err)
			w.observeDenied()
			ran[c.name] = true
			c.reply <- out
		case err := <-done:
			if err != nil {
				t.Fatalf("the browser suite failed: %v", err)
			}
			for name := range steps {
				if !ran[name] {
					t.Errorf("step %q never ran: its scenario was not exercised", name)
				}
			}
			return
		}
	}
}

// seedViewer creates the second tenant and the human the browser signs in as,
// who is also the administrator the steps act as: `read` and
// `manage-identities` on both projects, and org-scope `manage-members` in
// both organisations, because no human holds `report-delivery-status` and only an org member
// manager may grant what it does not hold. Its id is production-shaped, which
// the SPA's parsers require of every `created_by` it is shown.
func (w *reportingWorld) seedViewer() {
	t := w.t
	auth := authService(t, w.db)
	boot, err := auth.BootstrapAdmin(t.Context(), reportingViewer, "Reporting Viewer", "terminal")
	must(t, err)
	must(t, auth.EstablishCredential(t.Context(), boot.Authority, reportingPassword))
	stmts := []string{
		fmt.Sprintf(`INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('%s', 'e2e-org-b', TRUE, '{}', %s)`, e2eOrgB, ts),
		fmt.Sprintf(`INSERT INTO projects (id, org_id, name, created_at) VALUES ('%s', '%s', 'e2e-b', %s)`, e2ePrjB, e2eOrgB, ts),
		fmt.Sprintf(`INSERT INTO project_schema_revisions (org_id, project_id, revision) VALUES ('%s', '%s', 0)`, e2eOrgB, e2ePrjB),
		fmt.Sprintf(`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('%s', '%s', '%s', 'dev', '', %s, 0)`, e2eEnvB, e2eOrgB, e2ePrjB, ts),
	}
	for i, project := range [][2]string{{e2eOrg, e2ePrj}, {e2eOrgB, e2ePrjB}} {
		stmts = append(stmts, fmt.Sprintf(
			`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rep_mm%d', '%s', 'manage-members', '%s', NULL, NULL, %s)`,
			i, boot.PrincipalID, project[0], ts))
		for j, capability := range []string{"read", "manage-identities"} {
			stmts = append(stmts, fmt.Sprintf(
				`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rep_v%d%d', '%s', '%s', '%s', '%s', NULL, %s)`,
				i, j, boot.PrincipalID, capability, project[0], project[1], ts))
		}
	}
	for _, s := range stmts {
		execRaw(t, w.db, s)
	}
	seedOrigins(t, w.db)
	w.admin = service.LocalPrincipal(boot.PrincipalID)
}

// startFront opens the operator's listener over the shared router. It stays
// open for the whole run, so the HikyoInstance's address cannot be taken by
// another process; unreachable is the gate, not a closed port.
func (w *reportingWorld) startFront() {
	front := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/delivery-targets") {
			w.router.ServeHTTP(rw, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(rw, "read body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := &statusRecorder{ResponseWriter: rw, status: http.StatusOK}
		w.router.ServeHTTP(rec, r)
		w.mu.Lock()
		w.wire = append(w.wire, wireRecord{path: r.URL.Path, body: body, status: rec.status})
		w.mu.Unlock()
	}))
	w.gate = &gatedListener{Listener: front.Listener}
	front.Listener = w.gate
	front.StartTLS()
	w.server = front
	w.caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: front.Certificate().Raw})
}

// setUnreachable makes the server unreachable for the operator, or reachable
// again. Going down also drops the connections the operator keeps alive, so
// its next request has to dial: the dial is accepted and closed before the TLS
// handshake, which the operator sees as a transport error with no HTTP status.
func (w *reportingWorld) setUnreachable(down bool) {
	w.gate.down.Store(down)
	if down {
		w.server.CloseClientConnections()
	}
}

// startOperator runs the shipped operator process against the kind cluster.
func (w *reportingWorld) startOperator(reporting bool) {
	ctx, halt := context.WithCancel(w.ctx)
	w.halt = halt
	w.operator = exec.CommandContext(ctx, w.binary, "operator")
	// SIGTERM is the shutdown a pod gets; a process that ignores it is killed
	// when the delay runs out, so a stop never waits without bound.
	w.operator.Cancel = func() error { return w.operator.Process.Signal(syscall.SIGTERM) }
	w.operator.WaitDelay = 15 * time.Second
	w.operator.Env = append(os.Environ(),
		"KUBECONFIG="+w.kubeconfig,
		"HIKYO_OPERATOR_NAMESPACE="+w.ns,
		"HIKYO_OPERATOR_NAMESPACES="+w.ns,
		"HIKYO_OPERATOR_METRICS_ADDR=0",
		"HIKYO_OPERATOR_HEALTH_ADDR=0",
		"HIKYO_OPERATOR_STATUS_REPORTING="+strconv.FormatBool(reporting),
	)
	w.operator.Stdout, w.operator.Stderr = w.log, w.log
	if err := w.operator.Start(); err != nil {
		halt()
		w.t.Fatalf("start the operator: %v", err)
	}
}

func (w *reportingWorld) stopOperator() {
	if w.operator == nil {
		return
	}
	w.halt()
	_ = w.operator.Wait()
	w.operator = nil
}

// observeDenied adds what the namespace's CRs hold right now to the denied
// set. A deleted CR takes its status with it, so it runs as soon as a target
// has reported, and again after every step for what changed since.
func (w *reportingWorld) deny(value, category string) {
	if value != "" {
		w.denied[value] = category
	}
}

func (w *reportingWorld) observeDenied() {
	var crs hikyov1.HikyoSecretList
	must(w.t, w.cl.List(w.ctx, &crs, client.InNamespace(w.ns)))
	for _, cr := range crs.Items {
		for _, c := range cr.Status.Conditions {
			w.deny(c.Message, deniedMessage)
		}
		w.deny(cr.Status.Cursor, deniedCursor)
		w.deny(cr.Status.CursorBinding, deniedCursorBinding)
		w.deny(cr.Status.Stamp, deniedStamp)
		w.deny(cr.Status.ManagedSecretUID, deniedManagedSecret)
	}
}

// reporter creates a workload service account in the e2e project; see
// reporterIn.
func (w *reportingWorld) reporter(name string) (service.ServiceAccountView, service.MintResult) {
	return w.reporterIn(name, e2eScopeEnv())
}

// reporterIn creates a workload service account in env's project holding
// `read` and `report-delivery-status` on env, and its designated bootstrap
// Secret, named after the account.
func (w *reportingWorld) reporterIn(name string, env domain.Scope) (service.ServiceAccountView, service.MintResult) {
	project := domain.Scope{Org: env.Org, Project: env.Project}
	ident := identitySvc(w.db)
	sa, err := ident.CreateServiceAccount(w.ctx, w.admin, project, name, domain.ClassWorkload)
	must(w.t, err)
	minted, err := ident.MintCredential(w.ctx, w.admin, project, sa.ID, service.MintRequest{})
	must(w.t, err)
	for _, capability := range []domain.Capability{domain.CapRead, domain.CapReportDeliveryStatus} {
		if _, err := grantSvcWithAuth(w.db).Create(w.ctx, w.admin, service.GrantSpec{
			Target: sa.Principal, Capability: capability, Scope: env,
		}); err != nil {
			w.t.Fatalf("grant %s to %s: %v", capability, name, err)
		}
	}
	w.deny(minted.Value, deniedBearer)
	w.createBootstrapSecret(name, minted.Value, instanceName, true)
	return sa, minted
}

// target creates a config-only CR under the named reporter's credential and
// waits for its row.
func (w *reportingWorld) target(name, reporter string) {
	w.createCR(crSpec{name: name, target: name + "-secret", secretRef: reporter, mapping: configMapping(), projection: hikyov1.ProjectionConfigOnly})
	w.waitCondition(name, hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	w.waitRows(name, 1)
	w.observeDenied()
}

func (w *reportingWorld) rows(name string) int64 {
	return dtRows(w.t, w.db, fmt.Sprintf("namespace = '%s' AND name = '%s'", w.ns, name))
}

func (w *reportingWorld) waitRows(name string, want int64) {
	w.t.Helper()
	poll(w.t, w.ctx, func(context.Context) (bool, error) { return w.rows(name) == want, nil })
}

// sent returns the operator's recorded requests for one CR, oldest first. A
// tombstone names no CR, so it is matched on the CR's UID.
func (w *reportingWorld) sent(match string) []wireRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []wireRecord
	for _, r := range w.wire {
		if bytes.Contains(r.body, []byte(match)) {
			out = append(out, r)
		}
	}
	return out
}

// waitAnswer waits until the operator's latest request matching match was
// answered with status, and returns how many matching requests there are.
func (w *reportingWorld) waitAnswer(match string, status int) int {
	w.t.Helper()
	var n int
	poll(w.t, w.ctx, func(context.Context) (bool, error) {
		sent := w.sent(match)
		n = len(sent)
		return n > 0 && sent[n-1].status == status, nil
	})
	return n
}

// reconcileNow changes the CR's spec, so its generation moves: the operator
// reconciles at once and its reportable content changed, which is what makes
// it report outside the heartbeat. The resync interval it sets stays under
// five minutes, so the reported heartbeat, and with it the staleness
// threshold, stays at the floor.
func (w *reportingWorld) reconcileNow(name string) {
	w.pokes++
	cr := w.getCR(name)
	// A merge patch of the one field: an Update would carry the whole object
	// and conflict with the operator's own status writes.
	base := cr.DeepCopy()
	cr.Spec.ResyncInterval = fmt.Sprintf("%ds", 240-w.pokes)
	must(w.t, w.cl.Patch(w.ctx, cr, client.MergeFrom(base)))
}

// reportAgain forces a reconcile and waits until the server accepted the
// report that follows it.
func (w *reportingWorld) reportAgain(name string) {
	w.t.Helper()
	match := `"name":"` + name + `"`
	before := len(w.sent(match))
	w.reconcileNow(name)
	poll(w.t, w.ctx, func(context.Context) (bool, error) {
		sent := w.sent(match)
		return len(sent) > before && sent[len(sent)-1].status == http.StatusNoContent, nil
	})
}

func (w *reportingWorld) snapshot(name string) string {
	return queryString(w.t, w.db, fmt.Sprintf(
		"SELECT observed_generation || '/' || reported_at || '/' || received_at || '/' || conditions || '/' || COALESCE(refusal_cause, '') "+
			"FROM delivery_target_reports WHERE namespace = '%s' AND name = '%s'", w.ns, name))
}

// replay posts a crafted report to the browser's listener under a bearer and
// returns the status.
func (w *reportingWorld) replay(bearer string, report apigen.DeliveryTargetReportRequest) int {
	body, err := json.Marshal(report)
	must(w.t, err)
	url := fmt.Sprintf("%s/api/v1/orgs/%s/projects/%s/environments/%s/delivery-targets", w.browser.URL, e2eOrg, e2ePrj, e2eEnv)
	req, err := http.NewRequestWithContext(w.ctx, http.MethodPost, url, bytes.NewReader(body))
	must(w.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := w.browser.Client().Do(req)
	must(w.t, err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// lastAccepted decodes the newest report of the CR the server accepted: the
// operator's own bytes, which the ordering replays start from.
func (w *reportingWorld) lastAccepted(name string) apigen.DeliveryTargetReportRequest {
	sent := w.sent(`"name":"` + name + `"`)
	for _, r := range slices.Backward(sent) {
		if r.status == http.StatusNoContent {
			var report apigen.DeliveryTargetReportRequest
			must(w.t, json.Unmarshal(r.body, &report))
			return report
		}
	}
	w.t.Fatalf("the operator never had a report for %q accepted", name)
	return apigen.DeliveryTargetReportRequest{}
}

// ---- steps, in the order web/e2e/reporting/reporting.spec.ts asks for them ----

// Scenario 1. The happy CR delivers all four keys, secrets included, so the
// payload audit has real values to look for.
func (w *reportingWorld) stepHappyPath() any {
	sa, minted := w.reporter("happy-reporter")
	w.happyToken = minted.Value
	seedE2EReveal(w.t, w.db, "g_rep_reveal", sa.Principal, domain.CapReveal)
	w.createCR(crSpec{name: "happy", target: "happy-secret", secretRef: "happy-reporter", mapping: allFourMapping()})
	w.waitCondition("happy", hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	w.waitRows("happy", 1)
	return nil
}

// targetB creates a CR naming tenant B's environment under the named
// bootstrap Secret.
func (w *reportingWorld) targetB(name, secretRef string) {
	must(w.t, w.cl.Create(w.ctx, &hikyov1.HikyoSecret{
		ObjectMeta: metav1.ObjectMeta{Namespace: w.ns, Name: name},
		Spec: hikyov1.HikyoSecretSpec{
			InstanceRef: hikyov1.InstanceRef{Name: instanceName},
			Scope:       hikyov1.Scope{Org: e2eOrgB, Project: e2ePrjB, Environment: e2eEnvB},
			Target:      hikyov1.Target{Name: name + "-secret"},
			Mapping:     []hikyov1.Mapping{{Key: cfgKeyOne, SecretKey: cfgKeyOne}},
			Auth:        hikyov1.AuthRef{SecretRef: &hikyov1.LocalObjectRef{Name: secretRef}},
		},
	}))
}

// Scenario 2. Tenant B's own reporter reports into B first, so the browser has
// a row proving it can read B. Then a CR presents project A's credential and
// names the same environment.
func (w *reportingWorld) stepCrossTenant() any {
	w.reporterIn("tenant-b-reporter", domain.Scope{Org: e2eOrgB, Project: e2ePrjB, Env: e2eEnvB})
	w.targetB("tenant-b", "tenant-b-reporter")
	w.waitAnswer(`"name":"tenant-b"`, http.StatusNoContent)
	w.waitRows("tenant-b", 1)

	w.targetB("cross-tenant", "happy-reporter")
	w.waitAnswer(`"name":"cross-tenant"`, http.StatusNotFound)
	for _, r := range w.sent(`"name":"cross-tenant"`) {
		if !strings.Contains(r.path, e2eEnvB) || r.status != http.StatusNotFound {
			w.t.Fatalf("cross-tenant report to %s answered %d, want 404 on tenant B's environment", r.path, r.status)
		}
	}
	if got := dtRows(w.t, w.db, "name = 'cross-tenant' OR (environment_id = '"+e2eEnvB+"' AND name <> 'tenant-b')"); got != 0 {
		w.t.Fatalf("a refused cross-tenant report left %d rows", got)
	}
	return nil
}

// Scenario 3, first leg: the grant goes, the credential stays. The CR's next
// report is the uniform 404.
func (w *reportingWorld) stepGrantRevoked() any {
	sa, _ := w.reporter("grant-revoked-reporter")
	w.target("grant-revoked", "grant-revoked-reporter")
	must(w.t, grantSvcWithAuth(w.db).Revoke(w.ctx, w.admin, service.GrantSpec{
		Target: sa.Principal, Capability: domain.CapReportDeliveryStatus, Scope: e2eScopeEnv(),
	}))
	w.reconcileNow("grant-revoked")
	w.waitAnswer(`"name":"grant-revoked"`, http.StatusNotFound)
	return nil
}

// Scenario 3, second leg: the CR's only bootstrap token is revoked. Its next
// report is answered 401 while the happy CR's is still accepted.
func (w *reportingWorld) stepCredentialRevoked() any {
	var minted service.MintResult
	w.doomed, minted = w.reporter("credential-revoked-reporter")
	w.target("credential-revoked", "credential-revoked-reporter")
	must(w.t, identitySvc(w.db).RevokeCredential(w.ctx, w.admin, e2eScopePrj(), w.doomed.ID, minted.Credential.ID))

	w.reconcileNow("credential-revoked")
	w.waitAnswer(`"name":"credential-revoked"`, http.StatusUnauthorized)
	w.reportAgain("happy")
	return nil
}

// Scenario 3, third leg: the principal is deleted and its rows go with it.
func (w *reportingWorld) stepPrincipalDeleted() any {
	must(w.t, identitySvc(w.db).DeleteServiceAccount(w.ctx, w.admin, e2eScopePrj(), w.doomed.ID))
	if got := w.rows("credential-revoked"); got != 0 {
		w.t.Fatalf("rows after the principal was deleted = %d, want 0", got)
	}
	return nil
}

// Scenario 4, ordering: three crafted replays of the operator's own last
// accepted report, each refused 409 and none of them recorded on the row. The
// operator is stopped first and stays stopped until the stale step, so no
// heartbeat of its own can move the row, or clear the refusal that follows,
// before the browser has read it.
func (w *reportingWorld) stepOutOfOrder() any {
	w.stopOperator()
	accepted := w.lastAccepted("happy")
	before := w.snapshot("happy")
	older := accepted
	older.ObservedGeneration--
	older.ReportedAt = accepted.ReportedAt.Add(time.Second)
	earlier := accepted
	earlier.ReportedAt = accepted.ReportedAt.Add(-time.Second)
	future := accepted
	future.ReportedAt = w.clock.Now().Add(deliverytarget.MaxFutureSkew + time.Minute)
	for name, report := range map[string]apigen.DeliveryTargetReportRequest{
		"an older generation": older, "an earlier reported_at in the same generation": earlier, "a reported_at past the future skew": future,
	} {
		if got := w.replay(w.happyToken, report); got != http.StatusConflict {
			w.t.Fatalf("%s answered %d, want 409", name, got)
		}
	}
	if after := w.snapshot("happy"); after != before {
		w.t.Fatalf("an out-of-order report changed the row: %q -> %q", before, after)
	}
	return nil
}

// Scenario 4, vocabulary: a crafted report whose reason is outside the
// vocabulary is the one refusal the row records.
func (w *reportingWorld) stepUnknownReason() any {
	report := w.lastAccepted("happy")
	report.ReportedAt = report.ReportedAt.Add(time.Second)
	report.Conditions[0].Reason = "NotInVocabulary"
	if got := w.replay(w.happyToken, report); got != http.StatusUnprocessableEntity {
		w.t.Fatalf("an unknown reason answered %d, want 422", got)
	}
	return nil
}

// Scenario 5, stale: the operator comes back and its accepted report clears
// the refusal, then it stops and the delivery clock moves past the threshold.
func (w *reportingWorld) stepOperatorStopped() any {
	w.startOperator(true)
	w.reportAgain("happy")
	w.stopOperator()
	w.clock.set(pastStale)
	return nil
}

// Scenario 5, never reported: with reporting disabled the operator reconciles
// the new CR, and every other one, and sends nothing at all until it has
// exited, so the new CR has no row.
func (w *reportingWorld) stepReportingDisabled() any {
	w.clock.set(0)
	w.mu.Lock()
	before := len(w.wire)
	w.mu.Unlock()
	w.startOperator(false)
	w.reporter("never-reported-reporter")
	w.createCR(crSpec{name: "never-reported", target: "never-reported-secret", secretRef: "never-reported-reporter", mapping: configMapping(), projection: hikyov1.ProjectionConfigOnly})
	w.waitCondition("never-reported", hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	w.stopOperator()
	w.mu.Lock()
	sent := len(w.wire) - before
	w.mu.Unlock()
	if sent != 0 || w.rows("never-reported") != 0 {
		w.t.Fatalf("an operator with reporting disabled sent %d requests and its new CR holds %d rows", sent, w.rows("never-reported"))
	}
	return nil
}

// Scenario 6, tombstone: the CR is deleted while the server is reachable.
func (w *reportingWorld) stepTombstone() any {
	w.startOperator(true)
	w.reporter("tombstoned-reporter")
	w.target("tombstoned", "tombstoned-reporter")
	uid := string(w.getCR("tombstoned").UID)
	must(w.t, w.cl.Delete(w.ctx, w.getCR("tombstoned")))
	poll(w.t, w.ctx, func(context.Context) (bool, error) {
		for _, r := range w.sent(uid) {
			if strings.HasSuffix(r.path, "/tombstone") && r.status == http.StatusNoContent {
				return true, nil
			}
		}
		return false, nil
	})
	w.waitRows("tombstoned", 0)
	return nil
}

// Scenario 6, unreachable: the server cannot be reached when the CR is
// deleted, so the tombstone is lost. There is no finalizer: the CR goes anyway.
func (w *reportingWorld) stepUnreachableDelete() any {
	w.reporter("orphaned-reporter")
	w.target("orphaned", "orphaned-reporter")
	uid := string(w.getCR("orphaned").UID)
	w.mu.Lock()
	before := len(w.wire)
	w.mu.Unlock()
	w.setUnreachable(true)
	must(w.t, w.cl.Delete(w.ctx, w.getCR("orphaned")))
	var failure string
	poll(w.t, w.ctx, func(ctx context.Context) (bool, error) {
		events, err := w.cs.CoreV1().Events(w.ns).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + uid + ",reason=StatusReportFailed"})
		if err != nil {
			return false, err
		}
		i := slices.IndexFunc(events.Items, func(e corev1.Event) bool { return strings.Contains(e.Message, "status tombstone failed") })
		if i < 0 {
			return false, nil
		}
		failure = events.Items[i].Message
		return true, nil
	})
	w.t.Logf("the operator's tombstone failure: %s", failure)
	// A transport failure, not an answer: nothing reached the router.
	if strings.Contains(failure, "server answered status") {
		w.t.Fatalf("the tombstone was answered, not lost: %s", failure)
	}
	w.mu.Lock()
	reached := len(w.wire) - before
	w.mu.Unlock()
	if reached != 0 {
		w.t.Fatalf("%d delivery-target requests reached the unreachable server", reached)
	}
	w.setUnreachable(false)
	if got := w.rows("orphaned"); got != 1 {
		w.t.Fatalf("rows after a lost tombstone = %d, want the row still held", got)
	}
	return nil
}

func (w *reportingWorld) stepStaleAfterDelete() any {
	w.clock.set(pastStale)
	return nil
}

// The hourly purge: the scheduler's own job function on the server's own
// delivery service, under the one clock. The happy CR reports an hour short of
// thirty days on, so it is the row the purge must spare. The orphaned row,
// received at the real time, survives a purge at that clock and goes in one
// two hours later: the threshold lies within an hour of thirty days.
func (w *reportingWorld) stepPurge() any {
	w.clock.set(deliverytarget.PurgeAfter - time.Hour)
	w.reportAgain("happy")
	w.stopOperator()
	must(w.t, w.delivery.PurgeExpiredTargets(w.ctx))
	if got := w.rows("orphaned"); got != 1 {
		w.t.Fatalf("rows an hour short of thirty days = %d, want the row still held", got)
	}
	if got := dtEvents(w.t, w.db, "identity.delivery_target_purged", ""); got != 0 {
		w.t.Fatalf("purge events an hour short of thirty days = %d, want 0", got)
	}

	held := dtRows(w.t, w.db, "1 = 1")
	w.clock.set(deliverytarget.PurgeAfter + time.Hour)
	must(w.t, w.delivery.PurgeExpiredTargets(w.ctx))
	if orphaned, happy := w.rows("orphaned"), w.rows("happy"); orphaned != 0 || happy != 1 {
		w.t.Fatalf("rows an hour past thirty days: orphaned %d, happy %d; want 0 and 1", orphaned, happy)
	}
	purged := held - dtRows(w.t, w.db, "1 = 1")
	if got := dtEvents(w.t, w.db, "identity.delivery_target_purged", ""); got != purged {
		w.t.Fatalf("purge events = %d for %d purged rows", got, purged)
	}
	w.clock.set(0)
	return nil
}

// Scenario 7. Every body the operator sent during the run decodes strictly
// into its contract type and carries nothing from the denied set, and the
// denied set holds something the run observed in every category.
func (w *reportingWorld) stepWireAudit() any {
	w.mu.Lock()
	wire := slices.Clone(w.wire)
	w.mu.Unlock()
	counts := map[string]int{}
	for _, category := range w.denied {
		counts[category]++
	}
	for _, category := range []string{
		deniedSecretValue, deniedConfigValue, deniedKeyName, deniedBearer, deniedMessage,
		deniedCursor, deniedCursorBinding, deniedStamp, deniedManagedSecret,
	} {
		if counts[category] == 0 {
			w.t.Fatalf("the run observed no %s, so the audit would not have looked for one", category)
		}
	}
	for _, r := range wire {
		var into any = &apigen.DeliveryTargetReportRequest{}
		kind := "reports"
		if strings.HasSuffix(r.path, "/tombstone") {
			into, kind = &apigen.DeliveryTargetTombstoneRequest{}, "tombstones"
		}
		counts[kind]++
		dec := json.NewDecoder(bytes.NewReader(r.body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(into); err != nil {
			w.t.Errorf("a body on the wire is not the contract shape: %v: %s", err, r.body)
		}
		for d, category := range w.denied {
			if bytes.Contains(r.body, []byte(d)) {
				w.t.Errorf("a body on the wire carries the %s %q: %s", category, d, r.body)
			}
		}
	}
	if counts["reports"] == 0 || counts["tombstones"] == 0 {
		w.t.Fatalf("captured %d reports and %d tombstones; the audit needs both", counts["reports"], counts["tombstones"])
	}
	var dump bytes.Buffer
	for _, r := range wire {
		fmt.Fprintf(&dump, "%d %s %s\n", r.status, r.path, r.body)
	}
	must(w.t, os.WriteFile(filepath.Join(w.artifacts, "wire.log"), dump.Bytes(), 0o644))
	if w.t.Failed() {
		w.t.FailNow()
	}
	return counts
}
