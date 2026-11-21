//go:build darwin && !probe

package main

import "testing"

func TestReleaseBuildIgnoresDevelopmentVariables(t *testing.T) {
	t.Setenv("HIKYO_DESKTOP_HOME", t.TempDir())
	t.Setenv("HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP", "1")
	if devDataDir() != "" || devAllowLoopbackHTTP() {
		t.Fatal("a build without the probe tag honours a development variable")
	}
	if dir, err := dataDir(); err != nil || dir == "" || dir == devDataDir() {
		t.Fatalf("dataDir = %q, %v", dir, err)
	}
}
