// Command hikyo-receiver-example is the reference receiver for Hikyo's sealed
// webhook adapter (docs/spec/sealed-webhook.md). It is documentation and a
// test harness, not a release artifact: it applies values to a local JSON
// state file so the protocol can be exercised end to end.
//
//	hikyo-receiver-example keygen -dir DIR
//	hikyo-receiver-example fingerprint -origin URL -recipient AGE -ack-key KEY -generation N
//	hikyo-receiver-example serve -config FILE -listen ADDR -tls-cert FILE -tls-key FILE -state FILE
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/sealedreceiver"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "hikyo-receiver-example:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: hikyo-receiver-example keygen|fingerprint|serve [flags]")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:], stdout)
	case "fingerprint":
		return fingerprint(args[1:], stdout)
	case "serve":
		return serve(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// keygen writes the receiver's age identity and acknowledgement key with
// mode 0600 and prints only the public halves an instance admin pins.
func keygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory for identity.age and ack.pem")
	if err := fs.Parse(args); err != nil {
		return err
	}
	identity, recipient, err := sealedhook.GenerateRecipient()
	if err != nil {
		return err
	}
	ackPEM, ackKey, err := sealedhook.GenerateSigningKey()
	if err != nil {
		return err
	}
	if err := writeSecret(filepath.Join(*dir, "identity.age"), []byte(identity+"\n")); err != nil {
		return err
	}
	if err := writeSecret(filepath.Join(*dir, "ack.pem"), ackPEM); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "recipient_key: %s\nack_key: %s\n", recipient, ackKey)
	return err
}

func writeSecret(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// fingerprint prints the value the receiver operator reads to the Hikyo
// instance admin over an independent channel.
func fingerprint(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fingerprint", flag.ContinueOnError)
	origin := fs.String("origin", "", "exact https origin Hikyo will dial")
	recipient := fs.String("recipient", "", "age recipient (age1...)")
	ack := fs.String("ack-key", "", "acknowledgement public key (ed25519:...)")
	generation := fs.Int64("generation", 1, "trust-boundary generation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fp, err := sealedhook.Fingerprint(*origin, *recipient, *ack, *generation)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, fp)
	return err
}

type receiverFile struct {
	TargetID     string `json:"target_id"`
	InstanceID   string `json:"instance_id"`
	Generation   int64  `json:"generation"`
	IdentityFile string `json:"identity_file"`
	AckKeyFile   string `json:"ack_key_file"`
	Senders      []struct {
		Key      string `json:"key"`
		NotAfter string `json:"not_after,omitempty"`
	} `json:"senders"`
	Bindings map[string]string `json:"bindings"`
}

func loadReceiver(path string) (sealedreceiver.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return sealedreceiver.Config{}, err
	}
	var file receiverFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return sealedreceiver.Config{}, fmt.Errorf("config: %w", err)
	}
	base := filepath.Dir(path)
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	identity, err := os.ReadFile(resolve(file.IdentityFile))
	if err != nil {
		return sealedreceiver.Config{}, fmt.Errorf("identity: %w", err)
	}
	ackPEM, err := os.ReadFile(resolve(file.AckKeyFile))
	if err != nil {
		return sealedreceiver.Config{}, fmt.Errorf("ack key: %w", err)
	}
	ack, err := sealedhook.ParseSigningKeyPEM(ackPEM)
	if err != nil {
		return sealedreceiver.Config{}, err
	}
	cfg := sealedreceiver.Config{
		Identity: strings.TrimSpace(string(identity)), AckSigner: ack, TargetID: file.TargetID,
		InstanceID: file.InstanceID, Generation: file.Generation, Bindings: file.Bindings,
	}
	for _, s := range file.Senders {
		key, err := sealedhook.ParsePublicKey(s.Key)
		if err != nil {
			return sealedreceiver.Config{}, fmt.Errorf("sender: %w", err)
		}
		pinned := sealedhook.PinnedKey{Key: key}
		if s.NotAfter != "" {
			if pinned.NotAfter, err = time.Parse(time.RFC3339, s.NotAfter); err != nil {
				return sealedreceiver.Config{}, fmt.Errorf("sender not_after: %w", err)
			}
		}
		cfg.Senders = append(cfg.Senders, pinned)
	}
	return cfg, nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "receiver.json", "receiver configuration")
	listen := fs.String("listen", ":8443", "listen address")
	cert := fs.String("tls-cert", "", "TLS certificate (PEM)")
	key := fs.String("tls-key", "", "TLS private key (PEM)")
	statePath := fs.String("state", "state.json", "applied-values state file (plaintext; mode 0600)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cert == "" || *key == "" {
		return errors.New("serve requires -tls-cert and -tls-key: Hikyo dials https only")
	}
	cfg, err := loadReceiver(*configPath)
	if err != nil {
		return err
	}
	receiver, err := sealedreceiver.New(cfg)
	if err != nil {
		return err
	}
	if raw, err := os.ReadFile(*statePath); err == nil {
		var state sealedreceiver.State
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("state: %w", err)
		}
		receiver.Restore(state)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	receiver.Persist = func(state sealedreceiver.State) error {
		raw, err := sealedreceiver.MarshalState(state)
		if err != nil {
			return err
		}
		tmp := *statePath + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, *statePath)
	}
	mux := http.NewServeMux()
	mux.Handle(sealedhook.SyncPath, receiver)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second}
	return server.ListenAndServeTLS(*cert, *key)
}
