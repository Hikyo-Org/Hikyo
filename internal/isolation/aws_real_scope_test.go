package isolation

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// These tests are local fixture guards. They do not contact AWS and do not
// count as real provider acceptance.
// The real lane takes only the smoke shape (#158 option A): one static writer,
// its expired session, and separate oracle and cleanup identities.
func realAWSManifestFixture(now time.Time) realAWSManifest {
	m := realAWSManifest{
		Account: "123456789012", Region: "eu-west-1", Prefix: "hikyo-e2e-abcdefgh/",
		AuthorizationID: "test-scope", BudgetUSD: 1, ExpiresAt: now.Add(time.Hour),
		KMSKeyARN: "arn:aws:kms:eu-west-1:123456789012:key/11111111-2222-3333-4444-555555555555",
		Writers:   make(map[string]realAWSActor),
	}
	actor := func(name string) realAWSActor {
		return realAWSActor{CredentialFile: name + ".json", PrincipalARN: "arn:aws:iam::123456789012:role/" + name}
	}
	m.Oracle, m.Cleanup = actor("oracle"), actor("cleanup")
	static := actor("static")
	static.ExpiredCredentialFile = "static-expired.json"
	m.Writers["static"] = static
	return m
}

func TestAWSRealFixtureScope(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if err := realAWSManifestFixture(now).validate(now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*realAWSManifest){
		"emulator-account":  func(m *realAWSManifest) { m.Account = "000000000000" },
		"other-key-account": func(m *realAWSManifest) { m.KMSKeyARN = strings.Replace(m.KMSKeyARN, m.Account, "111122223333", 1) },
		"wildcard-key":      func(m *realAWSManifest) { m.KMSKeyARN += "*" },
		"shared-identity":   func(m *realAWSManifest) { m.Cleanup = m.Oracle },
		"expired":           func(m *realAWSManifest) { m.ExpiresAt = now },
		"unbounded-time":    func(m *realAWSManifest) { m.ExpiresAt = now.Add(3 * time.Hour) },
		"unbounded-prefix":  func(m *realAWSManifest) { m.Prefix = "*" },
		"missing-approval":  func(m *realAWSManifest) { m.AuthorizationID = "" },
		"missing-budget":    func(m *realAWSManifest) { m.BudgetUSD = 0 },
		"excess-budget":     func(m *realAWSManifest) { m.BudgetUSD = 10.01 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := realAWSManifestFixture(now)
			change(&m)
			if m.validate(now) == nil {
				t.Fatal("unsafe fixture scope accepted")
			}
		})
	}
	raw, err := json.Marshal(realAWSManifestFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), ']'), []byte(`{"unknown":"do not echo this"}`)} {
		if _, err := decodeAWSManifest(bad); err == nil || strings.Contains(err.Error(), "do not echo") {
			t.Fatal("manifest decoder accepted or disclosed invalid input")
		}
	}
	// The workload and role inputs of the retired five-writer lane are no
	// longer manifest fields, so a manifest that names one is refused.
	for _, field := range []string{"token_file", "expired_token_file", "workload_role_arn", "source_file", "source_principal_arn", "expired_source_file"} {
		withField := strings.Replace(string(raw), `"writers":{"static":{`, `"writers":{"static":{"`+field+`":"x",`, 1)
		if withField == string(raw) {
			t.Fatal("fixture JSON lost its static writer")
		}
		if _, err := decodeAWSManifest([]byte(withField)); err == nil {
			t.Fatalf("manifest with the former %s field accepted", field)
		}
	}
}

// The live smoke holds one static writer, its expired session and separate
// oracle and cleanup identities; IAM users are accepted as well as roles.
func realAWSSmokeManifestFixture(now time.Time) realAWSManifest {
	m := realAWSManifestFixture(now)
	user := func(name string) realAWSActor {
		return realAWSActor{CredentialFile: name + ".json", PrincipalARN: "arn:aws:iam::123456789012:user/hikyo-smoke-" + name}
	}
	static := user("writer")
	static.ExpiredCredentialFile = "static-expired.json"
	m.Writers = map[string]realAWSActor{"static": static}
	m.Oracle, m.Cleanup = user("oracle"), user("cleanup")
	return m
}

