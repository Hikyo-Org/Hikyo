package hostupgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigRejectsAuthorityAndEndpointInjection(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Config){
		"arbitrary service":         func(c *Config) { c.Unit = "sshd.service" },
		"argument injection":        func(c *Config) { c.Unit = "hikyo.service --all" },
		"root runtime":              func(c *Config) { c.User = "root" },
		"path traversal":            func(c *Config) { c.Binary = "/usr/bin/../bin/hikyo" },
		"systemd specifier":         func(c *Config) { c.Binary = "/usr/bin/%n" },
		"private inside writable":   func(c *Config) { c.CustodyDirectory = c.WorkingDirectory + "/keys" },
		"candidate inside writable": func(c *Config) { c.CandidateDirectory = c.PublicDirectory + "/bin" },
		"remote health":             func(c *Config) { c.HealthURL = "http://192.0.2.1:8081/readyz" },
		"DNS health":                func(c *Config) { c.HealthURL = "http://localhost:8081/readyz" },
		"health userinfo":           func(c *Config) { c.HealthURL = "http://user@127.0.0.1:8081/readyz" },
		"wrong probe":               func(c *Config) { c.HealthURL = "http://127.0.0.1:8081/healthz" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := DefaultConfig()
			change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted unsafe config")
			}
		})
	}
}

