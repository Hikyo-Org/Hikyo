package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// These tests are local fixture guards. They do not contact AWS and do not
// count as real provider acceptance.
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
	for _, mode := range []string{"static", "ambient", "assume-role", "web-identity", "kms-denied"} {
		a := actor(mode)
		switch mode {
		case "static":
			a.ExpiredCredentialFile = "static-expired.json"
		case "ambient", "web-identity":
			a.TokenFile = mode + ".jwt"
			a.ExpiredTokenFile = mode + "-expired.jwt"
			a.WorkloadRoleARN = a.PrincipalARN
		case "assume-role":
			a.SourceFile = "source.json"
			a.ExpiredSourceFile = "source-expired.json"
			a.SourcePrincipalARN = "arn:aws:iam::123456789012:role/source"
		}
		m.Writers[mode] = a
	}
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
		"user-identity": func(m *realAWSManifest) {
			m.Oracle.PrincipalARN = "arn:aws:iam::123456789012:user/oracle"
		},
		"foreign-source": func(m *realAWSManifest) {
			a := m.Writers["assume-role"]
			a.SourcePrincipalARN = "arn:aws:iam::111122223333:role/source"
			m.Writers["assume-role"] = a
		},
		"borrowed-source": func(m *realAWSManifest) {
			a := m.Writers["assume-role"]
			a.SourcePrincipalARN = m.Cleanup.PrincipalARN
			m.Writers["assume-role"] = a
		},
		"missing-mode":     func(m *realAWSManifest) { delete(m.Writers, "ambient") },
		"missing-expired-static": func(m *realAWSManifest) {
			a := m.Writers["static"]
			a.ExpiredCredentialFile = ""
			m.Writers["static"] = a
		},
		"missing-expired-source": func(m *realAWSManifest) {
			a := m.Writers["assume-role"]
			a.ExpiredSourceFile = ""
			m.Writers["assume-role"] = a
		},
		"missing-expired-token": func(m *realAWSManifest) {
			a := m.Writers["ambient"]
			a.ExpiredTokenFile = ""
			m.Writers["ambient"] = a
		},
		"extra-mode":       func(m *realAWSManifest) { m.Writers["production"] = m.Oracle },
		"expired":          func(m *realAWSManifest) { m.ExpiresAt = now },
		"unbounded-time":   func(m *realAWSManifest) { m.ExpiresAt = now.Add(3 * time.Hour) },
		"unbounded-prefix": func(m *realAWSManifest) { m.Prefix = "*" },
		"missing-approval": func(m *realAWSManifest) { m.AuthorizationID = "" },
		"missing-budget":   func(m *realAWSManifest) { m.BudgetUSD = 0 },
		"excess-budget":    func(m *realAWSManifest) { m.BudgetUSD = 2 },
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
	// the full-suite ceiling concurrently to expose check/increment races.
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
	if accepted != realAWSMaxCreations || len(s.creations) != realAWSMaxCreations {
		t.Fatal("creation guard is not atomic across the full suite")
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
	if len(s.creations) != realAWSMaxCreations {
		t.Fatal("refusal changed the exhausted budget")
	}
}

func TestAWSRealFixtureCreationReservationsAreCumulative(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	s := &realAWSSandbox{manifest: m, owned: make(map[string]string)}
	name := m.Prefix + "replacement"
	for range realAWSMaxCreations {
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