func TestAWSRealFixtureSmokeScope(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	smoke := realAWSSmokeManifestFixture(now)
	if err := smoke.validateLane(now, false); err != nil {
		t.Fatal(err)
	}
	if smoke.maxCreations() != realAWSSmokeMaxCreations || realAWSManifestFixture(now).maxCreations() != realAWSSmokeMaxCreations {
		t.Fatal("real lane budget is not the smoke budget")
	}
	for name, mutate := range map[string]func(*realAWSManifest){
		"no-static": func(m *realAWSManifest) { m.Writers = map[string]realAWSActor{"kms-denied": m.Writers["static"]} },
		"two-writers": func(m *realAWSManifest) {
			m.Writers["kms-denied"] = realAWSActor{CredentialFile: "k.json", PrincipalARN: "arn:aws:iam::123456789012:user/k"}
		},
		"five-writers": func(m *realAWSManifest) {
			for i, mode := range []string{"ambient", "assume-role", "web-identity", "kms-denied"} {
				m.Writers[mode] = realAWSActor{CredentialFile: mode + ".json", PrincipalARN: fmt.Sprintf("arn:aws:iam::123456789012:role/w%d", i)}
			}
		},
		"no-expired": func(m *realAWSManifest) {
			a := m.Writers["static"]
			a.ExpiredCredentialFile = ""
			m.Writers["static"] = a
		},
		"shared-principal": func(m *realAWSManifest) { m.Cleanup.PrincipalARN = m.Oracle.PrincipalARN },
		"foreign-account":  func(m *realAWSManifest) { m.Oracle.PrincipalARN = "arn:aws:iam::111122223333:user/oracle" },
		"group-principal":  func(m *realAWSManifest) { m.Oracle.PrincipalARN = "arn:aws:iam::123456789012:group/oracle" },
		"no-oracle-file":   func(m *realAWSManifest) { m.Oracle.CredentialFile = "" },
		"no-kms-key":       func(m *realAWSManifest) { m.KMSKeyARN = "" },
		"expired-window":   func(m *realAWSManifest) { m.ExpiresAt = now },
	} {
		t.Run(name, func(t *testing.T) {
			m := realAWSSmokeManifestFixture(now)
			mutate(&m)
			if m.validate(now) == nil {
				t.Fatal("invalid smoke manifest accepted")
			}
		})
	}
	// A non-smoke real manifest is refused for its shape, not by accident.
	five := realAWSSmokeManifestFixture(now)
	five.Writers["ambient"] = realAWSActor{CredentialFile: "ambient.json", PrincipalARN: "arn:aws:iam::123456789012:role/ambient"}
	if err := five.validateLane(now, false); err == nil || !strings.Contains(err.Error(), "only the smoke manifest") {
		t.Fatal("non-smoke real manifest was not refused as such")
	}
	// Exactly the smoke budget, shared by writer and cleanup creates.
	s := &realAWSSandbox{manifest: smoke, owned: make(map[string]string)}
	s.manifest.ExpiresAt = time.Now().Add(time.Hour)
	for i := range realAWSSmokeMaxCreations {
		if err := s.reserveCreate(fmt.Sprintf("%sstatic/sqlite/secret-%d", smoke.Prefix, i)); err != nil {
			t.Fatal(err)
		}
	}
	if s.reserveCreate(smoke.Prefix+"static/sqlite/over") == nil {
		t.Fatal("smoke exceeded its creation budget")
	}
	if !matchesAWSPrincipal(smoke.Oracle.PrincipalARN, smoke.Oracle.PrincipalARN) || matchesAWSPrincipal("arn:aws:sts::123456789012:assumed-role/hikyo-smoke-oracle/session", smoke.Oracle.PrincipalARN) {
		t.Fatal("IAM user principal binding is not exact")
	}
}

