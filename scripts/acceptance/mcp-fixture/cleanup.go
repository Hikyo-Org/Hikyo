//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func stopServer(p *serverProcess, grace time.Duration) {
	if p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func cleanupDocker(name, container, network string, run func(...string) (string, error)) bool {
	clean := true
	for _, resource := range []struct{ kind, id string }{{"container", container}, {"network", network}} {
		ids := []string{resource.id}
		if resource.id == "" {
			args := []string{"ps", "-aq", "--filter", "name=^/" + name + "$", "--filter", "label=hikyo.acceptance=651"}
			if resource.kind == "network" {
				args = []string{"network", "ls", "-q", "--filter", "name=^" + name + "$", "--filter", "label=hikyo.acceptance=651"}
			}
			out, err := run(args...)
			if err != nil {
				clean = false
				continue
			}
			ids = strings.Fields(out)
		}
		for _, id := range ids {
			if !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(id) {
				clean = false
				continue
			}
			args := []string{"rm", "-f", id}
			if resource.kind == "network" {
				args = []string{"network", "rm", id}
			}
			if _, err := run(args...); err != nil {
				clean = false
			}
			inspect := []string{"inspect", id}
			if resource.kind == "network" {
				inspect = []string{"network", "inspect", id}
			}
			if _, err := run(inspect...); err == nil {
				clean = false
			}
		}
	}
	return clean
}

func boundedDocker(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := command(ctx, "docker", args...)
	if ctx.Err() != nil {
		return "", errors.New("owned Docker cleanup timeout")
	}
	return out, err
}
