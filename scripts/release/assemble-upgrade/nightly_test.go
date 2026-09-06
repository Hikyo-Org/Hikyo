//go:build darwin || linux

package main

import (
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func TestNightlyAssemblyAuthenticatesCompleteDownload(t *testing.T) {
	for _, mutation := range []string{"none", "extra", "tamper", "missing", "wrong identity", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			_, declaration, err := buildcompat.Development()
			if err != nil {
				t.Fatal(err)
			}
			declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, "1.1.0-nightly.1", 2, strings.Repeat("a", 40)
			trust, nightly, _ := testfixture.Nightly(t, testfixture.JSON(t, declaration), mutation == "wrong identity")
			dir := t.TempDir()
			o := options{root: filepath.Join(dir, "root.json"), recovery: filepath.Join(dir, "recovery.pub"), snapshot: filepath.Join(dir, "snapshot"), keys: filepath.Join(dir, "keys"), output: filepath.Join(dir, "bundle"), nightlies: directories{filepath.Join(dir, "nightly")}}
			put(t, o.root, trust.Pinned.Root)
			put(t, o.recovery, trust.Pinned.RecoveryPublicKey)
			put(t, filepath.Join(o.keys, "primary.pub"), trust.PrimaryPublic)
			assemblyFixture{opts: o, trust: trust}.snapshot(t)
			for name, reader := range nightly.Artifacts {
				raw, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(o.nightlies[0], name), raw)
			}
			put(t, filepath.Join(o.nightlies[0], "release-manifest.json"), nightly.Manifest)
			put(t, filepath.Join(o.nightlies[0], "release-manifest.sigstore.json"), nightly.Bundle)
			switch mutation {
			case "extra":
				put(t, filepath.Join(o.nightlies[0], "extra.exe"), []byte("unsigned"))
			case "tamper":
				put(t, filepath.Join(o.nightlies[0], "hikyo_linux_arm64.tar.gz"), []byte("changed"))
			case "missing":
				if err := os.Remove(filepath.Join(o.nightlies[0], "checksums.txt")); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				o.nightlies = append(o.nightlies, o.nightlies[0])
			}
			err = assemble(t.Context(), o)
			if mutation != "none" {
				if err == nil {
					t.Fatal("unsafe nightly published")
				}
				if _, err := os.Stat(o.output); !os.IsNotExist(err) {
					t.Fatal("output exists after refusal")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := upgradebundle.Load(t.Context(), o.output, trust.Pinned, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.MatchBuild(testfixture.JSON(t, declaration)); err != nil {
				t.Fatal(err)
			}
			// Exercise the actual multicall CLI with release-linker stamps, not
			// --dev or a test-only injected admission. All keys remain test-local.
			binary := filepath.Join(dir, "hikyo")
			claim := testfixture.JSON(t, declaration)
			flags := "-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + base64.StdEncoding.EncodeToString(trust.Pinned.Root) +
				" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + base64.StdEncoding.EncodeToString(trust.Pinned.RecoveryPublicKey) +
				" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + base64.StdEncoding.EncodeToString(claim) +
				" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claim))
			build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", flags, "-o", binary, "../../../cmd/hikyo")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build packaged CLI: %v\n%s", err, output)
			}
			smoke := exec.CommandContext(t.Context(), "../smoke-release-startup.sh", binary, o.output)
			if output, err := smoke.CombinedOutput(); err != nil {
				t.Fatalf("signed nightly production startup: %v\n%s", err, output)
			}
		})
	}
}
