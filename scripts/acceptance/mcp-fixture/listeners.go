//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type operationalTargets struct {
	Addresses []string `json:"addresses"`
}
type listenerObservation struct {
	Accepting []bool `json:"accepting"`
}

func listenerAccepting(ctx context.Context, address string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err == nil {
		connection.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	return false, errors.New("listener observation unavailable; network failure is not shutdown proof")
}

func listeners(ctx context.Context, dir string) error {
	var targets operationalTargets
	if err := readPrivate(filepath.Join(dir, "operational-targets.json"), &targets); err != nil {
		return err
	}
	if len(targets.Addresses) != 2 || targets.Addresses[0] == targets.Addresses[1] {
		return errors.New("two independently bound operational sockets required")
	}
	_, network, _ := net.ParseCIDR("10.42.0.0/16")
	out := listenerObservation{Accepting: make([]bool, 2)}
	for i, address := range targets.Addresses {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "8081" || !network.Contains(net.ParseIP(host)) {
			return errors.New("fixture operational socket outside approved pod network")
		}
		out.Accepting[i], err = listenerAccepting(ctx, address)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func (c publicControl) stageOperationalTargets(ctx context.Context, pods []observedPod) error {
	value := operationalTargets{Addresses: []string{net.JoinHostPort(pods[0].Status.IP, "8081"), net.JoinHostPort(pods[1].Status.IP, "8081")}}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cmd := c.kubectl(ctx, "exec", "-i", "tooling", "--", "sh", "-eu", "-c", "umask 077; cat > /work/custody/operational-targets.json")
	cmd.Stdin = bytes.NewReader(b)
	if _, err := cmd.Output(); err != nil {
		return errors.New("operational observation binding failed")
	}
	return nil
}

func (c publicControl) listeners(ctx context.Context) (listenerObservation, error) {
	var out listenerObservation
	b, err := c.helper(ctx, "listeners")
	if err != nil {
		return out, err
	}
	if json.Unmarshal(b, &out) != nil || len(out.Accepting) != 2 {
		return out, errors.New("invalid listener observation")
	}
	return out, nil
}
