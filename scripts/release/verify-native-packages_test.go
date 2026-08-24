package main

import (
	"archive/tar"
	"bytes"
	"testing"
)

func TestInspectTarRejectsInstallHook(t *testing.T) {
	t.Parallel()

	archive := makeTar(t, map[string][]byte{
		".PKGINFO":      []byte("pkgname = hikyo\n"),
		".post-install": []byte("#!/bin/sh\nstart-service\n"),
	})
	inspection := newTarInspection()
	err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, map[string]bool{".PKGINFO": true})
	if err == nil {
		t.Fatal("inspectTar accepted an installation hook")
	}
}

func TestInspectTarRequiresExactPayload(t *testing.T) {
	t.Parallel()

	archive := makeTar(t, map[string][]byte{
		binaryPath:       []byte("binary"),
		licensePath:      []byte("license"),
		"etc/hikyo.conf": []byte("unsafe"),
	})
	inspection := newTarInspection()
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, nil); err == nil {
		t.Fatal("inspectTar accepted a payload outside the exact allowlist")
	}
}

func TestInspectTarRejectsNonExecutableBinary(t *testing.T) {
	t.Parallel()

	archive := makeTar(t, map[string][]byte{
		binaryPath:  []byte("binary"),
		licensePath: []byte("license"),
	})
	inspection := newTarInspection()
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, nil); err == nil {
		t.Fatal("inspectTar accepted a non-executable packaged binary")
	}
}

func TestInspectDebControlRejectsMaintainerScript(t *testing.T) {
	t.Parallel()

	archive := makeTar(t, map[string][]byte{
		"control":  []byte("Package: hikyo\n"),
		"postinst": []byte("#!/bin/sh\nstart-service\n"),
	})
	if _, err := inspectDebControl(tar.NewReader(bytes.NewReader(archive))); err == nil {
		t.Fatal("inspectDebControl accepted a maintainer script")
	}
}

func TestForbiddenRPMScriptTagsRejectsEveryExecutableClass(t *testing.T) {
	t.Parallel()

	for _, tag := range []int{
		1023, 1024, 1025, 1026, 1065, 1079,
		1085, 1086, 1087, 1088, 1091, 1092,
		1151, 1152, 1153, 1154, 1171,
		5066, 5067, 5076, 5077,
		5103, 5104, 5105, 5106,
		5109,
	} {
		got, found := firstForbiddenRPMHook(func(candidate int) bool { return candidate == tag })
		if !found || got != tag {
			t.Fatalf("RPM script tag %d is not rejected", tag)
		}
	}
	if tag, found := firstForbiddenRPMHook(func(int) bool { return false }); found {
		t.Fatalf("empty RPM hook metadata reported forbidden tag %d", tag)
	}
}

func makeTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
