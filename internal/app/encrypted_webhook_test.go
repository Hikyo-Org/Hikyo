package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/encryptedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/encryptedhook"
)

func encryptedWebhookRegistry(t *testing.T) *config.EncryptedWebhookConfig {
	t.Helper()
	signing, _, err := encryptedhook.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, _ := encryptedhook.GenerateRecipient()
	_, ack, _ := encryptedhook.GenerateSigningKey()
	fp, err := encryptedhook.Fingerprint("https://recv.example", recipient, ack, 1)
	if err != nil {
		t.Fatal(err)
	}
	return &config.EncryptedWebhookConfig{InstanceID: "hikyo-test", SigningKeyPEM: signing, Targets: []config.EncryptedWebhookTarget{{
		ID: "payments", Origin: "https://recv.example", RecipientKey: recipient, AckKey: ack, Generation: 1,
		Timeout: 5 * time.Second, MaxResponseBytes: 4096, ConfirmedFingerprint: fp,
	}}}
}

func TestEncryptedWebhookFactoryBuildsOnlyInstanceAdminOrigins(t *testing.T) {
	endpoints, err := activateEncryptedWebhookEndpoints(encryptedWebhookRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	factory := newAdapterModuleFactory(nil, endpoints, adapterProviderPolicy{})
	lease, err := factory.Build(adapter.EncryptedWebhookProvider, adapter.Config{Origin: "https://recv.example"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, ok := lease.Module.(*encryptedwebhook.Module); !ok {
		t.Fatalf("module = %T", lease.Module)
	}
	if _, err := factory.Build(adapter.EncryptedWebhookProvider, adapter.Config{Origin: "https://tenant-chosen.example"}, "binding"); err == nil || !strings.Contains(err.Error(), "instance-admin") {
		t.Fatalf("tenant-chosen origin accepted: %v", err)
	}
	if _, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.EncryptedWebhookProvider, adapter.Config{Origin: "https://recv.example"}, "binding"); err == nil {
		t.Fatal("no registry must mean no encrypted-webhook endpoint")
	}
}

func TestEncryptedWebhookActivationRefusesUnconfirmedTrustBoundary(t *testing.T) {
	registry := encryptedWebhookRegistry(t)
	_, other, _ := encryptedhook.GenerateRecipient()
	registry.Targets[0].RecipientKey = other // key swapped without re-confirmation
	if _, err := activateEncryptedWebhookEndpoints(registry); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("activation = %v, want fingerprint refusal", err)
	}
	registry = encryptedWebhookRegistry(t)
	registry.SigningKeyPEM = []byte("not a key")
	if _, err := activateEncryptedWebhookEndpoints(registry); err == nil {
		t.Fatal("invalid signing key accepted")
	}
	if endpoints, err := activateEncryptedWebhookEndpoints(nil); err != nil || endpoints != nil {
		t.Fatalf("absent registry = %v, %v", endpoints, err)
	}
}

func encryptedWebhookRegistryV2(t *testing.T) *config.EncryptedWebhookConfig {
	t.Helper()
	registry := encryptedWebhookRegistry(t)
	pqPEM, _, err := encryptedhook.GenerateMLDSASigningKey()
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, _ := encryptedhook.GenerateHybridRecipient()
	_, ackEdText, _ := encryptedhook.GenerateSigningKey()
	_, ackPQText, _ := encryptedhook.GenerateMLDSASigningKey()
	ackEd, _ := encryptedhook.ParsePublicKey(ackEdText)
	ackPQ, _ := encryptedhook.ParseMLDSAPublicKey(ackPQText)
	fp, err := encryptedhook.FingerprintV2("https://recv2.example", recipient, ackEd, ackPQ, 1)
	if err != nil {
		t.Fatal(err)
	}
	registry.SigningKeyMLDSA65PEM = pqPEM
	registry.Targets = append(registry.Targets, config.EncryptedWebhookTarget{
		ID: "payments-pq", Origin: "https://recv2.example", RecipientKey: recipient, AckKey: ackEdText, AckKeyMLDSA65: ackPQText,
		Protocol: 2, Generation: 1, Timeout: 5 * time.Second, MaxResponseBytes: 8192, ConfirmedFingerprint: fp,
	})
	return registry
}

func TestEncryptedWebhookMixedRegistryActivatesEachTargetUnderItsOwnProtocol(t *testing.T) {
	endpoints, err := activateEncryptedWebhookEndpoints(encryptedWebhookRegistryV2(t))
	if err != nil {
		t.Fatal(err)
	}
	if endpoints["https://recv.example"].Protocol() != 1 || endpoints["https://recv2.example"].Protocol() != 2 {
		t.Fatalf("protocols = %d %d", endpoints["https://recv.example"].Protocol(), endpoints["https://recv2.example"].Protocol())
	}
	factory := newAdapterModuleFactory(nil, endpoints, adapterProviderPolicy{})
	lease, err := factory.Build(adapter.EncryptedWebhookProvider, adapter.Config{Origin: "https://recv2.example"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if m, ok := lease.Module.(*encryptedwebhook.Module); !ok || m.Endpoint.Protocol() != 2 {
		t.Fatalf("module = %T", lease.Module)
	}
}

func TestEncryptedWebhookProtocolTwoActivationRefusesBeforeServing(t *testing.T) {
	for name, edit := range map[string]func(*config.EncryptedWebhookConfig){
		"response bound under one real acknowledgement": func(c *config.EncryptedWebhookConfig) { c.Targets[1].MaxResponseBytes = 4096 },
		"fingerprint not reconfirmed after a recipient swap": func(c *config.EncryptedWebhookConfig) {
			_, c.Targets[1].RecipientKey, _ = encryptedhook.GenerateHybridRecipient()
		},
		"v1 fingerprint pasted onto a v2 target": func(c *config.EncryptedWebhookConfig) {
			c.Targets[1].ConfirmedFingerprint = c.Targets[0].ConfirmedFingerprint
		},
		"classical recipient on a v2 target":   func(c *config.EncryptedWebhookConfig) { c.Targets[1].RecipientKey = c.Targets[0].RecipientKey },
		"hybrid recipient on a v1 target":      func(c *config.EncryptedWebhookConfig) { c.Targets[0].RecipientKey = c.Targets[1].RecipientKey },
		"ml-dsa signing key missing":           func(c *config.EncryptedWebhookConfig) { c.SigningKeyMLDSA65PEM = nil },
		"ed25519 key where ml-dsa is expected": func(c *config.EncryptedWebhookConfig) { c.SigningKeyMLDSA65PEM = c.SigningKeyPEM },
		"garbage ml-dsa key":                   func(c *config.EncryptedWebhookConfig) { c.SigningKeyMLDSA65PEM = []byte("nope") },
	} {
		t.Run(name, func(t *testing.T) {
			registry := encryptedWebhookRegistryV2(t)
			edit(registry)
			if endpoints, err := activateEncryptedWebhookEndpoints(registry); err == nil {
				t.Fatalf("activated %d endpoints", len(endpoints))
			}
		})
	}
}
