//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (p *probe) smoke(ctx context.Context, dir string, revoke func() error) error {
	binary := os.Getenv("MCP651_SMOKE")
	if !filepath.IsAbs(binary) {
		return errors.New("absolute current-source public smoke executable required")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--url", p.target.URL, "--org", string(p.fixture.Org), "--project", string(p.fixture.Project), "--revocation-timeout", "20s", "--json")
	cmd.Env = append(clientEnvironment(dir, p.target.CAFile), "HIKYO_MCP_TOKEN="+p.fixture.Credentials["live"].Token, "HIKYO_MCP_ROTATING_TOKEN="+p.fixture.Credentials["smoke"].Token)
	status, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	if cmd.Start() != nil {
		return errors.New("current smoke launch failed")
	}
	scanner := bufio.NewScanner(status)
	revoked := false
	for scanner.Scan() {
		if !p.safe(scanner.Bytes()) {
			cancel()
			cmd.Wait()
			return errors.New("smoke disclosed protected material")
		}
		if scanner.Text() == "MCP rotating credential proved live; revoke it now" && !revoked {
			if err := revoke(); err != nil {
				cancel()
				cmd.Wait()
				return errors.New("supported smoke revocation failed")
			}
			revoked = true
		}
	}
	if cmd.Wait() != nil || scanner.Err() != nil || !revoked || !p.safe(output.Bytes()) {
		return errors.New("current smoke failed; output withheld")
	}
	var r struct {
		Scope       string `json:"scope"`
		Protocol    string `json:"protocol"`
		Observation string `json:"revocation_observation"`
		Passed      bool   `json:"passed"`
	}
	if json.Unmarshal(output.Bytes(), &r) != nil || !r.Passed || r.Scope != "endpoint_smoke_only" || r.Observation != "polling_only_no_commit_receipt" {
		return errors.New("bounded smoke receipt missing")
	}
	if err := writePrivate(filepath.Join(dir, "smoke-receipt.json"), json.RawMessage(output.Bytes())); err != nil {
		return err
	}
	p.receipt.Checks["existing_public_smoke_polling_only_replacement_recheck"] = "PASS; separate predecessor from first-call proof"
	return nil
}
