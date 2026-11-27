package config

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const encryptedWebhookSetting = "HIKYO_ENCRYPTED_WEBHOOK_FILE"

// EncryptedWebhookConfig is the operator-owned encrypted webhook registry (#163).
// Endpoints are instance-admin configuration: the file lives on the server,
// and every endpoint must carry the fingerprint the admin verified out of
// band. No tenant request can add, move, or re-key one.
type EncryptedWebhookConfig struct {
	InstanceID string
	// SigningKeyPEM is the instance's Ed25519 envelope signing key, read from
	// the file named by signing_key_file. It is never logged or exported.
	SigningKeyPEM []byte
	// SigningKeyMLDSA65PEM is the instance's ML-DSA-65 envelope signing key
	// (seed-only PKCS#8), required exactly when a target speaks protocol 2.
	SigningKeyMLDSA65PEM []byte
	Targets              []EncryptedWebhookTarget
}

// EncryptedWebhookTarget is one receiver as written by the instance admin.
type EncryptedWebhookTarget struct {
	ID                   string
	Origin               string
	RecipientKey         string
	AckKey               string
	Generation           int64
	Timeout              time.Duration
	MaxResponseBytes     int64
	ConfirmedFingerprint string
	// Protocol is the wire protocol version, 1 (default) or 2. A protocol 2
	// target never falls back to protocol 1 after an error.
	Protocol      int
	AckKeyMLDSA65 string
}

type encryptedWebhookFile struct {
	InstanceID            string `json:"instance_id"`
	SigningKeyFile        string `json:"signing_key_file"`
	SigningKeyFileMLDSA65 string `json:"signing_key_file_mldsa65"`
	Targets               []struct {
		ID                   string `json:"id"`
		Origin               string `json:"origin"`
		RecipientKey         string `json:"recipient_key"`
		AckKey               string `json:"ack_key"`
		Generation           int64  `json:"generation"`
		TimeoutSeconds       int    `json:"timeout_seconds"`
		MaxResponseBytes     int64  `json:"max_response_bytes"`
		ConfirmedFingerprint string `json:"confirmed_fingerprint"`
		Protocol             *int   `json:"protocol"`
		AckKeyMLDSA65        string `json:"ack_key_mldsa65"`
	} `json:"targets"`
}

func loadEncryptedWebhookConfig(path string) (*EncryptedWebhookConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", encryptedWebhookSetting, err)
	}
	return parseEncryptedWebhookConfig(raw, filepath.Dir(path), readSigningKeyFile)
}

