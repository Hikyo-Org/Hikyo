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
	factory := newAdapterModuleFactory(nil, endpoints)
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
	if _, err := newAdapterModuleFactory(nil, nil).Build(adapter.SealedWebhookProvider, adapter.Config{Origin: "https://recv.example"}, "binding"); err == nil {
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
