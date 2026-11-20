//go:build darwin && probe

package main

import "testing"

func TestProbeBuildHonoursDevelopmentVariables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HIKYO_DESKTOP_HOME", home)
	t.Setenv("HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP", "1")
	if dir, err := dataDir(); err != nil || dir != home || !devAllowLoopbackHTTP() {
		t.Fatalf("dataDir = %q, %v", dir, err)
	}
}
