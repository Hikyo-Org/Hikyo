//go:build !ios

package main

// installSwitcher is the iOS view switcher; the other platforms have a window each, or (Android) a host that switches itself.
func installSwitcher([]string) {}
