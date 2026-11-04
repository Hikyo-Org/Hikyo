// Command hikyo-receiver-example is the reference receiver for Hikyo's sealed
// webhook adapter (docs/spec/sealed-webhook.md). It is documentation and a
// test harness, not a release artifact: it applies values to a local JSON
// state file so the protocol can be exercised end to end.
//
//	hikyo-receiver-example keygen [-protocol 1|2] -dir DIR
//	hikyo-receiver-example sender-keygen -dir DIR
//	hikyo-receiver-example pubkey -pem FILE
//	hikyo-receiver-example fingerprint [-protocol 1|2] -origin URL -recipient AGE -ack-key KEY [-ack-key-mldsa65 KEY] -generation N
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
		return errors.New("usage: hikyo-receiver-example keygen|sender-keygen|pubkey|fingerprint|serve [flags]")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:], stdout)
	case "sender-keygen":
		return senderKeygen(args[1:], stdout)
	case "pubkey":
		return pubkey(args[1:], stdout)
	case "fingerprint":
		return fingerprint(args[1:], stdout)
	case "serve":
		return serve(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// keygen writes the receiver's age identity and acknowledgement key(s) with
// mode 0600 and prints only the public halves an instance admin pins. With
// -protocol 2 the identity is an age hybrid identity and a second,
// ML-DSA-65 acknowledgement key is written; no private key is ever printed.
func keygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory for the identity and acknowledgement key files")
	protocol := fs.Int("protocol", 1, "protocol version: 1 (X25519, Ed25519) or 2 (hybrid, Ed25519 + ML-DSA-65)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *protocol != 1 && *protocol != 2 {
		return errors.New("keygen: protocol must be 1 or 2")
	}
	ackPEM, ackKey, err := sealedhook.GenerateSigningKey()
	if err != nil {
		return err
	}
	if *protocol == 1 {
		identity, recipient, err := sealedhook.GenerateRecipient()
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
	identity, recipient, err := sealedhook.GenerateHybridRecipient()
	if err != nil {
		return err
	}
	pqPEM, pqKey, err := sealedhook.GenerateMLDSASigningKey()
	if err != nil {
		return err
	}
	for name, raw := range map[string][]byte{"identity-hybrid.age": []byte(identity + "\n"), "ack.pem": ackPEM, "ack-mldsa65.pem": pqPEM} {
		if err := writeSecret(filepath.Join(*dir, name), raw); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "recipient_key: %s\nack_key: %s\nack_key_mldsa65: %s\n", recipient, ackKey, pqKey)
	return err
}

// senderKeygen writes the Hikyo instance's ML-DSA-65 signing key with mode
// 0600 and prints its public half and key id. The instance keeps its existing
// Ed25519 signing key (the one its protocol 1 receivers already pin); only a
// new installation passes -with-ed25519 to mint that one too.
func senderKeygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sender-keygen", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory for signing-mldsa65.pem (and signing-ed25519.pem with -with-ed25519)")
	withEd := fs.Bool("with-ed25519", false, "also generate a new Ed25519 signing key (new installations only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pqPEM, _, err := sealedhook.GenerateMLDSASigningKey()
	if err != nil {
		return err
	}
	if err := writeSecret(filepath.Join(*dir, "signing-mldsa65.pem"), pqPEM); err != nil {
		return err
	}
	if *withEd {
		edPEM, _, err := sealedhook.GenerateSigningKey()
		if err != nil {
			return err
		}
		if err := writeSecret(filepath.Join(*dir, "signing-ed25519.pem"), edPEM); err != nil {
			return err
		}
		if err := pubkey([]string{"-pem", filepath.Join(*dir, "signing-ed25519.pem")}, stdout); err != nil {
			return err
		}
	}
	return pubkey([]string{"-pem", filepath.Join(*dir, "signing-mldsa65.pem")}, stdout)
}

