package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/sealedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

func sealedWebhookRegistry(t *testing.T) *config.SealedWebhookConfig {
	t.Helper()
	signing, _, err := sealedhook.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, _ := sealedhook.GenerateRecipient()
	_, ack, _ := sealedhook.GenerateSigningKey()
	fp, err := sealedhook.Fingerprint("https://recv.example", recipient, ack, 1)
	if err != nil {
		t.Fatal(err)
	}
	return &config.SealedWebhookConfig{InstanceID: "hikyo-test", SigningKeyPEM: signing, Targets: []config.SealedWebhookTarget{{
		ID: "payments", Origin: "https://recv.example", RecipientKey: recipient, AckKey: ack, Generation: 1,
		Timeout: 5 * time.Second, MaxResponseBytes: 4096, ConfirmedFingerprint: fp,
	}}}
}

func TestSealedWebhookFactoryBuildsOnlyInstanceAdminOrigins(t *testing.T) {
	endpoints, err := activateSealedWebhookEndpoints(sealedWebhookRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	factory := newAdapterModuleFactory(nil, endpoints, adapterProviderPolicy{})
	lease, err := factory.Build(adapter.SealedWebhookProvider, adapter.Config{Origin: "https://recv.example"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, ok := lease.Module.(*sealedwebhook.Module); !ok {
		t.Fatalf("module = %T", lease.Module)
	}
	if _, err := factory.Build(adapter.SealedWebhookProvider, adapter.Config{Origin: "https://tenant-chosen.example"}, "binding"); err == nil || !strings.Contains(err.Error(), "instance-admin") {
		t.Fatalf("tenant-chosen origin accepted: %v", err)
	}
	if _, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.SealedWebhookProvider, adapter.Config{Origin: "https://recv.example"}, "binding"); err == nil {
		t.Fatal("no registry must mean no sealed-webhook endpoint")
	}
}

func TestSealedWebhookActivationRefusesUnconfirmedTrustBoundary(t *testing.T) {
	registry := sealedWebhookRegistry(t)
	_, other, _ := sealedhook.GenerateRecipient()
	registry.Targets[0].RecipientKey = other // key swapped without re-confirmation
	if _, err := activateSealedWebhookEndpoints(registry); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("activation = %v, want fingerprint refusal", err)
	}
	registry = sealedWebhookRegistry(t)
	registry.SigningKeyPEM = []byte("not a key")
	if _, err := activateSealedWebhookEndpoints(registry); err == nil {
		t.Fatal("invalid signing key accepted")
	}
	if endpoints, err := activateSealedWebhookEndpoints(nil); err != nil || endpoints != nil {
		t.Fatalf("absent registry = %v, %v", endpoints, err)
	}
}

func sealedWebhookRegistryV2(t *testing.T) *config.SealedWebhookConfig {
	t.Helper()
	registry := sealedWebhookRegistry(t)
	pqPEM, _, err := sealedhook.GenerateMLDSASigningKey()
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, _ := sealedhook.GenerateHybridRecipient()
	_, ackEdText, _ := sealedhook.GenerateSigningKey()
	_, ackPQText, _ := sealedhook.GenerateMLDSASigningKey()
	ackEd, _ := sealedhook.ParsePublicKey(ackEdText)
	ackPQ, _ := sealedhook.ParseMLDSAPublicKey(ackPQText)
	fp, err := sealedhook.FingerprintV2("https://recv2.example", recipient, ackEd, ackPQ, 2)
	if err != nil {
		t.Fatal(err)
	}
	registry.SigningKeyMLDSA65PEM = pqPEM
	registry.Targets = append(registry.Targets, config.SealedWebhookTarget{
		ID: "payments-pq", Origin: "https://recv2.example", RecipientKey: recipient, AckKey: ackEdText, AckKeyMLDSA65: ackPQText,
		Protocol: 2, Generation: 2, Timeout: 5 * time.Second, MaxResponseBytes: 4096, ConfirmedFingerprint: fp,
	})
	return registry
}

func TestSealedWebhookMixedRegistryActivatesEachTargetUnderItsOwnProtocol(t *testing.T) {
	endpoints, err := activateSealedWebhookEndpoints(sealedWebhookRegistryV2(t))
	if err != nil {
		t.Fatal(err)
	}
	if endpoints["https://recv.example"].Protocol() != 1 || endpoints["https://recv2.example"].Protocol() != 2 {
		t.Fatalf("protocols = %d %d", endpoints["https://recv.example"].Protocol(), endpoints["https://recv2.example"].Protocol())
	}
	factory := newAdapterModuleFactory(nil, endpoints, adapterProviderPolicy{})
	lease, err := factory.Build(adapter.SealedWebhookProvider, adapter.Config{Origin: "https://recv2.example"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if m, ok := lease.Module.(*sealedwebhook.Module); !ok || m.Endpoint.Protocol() != 2 {
		t.Fatalf("module = %T", lease.Module)
	}
}

func TestSealedWebhookProtocolTwoActivationRefusesBeforeServing(t *testing.T) {
	for name, edit := range map[string]func(*config.SealedWebhookConfig){
		"fingerprint not reconfirmed after a recipient swap": func(c *config.SealedWebhookConfig) {
			_, c.Targets[1].RecipientKey, _ = sealedhook.GenerateHybridRecipient()
		},
		"v1 fingerprint pasted onto a v2 target": func(c *config.SealedWebhookConfig) {
			c.Targets[1].ConfirmedFingerprint = c.Targets[0].ConfirmedFingerprint
		},
		"classical recipient on a v2 target":   func(c *config.SealedWebhookConfig) { c.Targets[1].RecipientKey = c.Targets[0].RecipientKey },
		"hybrid recipient on a v1 target":      func(c *config.SealedWebhookConfig) { c.Targets[0].RecipientKey = c.Targets[1].RecipientKey },
		"ml-dsa signing key missing":           func(c *config.SealedWebhookConfig) { c.SigningKeyMLDSA65PEM = nil },
		"ed25519 key where ml-dsa is expected": func(c *config.SealedWebhookConfig) { c.SigningKeyMLDSA65PEM = c.SigningKeyPEM },
		"garbage ml-dsa key":                   func(c *config.SealedWebhookConfig) { c.SigningKeyMLDSA65PEM = []byte("nope") },
	} {
		t.Run(name, func(t *testing.T) {
			registry := sealedWebhookRegistryV2(t)
			edit(registry)
			if endpoints, err := activateSealedWebhookEndpoints(registry); err == nil {
				t.Fatalf("activated %d endpoints", len(endpoints))
			}
		})
	}
}
