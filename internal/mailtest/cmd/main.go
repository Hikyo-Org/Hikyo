// Command mailtest exposes the existing TLS SMTP fixture to browser tests.
// It binds only loopback and emits JSON lines on stdout. Closing stdin or
// sending an interrupt stops it; no production server imports this command.
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Hikyo-Org/hikyo/internal/mailtest"
)

type ready struct {
	Kind  string `json:"kind"`
	Addr  string `json:"addr"`
	CAPEM string `json:"ca_pem"`
}

type message struct {
	Kind       string   `json:"kind"`
	From       string   `json:"from"`
	Recipients []string `json:"recipients"`
	Data       string   `json:"data"`
	TLS        string   `json:"tls"`
}

func run() error {
	encoder := json.NewEncoder(os.Stdout)
	var output sync.Mutex
	// Keep readiness first even if a client connects as soon as the sink binds.
	output.Lock()
	sink, err := mailtest.Start(mailtest.Options{Mode: "implicit", OnMessage: func(m mailtest.Message) error {
		var protocol string
		switch m.TLSVersion {
		case tls.VersionTLS12:
			protocol = "TLSv1.2"
		case tls.VersionTLS13:
			protocol = "TLSv1.3"
		default:
			return fmt.Errorf("unexpected mail sink TLS version %d", m.TLSVersion)
		}
		output.Lock()
		defer output.Unlock()
		return encoder.Encode(message{Kind: "message", From: m.From, Recipients: m.Recipients, Data: m.Data, TLS: protocol})
	}})
	if err != nil {
		output.Unlock()
		return err
	}
	err = encoder.Encode(ready{Kind: "ready", Addr: sink.Addr, CAPEM: sink.CAPEM})
	output.Unlock()
	defer sink.Close()
	if err != nil {
		return err
	}
	closed := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(closed) }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case <-closed:
	case <-stop:
	}
	return sink.Close()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
