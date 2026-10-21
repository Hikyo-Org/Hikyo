package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"gopkg.in/yaml.v3"
)

func verifyReleaseLayout(directory string, release releasetrust.VerifiedRelease, keyless bool) error {
	counts := map[string]int{}
	packages, binaries, verifiers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var image, chartDigest, chart releasetrust.Artifact
	payloads := map[string]releasetrust.Artifact{}
	for _, a := range release.Artifacts() {
		counts[a.Kind]++
		switch a.Kind {
		case "package":
			name, err := nativePackageName(release.Identity().Version, a.Format, a.Arch)
			if err != nil || a.Name != name {
				return errors.New("native package name differs from release identity")
			}
			identity := a.Format + "/" + a.Arch
			if packages[identity] {
				return errors.New("duplicate native package platform")
			}
			packages[identity] = true
		case "binary":
			binaries[a.Name] = true
		case "macos-dmg":
			if a.Name != "hikyo_"+release.Identity().Version+"_Darwin_arm64.dmg" &&
				a.Name != "hikyo_"+release.Identity().Version+"_Darwin_x86_64.dmg" {
				return errors.New("macOS disk image differs from release identity")
			}
		case "release-verifier":
			verifiers[a.Name] = true
		case "image":
			image = a
		case "chart-digest":
			chartDigest = a
		case "chart":
			chart = a
		case "oci-payload":
			if _, exists := payloads[a.SubjectKind]; exists {
				return errors.New("duplicate OCI signing subject")
			}
			payloads[a.SubjectKind] = a
		}
	}
	// Historical releases predate Apple packaging. When present, disk images
	// must cover both architectures; new builds require them before signing.
	if counts["macos-dmg"] != 0 && counts["macos-dmg"] != 2 {
		return errors.New("macOS disk images must cover both architectures")
	}
	required := map[string]int{"package": 8, "binary": 6, "binary-provenance": 1,
		"image": 1, "chart-digest": 1, "chart": 1, "installer": 1, "release-candidate": 1, "upgrade-compatibility": 1, "oci-payload": 2,
		"checksum": 1}
	if keyless {
		for kind, count := range map[string]int{"release-verifier": 6, "build-provenance": 1, "stable-policy": 1, "stable-policy-signature": 1, "sigstore-trusted-root": 1} {
			required[kind] = count
		}
	}
	for kind, count := range required {
		if counts[kind] != count {
			return fmt.Errorf("release requires %d %s artifacts, got %d", count, kind, counts[kind])
		}
	}
	if counts["sbom"] < 1 {
		return errors.New("release SBOM missing")
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext, exe := ".tar.gz", ""
			if goos == "windows" {
				ext, exe = ".zip", ".exe"
			}
			platform := map[string]string{"linux": "Linux", "darwin": "Darwin", "windows": "Windows"}[goos]
			nativeArch := arch
			if arch == "amd64" {
				nativeArch = "x86_64"
			}
			if !binaries["hikyo_"+release.Identity().Version+"_"+platform+"_"+nativeArch+ext] || (keyless && !verifiers["hikyo-release-verifier_"+platform+"_"+nativeArch+exe]) {
				return errors.New("complete six-platform binary/verifier coverage required")
			}
		}
	}
	if chart.ImageRepository != image.Image || chart.ImageDigest != image.Digest {
		return errors.New("chart and image digest identity differ")
	}
	for _, pair := range []struct{ kind, ref, digest string }{{"image", image.Image, image.Digest}, {"chart", chartDigest.Chart, chartDigest.Digest}} {
		a, ok := payloads[pair.kind]
		if !ok || a.Subject != pair.ref+"@"+pair.digest || a.Digest != pair.digest {
			return errors.New("OCI payload differs from manifest subject")
		}
		raw, err := readDocument(directory, a.Name)
		if err != nil {
			return err
		}
		var payload struct {
			Critical struct {
				Identity struct {
					Reference string `json:"docker-reference"`
				} `json:"identity"`
				Image struct {
					Digest string `json:"docker-manifest-digest"`
				} `json:"image"`
				Type string `json:"type"`
			} `json:"critical"`
			Optional map[string]string `json:"optional"`
		}
		if err := definitions.DecodeStrict(raw, &payload); err != nil {
			return err
		}
		if payload.Critical.Identity.Reference != pair.ref || payload.Critical.Image.Digest != pair.digest || payload.Critical.Type != "cosign container image signature" {
			return errors.New("OCI payload content differs from manifest")
		}
	}
	for _, a := range []releasetrust.Artifact{image, chartDigest} {
		raw, err := readDocument(directory, a.Name)
		if err != nil {
			return err
		}
		if strings.TrimSuffix(string(raw), "\n") != a.Digest {
			return errors.New("OCI digest file differs from manifest")
		}
	}
	if err := verifyBinaryProvenance(directory, release.Identity()); err != nil {
		return err
	}
	if err := verifyChecksums(directory, release.Artifacts()); err != nil {
		return err
	}
	return verifyChart(filepath.Join(directory, chart.Name), release.Identity().Version, image)
}