func TestEnvironmentIsDataNeverShellCode(t *testing.T) {
	values, err := ParseEnvironmentFile([]byte("# comment\nHIKYO_DB = 'sqlite:/var/lib/hikyo/db.sqlite'\nHIKYO_EXTERNAL_ORIGIN=\"https://example.test\"\nLITERAL=$(touch /tmp/not-executed)\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["HIKYO_DB"] != "sqlite:/var/lib/hikyo/db.sqlite" || values["LITERAL"] != "$(touch /tmp/not-executed)" {
		t.Fatalf("unexpected parsing: %#v", values)
	}
	for _, input := range []string{"export HIKYO_DB=sqlite:x", "HIKYO_DB=one\nHIKYO_DB=two", "HIKYO_DB=\"unterminated", "HIKYO_DB=one\\\ntwo", "HIKYO_DB=foo\"bar\"", "HIKYO_DB=bad\x00value"} {
		if _, err := ParseEnvironmentFile([]byte(input)); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func standardProperties() map[string]string {
	c := DefaultConfig()
	return map[string]string{"User": c.User, "Group": c.Group, "WorkingDirectory": c.WorkingDirectory, "EnvironmentFiles": c.EnvironmentFile + " (ignore_errors=no)", "ExecStart": "{ path=/usr/bin/hikyo ; argv[]=/usr/bin/hikyo server --root-key-file=/run/credentials/hikyo.service/hikyo-root-key ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", "KillMode": "control-group"}
}

func TestUnitValidationRequiresExactDeployment(t *testing.T) {
	if err := validateUnit(DefaultConfig(), standardProperties()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"User": "root", "EnvironmentFiles": "/tmp/user-controlled.env (ignore_errors=no)", "KillMode": "process", "ExecStartPre": "/bin/sh -c arbitrary", "RootDirectory": "/other", "ExecStart": "{ path=/usr/bin/hikyo ; argv[]=/usr/bin/hikyo server --root-key-file=/run/credentials/hikyo.service/hikyo-root-key --dev ; ignore_errors=no }"}
	for property, value := range cases {
		t.Run(property, func(t *testing.T) {
			p := standardProperties()
			p[property] = value
			if err := validateUnit(DefaultConfig(), p); err == nil {
				t.Fatal("accepted changed unit")
			}
		})
	}
	p := standardProperties()
	p["ExecStart"] += " { path=/bin/sh ; argv[]=/bin/sh -c unsafe ; }"
	if err := validateUnit(DefaultConfig(), p); err == nil {
		t.Fatal("accepted a second process")
	}
}

func TestStoppedProofRefusesSurvivingUnitWriter(t *testing.T) {
	for _, output := range []string{"ActiveState=active\nMainPID=123\nControlGroup=\n", "ActiveState=deactivating\nMainPID=0\nControlGroup=\n", "ActiveState=inactive\nMainPID=0\nControlGroup=/../../outside\n", "ActiveState=inactive\nControlGroup=\n"} {
		h := &Host{config: DefaultConfig(), run: func(context.Context, command) ([]byte, error) { return []byte(output), nil }}
		if err := h.verifyStopped(context.Background()); err == nil {
			t.Fatalf("accepted incomplete stop: %q", output)
		}
	}
	h := &Host{config: DefaultConfig(), run: func(context.Context, command) ([]byte, error) {
		return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
	}}
	if err := h.verifyStopped(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupProofIncludesDescendants(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "worker"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "worker", "cgroup.procs")
	if err := os.WriteFile(child, []byte("8123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := emptyControlGroup(dir); err == nil {
		t.Fatal("accepted a surviving child writer")
	}
	if err := os.WriteFile(child, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := emptyControlGroup(dir); err != nil {
		t.Fatal(err)
	}
}

func TestWriterProofChecksProcessesOutsideUnit(t *testing.T) {
	root := t.TempDir()
	fd := filepath.Join(root, "456", "fd")
	if err := os.MkdirAll(fd, 0700); err != nil {
		t.Fatal(err)
	}
	database := "/var/lib/hikyo/hikyo.db"
	if err := os.Symlink(database+"-wal", filepath.Join(fd, "3")); err != nil {
		t.Fatal(err)
	}
	if err := noOpenDatabaseDescriptors(root, database); err == nil {
		t.Fatal("ignored external WAL writer")
	}
	if err := os.Remove(filepath.Join(fd, "3")); err != nil {
		t.Fatal(err)
	}
	if err := noOpenDatabaseDescriptors(root, database); err != nil {
		t.Fatal(err)
	}
}

func TestSystemctlUsesFixedArgvAndReportsFailure(t *testing.T) {
	wantErr := errors.New("exit status 1")
	h := &Host{config: DefaultConfig(), run: func(ctx context.Context, c command) ([]byte, error) {
		if c.path != "/usr/bin/systemctl" || !reflect.DeepEqual(c.args, []string{"--no-pager", "--no-ask-password", "stop", "hikyo.service"}) {
			t.Fatalf("unexpected command: %#v", c)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("systemctl command has no deadline")
		}
		return nil, wantErr
	}}
	if _, err := h.systemctl(context.Background(), "stop", h.config.Unit); !errors.Is(err, wantErr) {
		t.Fatalf("stop failure lost: %v", err)
	}
}

func TestRuntimeEnvironmentStripsOperatorSecretsAndStaleEvidence(t *testing.T) {
	h := &Host{config: DefaultConfig(), env: map[string]string{"HIKYO_DB": "sqlite:/var/lib/hikyo/hikyo.db", "HIKYO_UPGRADE_OPERATOR_INSTANCE": "stale", "HIKYO_UPGRADE_BACKUP": "old", "COSIGN_PASSWORD": "operator-secret", "LD_PRELOAD": "/malicious.so"}}
	e := RuntimeEvidence{BundleDirectory: h.config.PublicDirectory + "/bundle", OperatorPublicKey: h.config.PublicDirectory + "/operator.pub"}
	values, err := h.runtimeEnvironment(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range environmentList(values) {
		if strings.Contains(entry, "operator-secret") || strings.Contains(entry, "malicious") || strings.Contains(entry, "stale") || strings.HasPrefix(entry, "HIKYO_UPGRADE_BACKUP=") {
			t.Fatalf("leaked environment %q", entry)
		}
	}
	if values["HIKYO_ROOT_KEY_FILE"] != "/proc/self/fd/3" {
		t.Fatal("root key is not delivered through the inherited descriptor")
	}
	e.BundleDirectory = "/etc/hikyo/upgrade-keys"
	if _, err = h.runtimeEnvironment(e); err == nil {
		t.Fatal("accepted private runtime bundle path")
	}
}

func TestRuntimeCommandCannotSelectArbitraryExecutable(t *testing.T) {
	h := &Host{config: DefaultConfig(), run: func(context.Context, command) ([]byte, error) { t.Fatal("ran an unapproved command"); return nil, nil }}
	if _, err := h.Migrate(context.Background(), "/bin/sh", RuntimeEvidence{}); err == nil {
		t.Fatal("accepted shell as migration target")
	}
	if _, err := h.Export(context.Background(), "/bin/sh", ExportRequest{OutputDirectory: "/etc/hikyo/upgrade-keys", Recipient: "age1recipient"}); err == nil {
		t.Fatal("accepted private output")
	}
}
