//go:build darwin || linux

package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// Query current recorder state through an owner-only local socket. A cached
// receipt cannot prove success after an observation limit or disk failure.
func serveWireSnapshot(ctx context.Context, dir string, recorder *wireRecorder) (func(), error) {
	path := filepath.Join(dir, "wire-status.sock")
	if info, err := os.Lstat(path); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode()&os.ModeSocket == 0 || !ok || int(stat.Uid) != os.Geteuid() {
			return nil, errors.New("unsafe wire snapshot socket")
		}
		connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			connection.Close()
			return nil, errors.New("wire snapshot socket already active")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, errors.New("wire socket state unknown")
		}
		if os.Remove(path) != nil {
			return nil, errors.New("stale owned wire socket unavailable")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, errors.New("wire snapshot listener unavailable")
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connection.SetWriteDeadline(time.Now().Add(time.Second))
			_ = json.MarshalEncode(jsontext.NewEncoder(connection, jsonwire.Stable), recorder.snapshot())
			connection.Close()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		case <-done:
		}
	}()
	return func() { listener.Close(); <-done }, nil
}

func fetchWireSnapshot(ctx context.Context, dir string) (wireReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "wire-status.sock"))
	if err != nil {
		return wireReceipt{}, errors.New("live wire snapshot unavailable")
	}
	defer connection.Close()
	deadline, _ := ctx.Deadline()
	connection.SetReadDeadline(deadline)
	decoder := jsontext.NewDecoder(io.LimitReader(connection, 1<<20))
	var document wireReceipt
	if json.UnmarshalDecode(decoder, &document, json.RejectUnknownMembers(true)) != nil || document.Schema != "hikyo.dev/mcp-wire/v1" {
		return wireReceipt{}, errors.New("invalid live wire snapshot")
	}
	if _, err := decoder.ReadValue(); !errors.Is(err, io.EOF) {
		return wireReceipt{}, errors.New("incomplete live wire snapshot")
	}
	return document, nil
}
func readWireSnapshot(ctx context.Context, dir string) error {
	document, err := fetchWireSnapshot(ctx, dir)
	if err != nil {
		return err
	}
	return json.MarshalEncode(jsontext.NewEncoder(os.Stdout, jsonwire.Stable), document)
}