func verifyChart(path, version string, image releasetrust.Artifact) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, 32<<20))
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "hikyo/Chart.yaml" && header.Name != "hikyo/values.yaml" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size > 2<<20 {
			return errors.New("invalid chart metadata member")
		}
		if _, exists := files[header.Name]; exists {
			return errors.New("duplicate chart metadata member")
		}
		raw, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		files[header.Name] = raw
	}
	var chart struct {
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	var values struct {
		Image struct {
			Repository string `yaml:"repository"`
			Digest     string `yaml:"digest"`
		} `yaml:"image"`
	}
	if err := yaml.Unmarshal(files["hikyo/Chart.yaml"], &chart); err != nil {
		return err
	}
	if err := yaml.Unmarshal(files["hikyo/values.yaml"], &values); err != nil {
		return err
	}
	if chart.Version != version || chart.AppVersion != version || values.Image.Repository != image.Image || values.Image.Digest != image.Digest {
		return errors.New("packaged chart version/image pin differs from manifest")
	}
	return nil
}

// Match the package naming rules in scripts/lib/release.sh, including the
// different native architectures and prerelease punctuation used by nfpm.
func nativePackageName(version, format, arch string) (string, error) {
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z][0-9A-Za-z.-]*)?$`).MatchString(version) {
		return "", errors.New("unsupported package version")
	}
	if arch != "amd64" && arch != "arm64" {
		return "", errors.New("unsupported package architecture")
	}
	base, prerelease, _ := strings.Cut(version, "-")
	nativeArch := arch
	if format != "deb" {
		if arch == "amd64" {
			nativeArch = "x86_64"
		} else {
			nativeArch = "aarch64"
		}
	}
	suffix := ""
	if prerelease != "" {
		suffix = "_" + prerelease
	}
	switch format {
	case "deb":
		return "hikyo_" + base + suffix + "_" + nativeArch + ".deb", nil
	case "apk":
		return "hikyo_" + base + suffix + "_" + nativeArch + ".apk", nil
	case "rpm":
		return "hikyo-" + base + strings.ReplaceAll(suffix, "-", "_") + "-1." + nativeArch + ".rpm", nil
	case "archlinux":
		return "hikyo-" + base + strings.ReplaceAll(prerelease, "-", "_") + "-1-" + nativeArch + ".pkg.tar.zst", nil
	default:
		return "", errors.New("unsupported package format")
	}
}

func verifyChecksums(directory string, artifacts []releasetrust.Artifact) error {
	raw, err := readDocument(directory, "checksums.txt")
	if err != nil {
		return err
	}
	expected := map[string]string{}
	for _, a := range artifacts {
		if a.Kind == "binary" {
			expected[a.Name] = a.SHA256
		}
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != len(expected) {
		return errors.New("checksums must cover all release archives exactly once")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return errors.New("invalid archive checksum line")
		}
		want, exists := expected[fields[1]]
		if !exists || fields[0] != want {
			return errors.New("archive checksum differs from signed manifest")
		}
		delete(expected, fields[1])
	}
	return nil
}

func verifyBinaryProvenance(directory string, id releaseidentity.Identity) error {
	raw, err := readDocument(directory, "binary-provenance.json")
	if err != nil {
		return err
	}
	var provenance struct {
		Schema   string `json:"schema"`
		Commit   string `json:"source_commit"`
		Version  string `json:"version"`
		Producer struct {
			Name      string                 `json:"name"`
			BuildID   string                 `json:"build_id"`
			Config    string                 `json:"config"`
			ConfigSHA releaseidentity.Digest `json:"config_sha256"`
		} `json:"producer"`
		Packages []struct {
			Goos    string `json:"goos"`
			Goarch  string `json:"goarch"`
			Archive struct {
				BuildID string                 `json:"build_id"`
				SHA     releaseidentity.Digest `json:"sha256"`
			} `json:"archive_input"`
			OCI struct {
				Path string                 `json:"path"`
				SHA  releaseidentity.Digest `json:"sha256"`
			} `json:"oci_input"`
		} `json:"packages"`
	}
	if err := definitions.DecodeStrict(raw, &provenance); err != nil {
		return err
	}
	if provenance.Schema != "hikyo.dev/release-binaries/v1" || provenance.Commit != id.Commit || provenance.Version != id.Version || provenance.Producer.Name != "goreleaser" || provenance.Producer.BuildID != "hikyo" || provenance.Producer.Config != ".goreleaser.yaml" || provenance.Producer.ConfigSHA.Validate() != nil || len(provenance.Packages) != 2 {
		return errors.New("binary provenance producer or source identity mismatch")
	}
	seen := map[string]bool{}
	for _, p := range provenance.Packages {
		if (p.Goarch != "amd64" && p.Goarch != "arm64") || seen[p.Goarch] || p.Goos != "linux" || p.Archive.BuildID != "hikyo" || p.Archive.SHA.Validate() != nil || p.Archive.SHA != p.OCI.SHA || p.OCI.Path != "image-root/"+p.Goarch+"/hikyo" {
			return errors.New("binary provenance archive and image inputs differ")
		}
		seen[p.Goarch] = true
	}
	return nil
}
