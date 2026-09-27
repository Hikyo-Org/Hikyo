package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const sealedWebhookSetting = "HIKYO_SEALED_WEBHOOK_FILE"

// SealedWebhookConfig is the operator-owned sealed webhook registry (#163).
// Endpoints are instance-admin configuration: the file lives on the server,
// and every endpoint must carry the fingerprint the admin verified out of
// band. No tenant request can add, move, or re-key one.
type SealedWebhookConfig struct {
	InstanceID string
	// SigningKeyPEM is the instance's Ed25519 envelope signing key, read from
	// the file named by signing_key_file. It is never logged or exported.
	SigningKeyPEM []byte
	Targets       []SealedWebhookTarget
}

// SealedWebhookTarget is one receiver as written by the instance admin.
type SealedWebhookTarget struct {
	ID                   string
	Origin               string
	RecipientKey         string
	AckKey               string
	Generation           int64
	Timeout              time.Duration
	MaxResponseBytes     int64
	ConfirmedFingerprint string
}

type sealedWebhookFile struct {
	InstanceID     string `json:"instance_id"`
	SigningKeyFile string `json:"signing_key_file"`
	Targets        []struct {
		ID                   string `json:"id"`
		Origin               string `json:"origin"`
		RecipientKey         string `json:"recipient_key"`
		AckKey               string `json:"ack_key"`
		Generation           int64  `json:"generation"`
		TimeoutSeconds       int    `json:"timeout_seconds"`
		MaxResponseBytes     int64  `json:"max_response_bytes"`
		ConfirmedFingerprint string `json:"confirmed_fingerprint"`
	} `json:"targets"`
}

func loadSealedWebhookConfig(path string) (*SealedWebhookConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", sealedWebhookSetting, err)
	}
	return parseSealedWebhookConfig(raw, filepath.Dir(path), readSigningKeyFile)
}

func parseSealedWebhookConfig(raw []byte, baseDir string, readKey func(string) ([]byte, error)) (*SealedWebhookConfig, error) {
	var file sealedWebhookFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", sealedWebhookSetting, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%s: trailing data after the JSON object", sealedWebhookSetting)
	}
	if file.InstanceID == "" || file.SigningKeyFile == "" || len(file.Targets) == 0 {
		return nil, fmt.Errorf("%s: instance_id, signing_key_file, and at least one target are required", sealedWebhookSetting)
	}
	keyPath := file.SigningKeyFile
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(baseDir, keyPath)
	}
	key, err := readKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("%s: signing key: %w", sealedWebhookSetting, err)
	}
	out := &SealedWebhookConfig{InstanceID: file.InstanceID, SigningKeyPEM: key}
	ids, origins := map[string]bool{}, map[string]bool{}
	for _, t := range file.Targets {
		switch {
		case t.ID == "" || t.Origin == "" || t.RecipientKey == "" || t.AckKey == "" || t.Generation < 1:
			return nil, fmt.Errorf("%s: target %q needs id, origin, recipient_key, ack_key, and generation >= 1", sealedWebhookSetting, t.ID)
		case t.ConfirmedFingerprint == "":
			return nil, fmt.Errorf("%s: target %q has no confirmed_fingerprint; verify the receiver fingerprint out of band before activation", sealedWebhookSetting, t.ID)
		case t.TimeoutSeconds < 1 || t.TimeoutSeconds > 10:
			return nil, fmt.Errorf("%s: target %q timeout_seconds must be 1..10", sealedWebhookSetting, t.ID)
		case t.MaxResponseBytes < 1 || t.MaxResponseBytes > 64<<10:
			return nil, fmt.Errorf("%s: target %q max_response_bytes must be 1..65536", sealedWebhookSetting, t.ID)
		case ids[t.ID]:
			return nil, fmt.Errorf("%s: duplicate target id %q", sealedWebhookSetting, t.ID)
		case origins[t.Origin]:
			return nil, fmt.Errorf("%s: origin %q is configured twice", sealedWebhookSetting, t.Origin)
		}
		ids[t.ID], origins[t.Origin] = true, true
		out.Targets = append(out.Targets, SealedWebhookTarget{
			ID: t.ID, Origin: t.Origin, RecipientKey: t.RecipientKey, AckKey: t.AckKey, Generation: t.Generation,
			Timeout: time.Duration(t.TimeoutSeconds) * time.Second, MaxResponseBytes: t.MaxResponseBytes,
			ConfirmedFingerprint: t.ConfirmedFingerprint,
		})
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