// The emulator branch accepts only a static writer behind a non-AWS origin, and
// neither lane loads the other's manifest.
func TestAWSRealFixtureEmulatorScope(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	emulated := func() realAWSManifest {
		m := realAWSManifestFixture(now)
		m.Writers = map[string]realAWSActor{"static": {CredentialFile: "static.json", PrincipalARN: "arn:aws:iam::123456789012:role/static"}}
		m.Emulator = &realAWSEmulator{Origin: "https://127.0.0.1:5444", STSOrigin: "https://127.0.0.1:5444", CAFile: "/certs/ca.pem", AllowedCIDR: "127.0.0.1/32"}
		return m
	}
	if err := emulated().validateLane(now, true); err != nil {
		t.Fatal(err)
	}
	if emulated().maxCreations() != realAWSEmulatorMaxCreations {
		t.Fatal("emulator lane lost its own creation budget")
	}
	if emulated().validateLane(now, false) == nil || realAWSManifestFixture(now).validateLane(now, true) == nil {
		t.Fatal("AWS sandbox manifest loaded by the wrong lane")
	}
	if err := realAWSManifestFixture(now).validateLane(now, false); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*realAWSManifest){
		"aws-origin":    func(m *realAWSManifest) { m.Emulator.Origin = "https://secretsmanager.eu-west-1.amazonaws.com" },
		"aws-sts":       func(m *realAWSManifest) { m.Emulator.STSOrigin = "https://sts.eu-west-1.amazonaws.com" },
		"plaintext":     func(m *realAWSManifest) { m.Emulator.Origin = "http://127.0.0.1:5444" },
		"non-canonical": func(m *realAWSManifest) { m.Emulator.Origin = "https://127.0.0.1:5444/" },
		"missing-cidr":  func(m *realAWSManifest) { m.Emulator.AllowedCIDR = "" },
		"missing-ca":    func(m *realAWSManifest) { m.Emulator.CAFile = "" },
		"relative-ca":   func(m *realAWSManifest) { m.Emulator.CAFile = "ca.pem" },
		"extra-writer":  func(m *realAWSManifest) { m.Writers["ambient"] = m.Oracle },
		"no-static": func(m *realAWSManifest) {
			m.Writers = map[string]realAWSActor{"ambient": {CredentialFile: "a.json", PrincipalARN: "arn:aws:iam::123456789012:role/a"}}
		},
		"shared-identity":  func(m *realAWSManifest) { m.Cleanup = m.Oracle },
		"emulator-account": func(m *realAWSManifest) { m.Account = "000000000000" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := emulated()
			change(&m)
			if m.validateLane(now, true) == nil {
				t.Fatal("unsafe fixture scope accepted")
			}
		})
	}
}

// bindEndpoint gives the emulator its own origins, CA and CIDR, and leaves real
// AWS on the regional origin with system roots.
func TestAWSRealFixtureEndpointBinding(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.NotFoundHandler())
	server.Close()
	dir := t.TempDir()
	ca, empty := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "empty.pem")
	if os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600) != nil || os.WriteFile(empty, nil, 0o600) != nil {
		t.Fatal("cannot write CA fixtures")
	}
	regional := &realAWSSandbox{manifest: realAWSManifestFixture(now)}
	if err := regional.bindEndpoint(); err != nil || regional.origin != "https://secretsmanager.eu-west-1.amazonaws.com" || regional.stsOrigin != "" || regional.roots != nil || regional.allowed != nil {
		t.Fatal("real AWS binding changed")
	}
	if cfg := regional.endpointConfig(awssm.ClientConfig{}); cfg.RootCAs != nil || cfg.AllowedCIDRs != nil || cfg.STSAllowedCIDRs != nil {
		t.Fatal("regional AWS client gained emulator trust")
	}
	m := realAWSManifestFixture(now)
	m.Emulator = &realAWSEmulator{Origin: "https://127.0.0.1:5444", STSOrigin: "https://127.0.0.1:5445", CAFile: ca, AllowedCIDR: "127.0.0.1/32"}
	emulated := &realAWSSandbox{manifest: m}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	if err := emulated.bindEndpoint(); err != nil || !emulated.emulated() || emulated.origin != m.Emulator.Origin || emulated.stsOrigin != m.Emulator.STSOrigin || emulated.roots == nil || !slices.Equal(emulated.allowed, want) {
		t.Fatal("emulator binding lost its endpoint")
	}
	if cfg := emulated.endpointConfig(awssm.ClientConfig{}); cfg.RootCAs != emulated.roots || !slices.Equal(cfg.AllowedCIDRs, want) || !slices.Equal(cfg.STSAllowedCIDRs, want) {
		t.Fatal("emulator client lost its CA or CIDR")
	}
	m.Emulator.CAFile = empty
	if (&realAWSSandbox{manifest: m}).bindEndpoint() == nil {
		t.Fatal("emulator CA without a certificate accepted")
	}
}