// pubkey prints the public key text and the version 2 key id of a signing key
// PEM (Ed25519 or ML-DSA-65). The private key is parsed, never printed.
func pubkey(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pubkey", flag.ContinueOnError)
	path := fs.String("pem", "", "signing key PEM file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	if ed, err := sealedhook.ParseSigningKeyPEM(raw); err == nil {
		_, err = fmt.Fprintf(stdout, "public_key: %s\nkey_id_v1: %s\nkey_id_v2: %s\n", ed.Public(), ed.Public().ID(), ed.Public().IDV2())
		return err
	}
	pq, err := sealedhook.ParseMLDSASigningKeyPEM(raw)
	if err != nil {
		return errors.New("pubkey: not an Ed25519 or ML-DSA-65 PKCS#8 signing key")
	}
	_, err = fmt.Fprintf(stdout, "public_key: %s\nkey_id_v2: %s\n", pq.Public(), pq.Public().ID())
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
	protocol := fs.Int("protocol", 1, "protocol version: 1 or 2")
	origin := fs.String("origin", "", "exact https origin Hikyo will dial")
	recipient := fs.String("recipient", "", "age recipient (age1... for protocol 1, age1pq1... for protocol 2)")
	ack := fs.String("ack-key", "", "acknowledgement public key (ed25519:...)")
	ackPQ := fs.String("ack-key-mldsa65", "", "protocol 2 only: ML-DSA-65 acknowledgement public key (mldsa65:...)")
	generation := fs.Int64("generation", 1, "trust-boundary generation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var fp string
	var err error
	switch *protocol {
	case 1:
		if *ackPQ != "" {
			return errors.New("fingerprint: -ack-key-mldsa65 needs -protocol 2")
		}
		fp, err = sealedhook.Fingerprint(*origin, *recipient, *ack, *generation)
	case 2:
		var ed sealedhook.PublicKey
		var pq sealedhook.MLDSAPublicKey
		if ed, err = sealedhook.ParsePublicKey(*ack); err == nil {
			if pq, err = sealedhook.ParseMLDSAPublicKey(*ackPQ); err == nil {
				fp, err = sealedhook.FingerprintV2(*origin, *recipient, ed, pq, *generation)
			}
		}
	default:
		return errors.New("fingerprint: protocol must be 1 or 2")
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, fp)
	return err
}

type receiverFile struct {
	TargetID   string `json:"target_id"`
	InstanceID string `json:"instance_id"`
	Generation int64  `json:"generation"`
	// Protocol 1 (omit every v2 field to stay version 1 only; omit
	// identity_file and the v1 sender keys for a PQ-required receiver).
	IdentityFile string `json:"identity_file"`
	AckKeyFile   string `json:"ack_key_file"`
	// Protocol 2: a hybrid identity, the ML-DSA-65 acknowledgement key (the
	// Ed25519 ack_key_file is the other half), and sender pairs.
	HybridIdentityFile string `json:"hybrid_identity_file"`
	AckKeyMLDSA65File  string `json:"ack_key_mldsa65_file"`
	Senders            []struct {
		Key      string `json:"key"`
		NotAfter string `json:"not_after,omitempty"`
	} `json:"senders"`
	// SendersV2 pins version 2 sender pairs; both keys are required.
	SendersV2 []struct {
		Key        string `json:"key"`
		KeyMLDSA65 string `json:"key_mldsa65"`
		NotAfter   string `json:"not_after,omitempty"`
	} `json:"senders_v2"`
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
	ackPEM, err := os.ReadFile(resolve(file.AckKeyFile))
	if err != nil {
		return sealedreceiver.Config{}, fmt.Errorf("ack key: %w", err)
	}
	ack, err := sealedhook.ParseSigningKeyPEM(ackPEM)
	if err != nil {
		return sealedreceiver.Config{}, err
	}
	cfg := sealedreceiver.Config{
		TargetID: file.TargetID, InstanceID: file.InstanceID, Generation: file.Generation, Bindings: file.Bindings,
	}
	if file.IdentityFile != "" {
		identity, err := os.ReadFile(resolve(file.IdentityFile))
		if err != nil {
			return sealedreceiver.Config{}, fmt.Errorf("identity: %w", err)
		}
		cfg.Identity, cfg.AckSigner = strings.TrimSpace(string(identity)), ack
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
	if file.HybridIdentityFile != "" {
		identity, err := os.ReadFile(resolve(file.HybridIdentityFile))
		if err != nil {
			return sealedreceiver.Config{}, fmt.Errorf("hybrid identity: %w", err)
		}
		pqPEM, err := os.ReadFile(resolve(file.AckKeyMLDSA65File))
		if err != nil {
			return sealedreceiver.Config{}, fmt.Errorf("ML-DSA-65 ack key: %w", err)
		}
		pq, err := sealedhook.ParseMLDSASigningKeyPEM(pqPEM)
		if err != nil {
			return sealedreceiver.Config{}, err
		}
		if cfg.AckSignerV2, err = sealedhook.NewSignerV2(ack, pq); err != nil {
			return sealedreceiver.Config{}, err
		}
		cfg.HybridIdentity = strings.TrimSpace(string(identity))
	}
	for _, s := range file.SendersV2 {
		ed, err1 := sealedhook.ParsePublicKey(s.Key)
		pq, err2 := sealedhook.ParseMLDSAPublicKey(s.KeyMLDSA65)
		if err := errors.Join(err1, err2); err != nil {
			return sealedreceiver.Config{}, fmt.Errorf("sender pair: %w", err)
		}
		pinned := sealedhook.PinnedSenderV2{Ed: ed, PQ: pq}
		if s.NotAfter != "" {
			if pinned.NotAfter, err = time.Parse(time.RFC3339, s.NotAfter); err != nil {
				return sealedreceiver.Config{}, fmt.Errorf("sender pair not_after: %w", err)
			}
		}
		cfg.SendersV2 = append(cfg.SendersV2, pinned)
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
		return persistState(*statePath, raw)
	}
	mux := http.NewServeMux()
	mux.Handle(sealedhook.SyncPath, receiver)
	mux.Handle(sealedhook.SyncPathV2, receiver)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second}
	return server.ListenAndServeTLS(*cert, *key)
}

// syncFile and syncDir are variables so a test can prove they run, and in
// which order. They are what make an acknowledged apply survive power loss.
var (
	syncFile = func(f *os.File) error { return f.Sync() }
	syncDir  = func(dir string) error {
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
)

// persistState writes the state so that it survives power loss before the
// receiver acknowledges: the temporary file is synced, renamed over the state
// file, and then the parent directory is synced so the rename itself is
// durable. Spec section 7 requires a durable apply before the acknowledgement.
// shortcut: whole-file rewrite on every apply and Unix directory sync, fine
// for a reference receiver, not for production.
func persistState(path string, raw []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := syncFile(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
