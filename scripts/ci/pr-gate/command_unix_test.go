//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGateSubprocessCancellationReleasesDescendantsAndPipes(t *testing.T) {
	for _, kind := range []string{"gh", "authority", "dco"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "child")
			name := "gh"
			if kind == "authority" {
				name = "sh"
			}
			if kind == "dco" {
				name = "git"
			}
			script := "#!/bin/sh\n"
			if kind == "dco" {
				script += "case \" $* \" in *' init '*) exit 0;; esac\n"
			}
			script += "sleep 20 &\nchild=$!\nprintf '%s' \"$child\" > '" + marker + "'\nwait\n"
			if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "gh":
					var out any
					err = (ghAPI{}).read(ctx, "repos/Hikyo-Org/Hikyo", &out)
				case "authority":
					_, err = verifyPolicy(ctx, 7, strings.Repeat("a", 40))
				case "dco":
					_, err = verifyQueueDCO(ctx, "Hikyo-Org/Hikyo", pull{Head: reference{SHA: strings.Repeat("a", 40)}, Base: reference{SHA: strings.Repeat("b", 40)}})
				}
				done <- err
			}()
			var pid int
			readyUntil := time.Now().Add(2 * time.Second)
			for time.Now().Before(readyUntil) {
				data, err := os.ReadFile(marker)
				if err == nil {
					pid, _ = strconv.Atoi(string(data))
					if pid > 0 {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
			}
			if pid <= 0 {
				t.Fatal("descendant never started; cancellation regression was not exercised")
			}
			defer syscall.Kill(pid, syscall.SIGKILL)
			started := time.Now()
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled subprocess reported success")
				}
			case <-time.After(1500 * time.Millisecond):
				t.Fatal("cancelled subprocess consumed the publication reserve")
			}
			if time.Since(started) > 1200*time.Millisecond {
				t.Fatal("descendant pipe delayed cancellation")
			}
			settleUntil := time.Now().Add(time.Second)
			for {
				state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
				if err != nil || strings.TrimSpace(string(state)) == "" || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
					break
				}
				if !time.Now().Before(settleUntil) {
					t.Fatalf("descendant %d survived group cancellation: %s", pid, state)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}