func TestAWSRealFixtureBudgetBound(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		name   string
		budget float64
		valid  bool
	}{
		{"stricter fixture budget", 1, true},
		{"approved completion estimate", 2.29, true},
		{"gross target", 5, true},
		{"absolute maximum", 10, true},
		{"above maximum", math.Nextafter(10, math.Inf(1)), false},
		{"zero", 0, false},
		{"negative", -1, false},
		{"not a number", math.NaN(), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manifest := realAWSManifestFixture(now)
			manifest.BudgetUSD = scenario.budget
			if err := manifest.validate(now); (err == nil) != scenario.valid {
				t.Fatalf("budget validity = %t, want %t", err == nil, scenario.valid)
			}
		})
	}
	raw, err := json.Marshal(realAWSManifestFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"NaN", "Infinity", "-Infinity", "1e999", `"10"`} {
		t.Run("invalid JSON budget/"+invalid, func(t *testing.T) {
			body := strings.Replace(string(raw), `"budget_usd":1`, `"budget_usd":`+invalid, 1)
			if _, err := decodeAWSManifest([]byte(body)); err == nil {
				t.Fatal("invalid JSON budget accepted")
			}
		})
	}
}

func TestAWSRealFixturePrincipalBinding(t *testing.T) {
	want := "arn:aws:iam::123456789012:role/test/writer"
	for _, actual := range []string{want, "arn:aws:sts::123456789012:assumed-role/writer/session"} {
		if !matchesAWSPrincipal(actual, want) {
			t.Fatal("expected role session refused")
		}
	}
	for _, actual := range []string{"arn:aws:sts::111122223333:assumed-role/writer/session", "arn:aws:sts::123456789012:assumed-role/cleanup/session", "arn:aws:sts::123456789012:assumed-role/writer/", "arn:aws:sts::123456789012:assumed-role/writer/session/extra"} {
		if matchesAWSPrincipal(actual, want) {
			t.Fatal("unapproved role session accepted")
		}
	}
}

func TestAWSRealFixtureProtectedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real AWS fixture custody requires POSIX permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte("synthetic input"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAWSProtectedFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAWSProtectedFile(path); err == nil {
		t.Fatal("world-readable fixture accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readAWSProtectedFile(link); err == nil {
		t.Fatal("symlink fixture accepted")
	}
	if err := os.WriteFile(path, make([]byte, 32769), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAWSProtectedFile(path); err == nil {
		t.Fatal("oversized fixture accepted")
	}
}

func TestAWSRealFixtureExactResourceGuard(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	s := &realAWSSandbox{manifest: m, owned: make(map[string]string)}
	name := m.Prefix + "static/sqlite/document"
	arn := "arn:aws:secretsmanager:" + m.Region + ":" + m.Account + ":secret:" + name + "-ABC123"
	if err := s.reserveCreate(name); err != nil {
		t.Fatal(err)
	}
	if err := s.remember(name, arn); err != nil {
		t.Fatal(err)
	}
	a := &realAWSWriter{s: s}
	if err := a.mutation(arn, "PutSecretValue"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{name, arn + "other", strings.Replace(arn, m.Account, "111122223333", 1)} {
		if a.mutation(id, "DeleteSecret") == nil {
			t.Fatal("mutation accepted an unrecorded resource")
		}
	}
	if s.remember("outside/secret", arn) == nil {
		t.Fatal("resource outside run accepted")
	}
	// Same-name replacement must change lookup while retaining both exact
	// ARNs for cleanup; map iteration must not randomly select the old ARN.
	replacement := strings.TrimSuffix(arn, "ABC123") + "DEF456"
	if err := s.reserveCreate(name); err != nil {
		t.Fatal(err)
	}
	if err := s.remember(name, replacement); err != nil {
		t.Fatal(err)
	}
	if s.arn(t, name) != replacement || s.owned[arn] != name || s.owned[replacement] != name {
		t.Fatal("replacement lookup lost current identity or historical cleanup custody")
	}
	s.manifest.ExpiresAt = time.Now().Add(-time.Second)
	if a.mutation(arn, "PutSecretValue") == nil {
		t.Fatal("mutation outlived its authorization")
	}
}

func TestAWSRealFixtureSharedCreationBudget(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	s := &realAWSSandbox{manifest: m, owned: make(map[string]string)}
	// Every engine and auth group uses this same object. Reserve more than
	// the ceiling concurrently to expose check/increment races.
	var workers sync.WaitGroup
	results := make(chan error, 96)
	for i := range 96 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- s.reserveCreate(fmt.Sprintf("%sgroup-%d/secret-%d", m.Prefix, i%6, i))
		}()
	}
	workers.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != m.maxCreations() || len(s.creations) != m.maxCreations() {
		t.Fatal("creation guard is not atomic across the run")
	}
	// No resources were recorded: failed/ambiguous calls still spend their
	// reservations. Neither writer nor cleanup can reach its transport.
	writer := &realAWSWriter{s: s}
	if err := writer.CreateSecret(context.Background(), awssm.CreateSecretInput{Name: m.Prefix + "writer"}); err == nil {
		t.Fatal("writer bypassed the shared creation budget")
	}
	if err := s.call(context.Background(), false, "CreateSecret", m.Prefix+"seed", map[string]any{}, nil); err == nil {
		t.Fatal("cleanup seed bypassed the shared creation budget")
	}
	if len(s.creations) != m.maxCreations() {
		t.Fatal("refusal changed the exhausted budget")
	}
}

func TestAWSRealFixtureCreationReservationsAreCumulative(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	s := &realAWSSandbox{manifest: m, owned: make(map[string]string)}
	name := m.Prefix + "replacement"
	for range m.maxCreations() {
		if err := s.reserveCreate(name); err != nil {
			t.Fatal(err)
		}
	}
	if s.reserveCreate(name) == nil {
		t.Fatal("same-name retries reset the creation budget")
	}
	if s.remember(m.Prefix+"unreserved", "arn:aws:secretsmanager:"+m.Region+":"+m.Account+":secret:"+m.Prefix+"unreserved-ABC123") == nil {
		t.Fatal("unreserved resource accepted")
	}
	if s.reserveCreate("outside/fixture") == nil {
		t.Fatal("foreign name reserved")
	}
}

func TestAWSRealFixtureSingleUseRunClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real AWS fixture custody requires POSIX permissions")
	}
	m := realAWSManifestFixture(time.Now())
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := claimRealAWSRun(dir, m); err != nil {
		t.Fatal(err)
	}
	// A new test process sees the same on-disk marker. Changing only the
	// authorization label must not reset the approved namespace's allowance.
	m.AuthorizationID = "changed-label"
	if claimRealAWSRun(dir, m) == nil {
		t.Fatal("used namespace was reclaimable")
	}
	m.Prefix = "hikyo-e2e-anotherrun/"
	if err := claimRealAWSRun(dir, m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	m.Prefix = "hikyo-e2e-thirdrun/"
	if claimRealAWSRun(dir, m) == nil {
		t.Fatal("nonprivate fixture directory accepted")
	}
}
