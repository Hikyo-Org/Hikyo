//go:build linux

package hostupgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This test must run in the disposable PID-1 systemd container created by
// scripts/ci/test-host-upgrade-systemd.sh. It never substitutes systemctl.
func TestRealSystemdUpgrade(t *testing.T) {
	if os.Getenv("HIKYO_REAL_SYSTEMD_ACCEPTANCE") != "1" {
		t.Skip("requires the isolated real-systemd acceptance container")
	}
	pidOne, err := os.ReadFile("/proc/1/comm")
	if err != nil || strings.TrimSpace(string(pidOne)) != "systemd" {
		t.Fatal("acceptance requires actual systemd as PID 1")
	}
	ctx := t.Context()
	c := DefaultConfig()
	h, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, value string, mode os.FileMode) { t.Helper(); must(os.WriteFile(path, []byte(value), mode)) }
	must(os.MkdirAll("/etc/hikyo", 0755))
	must(os.MkdirAll(c.WorkingDirectory, 0700))
	must(os.Chown(c.WorkingDirectory, int(h.uid), int(h.gid)))
	must(os.MkdirAll("/var/backups/hikyo", 0700))
	write(c.EnvironmentFile, "HIKYO_DB=sqlite:hikyo.db\n", 0600)
	write(c.RootKeyFile, strings.Repeat("a", 64), 0600)
	write(filepath.Join(c.WorkingDirectory, "hikyo.db"), "adapter-only database sentinel", 0600)
	write(filepath.Join(c.WorkingDirectory, "ready"), "ready", 0644)
	must(h.PrepareDirectories())
	write(filepath.Join(c.CustodyDirectory, "operator.age"), "operator-only encrypted fixture", 0600)
	digest, err := fileDigest("/host-upgrade-helper")
	must(err)
	must(copyBinary("/host-upgrade-helper", c.Binary, digest))
	unit := `[Unit]
Description=Hikyo adapter acceptance
[Service]
Type=simple
User=hikyo
Group=hikyo
WorkingDirectory=/var/lib/hikyo
EnvironmentFile=/etc/hikyo/hikyo.env
LoadCredential=hikyo-root-key:/etc/hikyo/root.key
ExecStart=/usr/bin/hikyo server --root-key-file=%d/hikyo-root-key
Restart=on-failure
RestartSec=1s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectKernelLogs=true
ProtectClock=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/hikyo /var/backups/hikyo
[Install]
WantedBy=multi-user.target
`
	write("/etc/systemd/system/hikyo.service", unit, 0644)
	ctl := func(args ...string) { t.Helper(); _, err := h.systemctl(ctx, args...); must(err) }
	ctl("daemon-reload")
	ctl("start", c.Unit)
	serialized, err := h.systemctl(ctx, "show", "--all", "--property=LoadCredential,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,DropInPaths,RootDirectory,RootImage,KillMode", c.Unit)
	must(err)
	t.Logf("systemd optional property serialization: %s", serialized)
	t.Cleanup(func() { _, _ = h.systemctl(context.Background(), "stop", c.Unit) })
	must(h.Preflight(ctx))
	if h.cgroup == "" {
		t.Fatal("real service has no cgroup")
	}
	initialDeadline := time.Now().Add(5 * time.Second)
	for err = h.checkCandidate(ctx, digest, true); err != nil && time.Now().Before(initialDeadline); err = h.checkCandidate(ctx, digest, true) {
		time.Sleep(50 * time.Millisecond)
	}
	must(err)
	t.Log("original hardened unit passed preflight with real systemd and a service cgroup")
	must(h.FenceAndStop(ctx))
	candidate, err := h.StageCandidate("/host-upgrade-helper", digest)
	must(err)
	pub, err := h.PublishPublicEvidence("operator.pub", []byte("public verification fixture"))
	must(err)
	evidence := RuntimeEvidence{BundleDirectory: c.PublicDirectory, OperatorPublicKey: pub}
	t.Setenv("OPERATOR_PASSPHRASE", "must-not-reach-runtime")
	output, err := h.Migrate(ctx, candidate, evidence)
	must(err)
	if !strings.Contains(string(output), `"credential":"verified"`) {
		t.Fatal("runtime child did not verify descriptor custody")
	}
	outdir, err := h.PreparePublicOutput("export-test")
	must(err)
	_, err = h.Export(ctx, candidate, ExportRequest{OutputDirectory: outdir, Recipient: "fixture", Runtime: evidence})
	must(err)
	t.Log("real unprivileged migration/export children read memfd credential and could not read operator custody")
	must(h.InstallBinary(ctx, candidate, digest))
	must(h.ConfigureRuntime(ctx, evidence))
	serialized, err = h.systemctl(ctx, "show", "--all", "--property=EnvironmentFiles,ExecStart", c.Unit)
	must(err)
	t.Logf("systemd generated runtime serialization: %s", serialized)
	// A second preflight reads the actual systemd serialization of both files.
	must(h.Preflight(ctx))
	write(filepath.Join(c.WorkingDirectory, "ready"), "maintenance", 0644)
	must(h.StartCandidate(ctx, digest, false, 5*time.Second))
	must(h.FenceAndStop(ctx))
	write(filepath.Join(c.WorkingDirectory, "ready"), "ready", 0644)
	must(h.StartCandidate(ctx, digest, true, 5*time.Second))
	t.Log("generated runtime and fence drop-ins passed intermediate 503 and final 200 readiness")
	// A reboot discards /run. Reproduce that loss without restarting the test
	// process, then ask the real manager to start the enabled service.
	ctl("stop", c.Unit)
	must(os.RemoveAll(filepath.Dir(h.startPermission())))
	ctl("daemon-reload")
	ctl("start", c.Unit)
	must(h.verifyStopped(ctx))
	if active, err := h.Maintenance(); err != nil || !active {
		t.Fatal("transient-directory loss removed persistent maintenance")
	}
	t.Log("actual systemd refused start after loss of transient /run permission")
	must(h.StartCandidate(ctx, digest, true, 5*time.Second))
	must(h.Complete(ctx))
	ctl("restart", c.Unit)
	// StartCandidate cannot run after Complete; check the restarted process
	// directly with the same digest and HTTP identity proof.
	deadline := time.Now().Add(5 * time.Second)
	for err = h.checkCandidate(ctx, digest, true); err != nil && time.Now().Before(deadline); err = h.checkCandidate(ctx, digest, true) {
		time.Sleep(50 * time.Millisecond)
	}
	must(err)
	t.Log("completed upgrade restarted through real systemd without temporary permission")
	must(h.FenceAndStop(ctx))
	write(filepath.Join(c.WorkingDirectory, "ready"), "failed", 0644)
	if err := h.StartCandidate(ctx, digest, true, 300*time.Millisecond); err == nil {
		t.Fatal("failed readiness was accepted")
	}
	must(h.verifyStopped(ctx))
	if _, err := os.Lstat(h.startPermission()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed readiness retained transient start permission")
	}
	ctl("start", c.Unit)
	must(h.verifyStopped(ctx))
	t.Log("failed final readiness stopped the real cgroup and blocked subsequent systemd start")
}
