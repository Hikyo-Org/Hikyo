package main

import (
	"path/filepath"
	"testing"
)

func TestReleaseAssetStaysInsideItsDirectory(t *testing.T) {
	if path, ok := releaseAsset("/releases/v1", "hikyo.tar.gz"); !ok || path != filepath.Join("/releases/v1", "hikyo.tar.gz") {
		t.Fatalf("plain asset refused: %q %t", path, ok)
	}
	for _, name := range []string{"", ".", "..", "../secret", "a/b", "/etc/passwd"} {
		if _, ok := releaseAsset("/releases/v1", name); ok {
			t.Fatalf("asset %q escaped its release directory", name)
		}
	}
	if _, ok := releaseAsset("", "hikyo.tar.gz"); ok {
		t.Fatal("unknown release served an asset")
	}
}