func parseEncryptedWebhookConfig(raw []byte, baseDir string, readKey func(string) ([]byte, error)) (*EncryptedWebhookConfig, error) {
	var file encryptedWebhookFile
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	if err := json.UnmarshalDecode(dec, &file, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", encryptedWebhookSetting, err)
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: trailing data after the JSON object", encryptedWebhookSetting)
	}
	if file.InstanceID == "" || file.SigningKeyFile == "" || len(file.Targets) == 0 {
		return nil, fmt.Errorf("%s: instance_id, signing_key_file, and at least one target are required", encryptedWebhookSetting)
	}
	keyPath := file.SigningKeyFile
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(baseDir, keyPath)
	}
	key, err := readKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("%s: signing key: %w", encryptedWebhookSetting, err)
	}
	out := &EncryptedWebhookConfig{InstanceID: file.InstanceID, SigningKeyPEM: key}
	ids, origins := map[string]bool{}, map[string]bool{}
	protocol2 := false
	for _, t := range file.Targets {
		protocol := 1
		if t.Protocol != nil {
			protocol = *t.Protocol
		}
		switch {
		case protocol != 1 && protocol != 2:
			return nil, fmt.Errorf("%s: target %q protocol must be 1 or 2", encryptedWebhookSetting, t.ID)
		case protocol == 2 && t.AckKeyMLDSA65 == "":
			return nil, fmt.Errorf("%s: protocol 2 target %q needs ack_key_mldsa65", encryptedWebhookSetting, t.ID)
		case protocol == 1 && t.AckKeyMLDSA65 != "":
			return nil, fmt.Errorf("%s: target %q sets ack_key_mldsa65 but speaks protocol 1; set protocol to 2 or remove it", encryptedWebhookSetting, t.ID)
		case t.ID == "" || t.Origin == "" || t.RecipientKey == "" || t.AckKey == "" || t.Generation < 1:
			return nil, fmt.Errorf("%s: target %q needs id, origin, recipient_key, ack_key, and generation >= 1", encryptedWebhookSetting, t.ID)
		case t.ConfirmedFingerprint == "":
			return nil, fmt.Errorf("%s: target %q has no confirmed_fingerprint; verify the receiver fingerprint out of band before activation", encryptedWebhookSetting, t.ID)
		case t.TimeoutSeconds < 1 || t.TimeoutSeconds > 10:
			return nil, fmt.Errorf("%s: target %q timeout_seconds must be 1..10", encryptedWebhookSetting, t.ID)
		case t.MaxResponseBytes < 1 || t.MaxResponseBytes > 64<<10:
			return nil, fmt.Errorf("%s: target %q max_response_bytes must be 1..65536", encryptedWebhookSetting, t.ID)
		case ids[t.ID]:
			return nil, fmt.Errorf("%s: duplicate target id %q", encryptedWebhookSetting, t.ID)
		case origins[t.Origin]:
			return nil, fmt.Errorf("%s: origin %q is configured twice", encryptedWebhookSetting, t.Origin)
		}
		ids[t.ID], origins[t.Origin] = true, true
		protocol2 = protocol2 || protocol == 2
		out.Targets = append(out.Targets, EncryptedWebhookTarget{
			ID: t.ID, Origin: t.Origin, RecipientKey: t.RecipientKey, AckKey: t.AckKey, Generation: t.Generation,
			Timeout: time.Duration(t.TimeoutSeconds) * time.Second, MaxResponseBytes: t.MaxResponseBytes,
			ConfirmedFingerprint: t.ConfirmedFingerprint, Protocol: protocol, AckKeyMLDSA65: t.AckKeyMLDSA65,
		})
	}
	switch {
	case protocol2 && file.SigningKeyFileMLDSA65 == "":
		return nil, fmt.Errorf("%s: a protocol 2 target needs signing_key_file_mldsa65", encryptedWebhookSetting)
	case !protocol2 && file.SigningKeyFileMLDSA65 != "":
		// Unused private key material is surprise custody: remove the line
		// when the last protocol 2 target is rolled back.
		return nil, fmt.Errorf("%s: signing_key_file_mldsa65 is set but no target speaks protocol 2", encryptedWebhookSetting)
	case protocol2:
		pqPath := file.SigningKeyFileMLDSA65
		if !filepath.IsAbs(pqPath) {
			pqPath = filepath.Join(baseDir, pqPath)
		}
		if out.SigningKeyMLDSA65PEM, err = readKey(pqPath); err != nil {
			return nil, fmt.Errorf("%s: ML-DSA signing key: %w", encryptedWebhookSetting, err)
		}
	}
	return out, nil
}

// readSigningKeyFile refuses a key readable by group or other on Unix: the
// signing key is what receivers trust, so its custody matches the root key's.
func readSigningKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("mode %v is accessible to group or other; chmod 600", info.Mode().Perm())
	}
	return os.ReadFile(path)
}

// getenvDeprecatedAlias reads a renamed setting. The old name is accepted until
// v2 with a warning; setting both to different values fails fast.
func getenvDeprecatedAlias(getenv func(string) string, key, oldKey string, warnings *[]string) (string, error) {
	value, old := getenv(key), getenv(oldKey)
	if old == "" {
		return value, nil
	}
	if value != "" && value != old {
		return "", fmt.Errorf("%s and the deprecated %s are both set to different values; set only %s", key, oldKey, key)
	}
	*warnings = append(*warnings, fmt.Sprintf("%s is deprecated and will be removed in v2; use %s", oldKey, key))
	return old, nil
}
