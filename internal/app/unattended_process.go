package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// This adapter runs signed intermediate health checks on loopback only. The
// final image's health gate runs in this process without starting its workers.
// InstallBinary intentionally selects a child path, never replaces the image.
type unattendedProcessHost struct {
	cfg       *config.Config
	root      []byte
	directory string
	options   UnattendedUpgradeOptions
	candidate string
	command   *exec.Cmd
	done      chan error
	logPath   string
}

func (h *unattendedProcessHost) FenceAndStop(ctx context.Context) error {
	if h.command == nil {
		return nil
	}
	command, done := h.command, h.done
	h.command, h.done = nil, nil
	defer os.Remove(h.logPath)
	_ = command.Process.Signal(os.Interrupt)
	stop, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-done:
		return nil
	case <-stop.Done():
		_ = command.Process.Kill()
		<-done
		return nil
	}
}

func (h *unattendedProcessHost) Migrate(ctx context.Context, candidate string, evidence hostupgrade.RuntimeEvidence) ([]byte, error) {
	if h.options.Progress != nil {
		h.options.Progress("migration")
	}
	applyUnattendedEvidence(h.cfg, evidence)
	command, closeCredential, err := h.child(ctx, candidate, "migrate")
	if err != nil {
		return nil, err
	}
	defer closeCredential()
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("authenticated migration candidate failed: %w", err)
	}
	return nil, nil
}

func (h *unattendedProcessHost) InstallBinary(_ context.Context, candidate, digest string) error {
	actual, err := fileDigest(candidate)
	if err != nil || strings.TrimPrefix(actual, "sha256:") != digest {
		return errors.New("candidate executable differs from authenticated payload")
	}
	h.candidate = candidate
	return nil
}

func (h *unattendedProcessHost) ConfigureRuntime(_ context.Context, evidence hostupgrade.RuntimeEvidence) error {
	applyUnattendedEvidence(h.cfg, evidence)
	return nil
}
func (h *unattendedProcessHost) Complete(context.Context) error                { return nil }
func (h *unattendedProcessHost) PrunePublic(hostupgrade.RuntimeEvidence) error { return nil }

func (h *unattendedProcessHost) StartCandidate(ctx context.Context, digest string, final bool, timeout time.Duration) error {
	if h.options.Progress != nil {
		h.options.Progress("health-check")
	}
	if final {
		_, err := databaseGate(ctx, h.cfg, h.root, upgradegate.Boot)
		return err
	}
	actual, err := fileDigest(h.candidate)
	if err != nil || strings.TrimPrefix(actual, "sha256:") != digest || h.command != nil {
		return errors.New("intermediate candidate is not a verified stopped executable")
	}
	command, closeCredential, err := h.child(ctx, h.candidate, "server", "--auto-migrate=false", "--listen=localhost:0", "--operational-listen=127.0.0.1:0")
	if err != nil {
		return err
	}
	defer closeCredential()
	log, err := os.CreateTemp(h.directory, "candidate-*.log")
	if err != nil {
		return err
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		os.Remove(log.Name())
		return err
	}
	h.command, h.done, h.logPath = command, make(chan error, 1), log.Name()
	done := h.done
	go func() { done <- command.Wait() }()
	check, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	for {
		select {
		case err := <-done:
			h.command, h.done = nil, nil
			return fmt.Errorf("intermediate health candidate exited: %w", err)
		case <-check.Done():
			return errors.New("intermediate health candidate did not prove readiness before timeout")
		case <-ticker.C:
			info, err := log.Stat()
			if err != nil {
				return err
			}
			if info.Size() > 1<<20 {
				return errors.New("intermediate health candidate log exceeds bound")
			}
			raw, err := io.ReadAll(io.NewSectionReader(log, 0, info.Size()))
			if err != nil {
				return err
			}
			for _, line := range bytes.Split(raw, []byte("\n")) {
				var event struct {
					Message     string `json:"msg"`
					Operational string `json:"operational_addr"`
				}
				if json.Unmarshal(line, &event) != nil || !strings.HasPrefix(event.Message, "maintenance active;") || !strings.HasPrefix(event.Operational, "127.0.0.1:") {
					continue
				}
				all := true
				for path, expected := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusServiceUnavailable} {
					request, err := http.NewRequestWithContext(check, http.MethodGet, "http://"+event.Operational+path, nil)
					if err != nil {
						return err
					}
					response, err := client.Do(request)
					if err != nil {
						all = false
						break
					}
					response.Body.Close()
					if response.StatusCode != expected {
						all = false
						break
					}
				}
				if all {
					return nil
				}
			}
		}
	}
}

func (h *unattendedProcessHost) child(ctx context.Context, executable string, args ...string) (*exec.Cmd, func(), error) {
	credential, credentialPath, err := unattendedRootCredential(h.cfg, h.root)
	if err != nil {
		return nil, nil, err
	}
	closeCredential := func() {
		if credential != nil {
			credential.Close()
		}
	}
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(key, "HIKYO_UPGRADE_") || key == "HIKYO_ROOT_KEY" || key == "HIKYO_ROOT_KEY_FILE" {
			continue
		}
		values[key] = value
	}
	values["HIKYO_DB"] = h.cfg.Store.DSN
	if h.cfg.Store.Engine == config.EngineSQLite {
		values["HIKYO_DB"] = "sqlite:" + h.cfg.Store.Path
	}
	values["HIKYO_UPGRADE_BUNDLE"], values["HIKYO_UPGRADE_STATE_DIR"] = h.cfg.Upgrade.BundleDirectory, h.cfg.Upgrade.StateDirectory
	values["HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY"] = h.cfg.Upgrade.OperatorPublicKeyFile
	values["HIKYO_UPGRADE_TARGET_MANIFEST"] = h.cfg.Upgrade.TargetManifestSHA256
	values["HIKYO_UPGRADE_EVIDENCE"], values["HIKYO_UPGRADE_BACKUP"] = h.cfg.Upgrade.EvidenceDirectory, h.cfg.Upgrade.CiphertextPath
	values["HIKYO_ROOT_KEY_FILE"] = credentialPath
	command := exec.CommandContext(ctx, executable, args...)
	if credential != nil {
		command.ExtraFiles = []*os.File{credential}
	}
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}
	return command, closeCredential, nil
}
