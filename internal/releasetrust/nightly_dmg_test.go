package releasetrust_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestNightlyDiskImagesBindCanonicalPairAndCompletePayloads(t *testing.T) {
	for _, mutation := range []string{"none", "partial pair", "wrong version", "wrong platform", "missing payload", "tampered payload"} {
		t.Run(mutation, func(t *testing.T) {
			compatibility := []byte("signed compatibility fixture")
			payloads := map[string][]byte{"upgrade-compatibility.json": compatibility, "binary-provenance.json": []byte("{}"), "checksums.txt": []byte("checksums"), "native.archive": []byte("native binary")}
			artifacts := []releasetrust.Artifact{{Name: "upgrade-compatibility.json", Kind: "upgrade-compatibility"}, {Name: "binary-provenance.json", Kind: "binary-provenance"}, {Name: "checksums.txt", Kind: "checksum"}, {Name: "native.archive", Kind: "binary", Platform: "darwin/arm64"}}
			for _, arch := range []string{"arm64", "x86_64"} {
				if mutation == "partial pair" && arch == "x86_64" {
					continue
				}
				name := "hikyo_1.1.0-nightly.1_Darwin_" + arch + ".dmg"
				platform := "darwin/" + arch
				if arch == "x86_64" {
					platform = "darwin/amd64"
				}
				if mutation == "wrong version" && arch == "arm64" {
					name = strings.Replace(name, "1.1.0", "1.2.0", 1)
				}
				if mutation == "wrong platform" && arch == "arm64" {
					platform = "linux/arm64"
				}
				payloads[name] = []byte("signed disk image " + arch)
				artifacts = append(artifacts, releasetrust.Artifact{Name: name, Kind: "macos-dmg", Platform: platform})
			}
			fixture, material, _ := testfixture.NightlyWithPayloads(t, compatibility, false, payloads, artifacts)
			snapshot := fixture.Snapshot(t)
			switch mutation {
			case "missing payload":
				delete(material.Artifacts, "hikyo_1.1.0-nightly.1_Darwin_arm64.dmg")
			case "tampered payload":
				material.Artifacts["hikyo_1.1.0-nightly.1_Darwin_arm64.dmg"] = strings.NewReader("tampered")
			}
			release, err := releasetrust.VerifyNightly(snapshot, material)
			if mutation != "none" {
				if err == nil {
					t.Fatal("invalid nightly disk image evidence accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// The binary self-updater consumes the signed tarball, while complete
			// offline verification authenticates the two additional DMG payloads.
			selected, err := release.NightlyArtifacts("darwin/arm64")
			if err != nil {
				t.Fatal(err)
			}
			material.Platform = "darwin/arm64"
			clear(material.Artifacts)
			for _, artifact := range selected {
				if artifact.Kind == "macos-dmg" {
					t.Fatal("self-updater selected a disk image instead of its canonical archive")
				}
				material.Artifacts[artifact.Name] = bytes.NewReader(payloads[artifact.Name])
			}
			if _, err := releasetrust.VerifyNightly(snapshot, material); err != nil {
				t.Fatal(err)
			}
		})
	}
}
