package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/sassoftware/go-rpmutils"
)

func TestInspectTarRejectsInstallHook(t *testing.T) {
	t.Parallel()

	archive := makeTar(t, map[string][]byte{
		".PKGINFO":      []byte("pkgname = hikyo\n"),
		".post-install": []byte("#!/bin/sh\nstart-service\n"),
	})
	inspection := newTarInspection()
	err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, map[string]bool{".PKGINFO": true}, false)
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
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, nil, false); err == nil {
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
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), inspection, nil, false); err == nil {
		t.Fatal("inspectTar accepted a non-executable packaged binary")
	}
}

func TestInspectTarRejectsUnsafeInstallationMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header tar.Header
	}{
		{name: "non-root owner", header: tar.Header{Name: binaryPath, Typeflag: tar.TypeReg, Mode: 0o755, Uid: 1000}},
		{name: "writable directory", header: tar.Header{Name: "usr/bin/", Typeflag: tar.TypeDir, Mode: 0o777}},
		{name: "extended attribute", header: tar.Header{Name: binaryPath, Typeflag: tar.TypeReg, Mode: 0o755, PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "value"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			writer := tar.NewWriter(&output)
			header := tc.header
			if header.Typeflag != tar.TypeDir {
				header.Size = 1
			}
			if err := writer.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
			if header.Size != 0 {
				if _, err := writer.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := inspectTar(tar.NewReader(bytes.NewReader(output.Bytes())), newTarInspection(), nil, false); err == nil {
				t.Fatal("inspectTar accepted unsafe installation metadata")
			}
		})
	}
}

func TestInspectTarAcceptsOnlyMatchingAPKChecksumMetadata(t *testing.T) {
	t.Parallel()

	content := []byte("binary")
	checksum := sha1.Sum(content)
	archive := makeTarWithHeader(t, tar.Header{
		Name:       binaryPath,
		Typeflag:   tar.TypeReg,
		Format:     tar.FormatPAX,
		Mode:       0o755,
		Size:       int64(len(content)),
		ModTime:    time.Unix(1, 500),
		PAXRecords: map[string]string{apkChecksumPAXKey: hex.EncodeToString(checksum[:])},
	}, content)
	header, err := tar.NewReader(bytes.NewReader(archive)).Next()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := header.PAXRecords[paxModTimeKey]; !ok {
		t.Fatal("test APK entry does not exercise standard PAX modification time metadata")
	}
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), newTarInspection(), nil, true); err != nil {
		t.Fatalf("matching APK checksum and standard modification time refused: %v", err)
	}

	archive = makeTarWithHeader(t, tar.Header{
		Name:       binaryPath,
		Typeflag:   tar.TypeReg,
		Mode:       0o755,
		Size:       int64(len(content)),
		PAXRecords: map[string]string{apkChecksumPAXKey: "0000000000000000000000000000000000000000"},
	}, content)
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), newTarInspection(), nil, true); err == nil {
		t.Fatal("mismatched APK checksum accepted")
	}

	archive = makeTarWithHeader(t, tar.Header{
		Name:     binaryPath,
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     int64(len(content)),
		PAXRecords: map[string]string{
			apkChecksumPAXKey: hex.EncodeToString(checksum[:]),
			"comment":         "untrusted metadata",
		},
	}, content)
	if err := inspectTar(tar.NewReader(bytes.NewReader(archive)), newTarInspection(), nil, true); err == nil {
		t.Fatal("APK checksum accompanied by non-time PAX metadata was accepted")
	}
}

func TestRPMMetadataRejectsCapabilitiesAndUnsafeFlags(t *testing.T) {
	t.Parallel()

	if tag, found := firstForbiddenRPMHook(func(candidate int) bool { return candidate == 5010 }); !found || tag != 5010 {
		t.Fatal("RPM FILECAPS metadata is not rejected")
	}
	if err := validateRPMFileFlags(binaryPath, 1); err == nil {
		t.Fatal("binary file flags were accepted")
	}
	if err := validateRPMFileFlags(licensePath, 1<<7); err != nil {
		t.Fatalf("standard RPM license marker refused: %v", err)
	}
}

type fakeRPMFile struct {
	rpmutils.FileInfo
	name  string
	mode  int
	flags int
}

func (f fakeRPMFile) Name() string { return f.name }
func (f fakeRPMFile) Mode() int    { return f.mode }
func (f fakeRPMFile) Flags() int   { return f.flags }

func TestValidateRPMHeaderFilesRejectsHeaderOnlyEntries(t *testing.T) {
	t.Parallel()

	inspection := newTarInspection()
	inspection.files[binaryPath] = true
	inspection.files[licensePath] = true
	good := []rpmutils.FileInfo{
		fakeRPMFile{name: "/" + binaryPath, mode: 0o100755},
		fakeRPMFile{name: "/" + licensePath, mode: 0o100644, flags: rpmutils.RPMFILE_LICENSE},
	}
	if err := validateRPMHeaderFiles(good, inspection); err != nil {
		t.Fatalf("genuine header file table refused: %v", err)
	}
	ghost := append(append([]rpmutils.FileInfo{}, good...), fakeRPMFile{name: "/etc/passwd", mode: 0o100644, flags: rpmutils.RPMFILE_GHOST})
	for name, files := range map[string][]rpmutils.FileInfo{
		"header-only ghost":       ghost,
		"ghost flag on payload":   {good[0], fakeRPMFile{name: "/" + licensePath, mode: 0o100644, flags: rpmutils.RPMFILE_GHOST}},
		"same count, other path":  {good[0], fakeRPMFile{name: "/etc/passwd", mode: 0o100644}},
		"duplicate entry":         {good[0], good[0]},
		"unsafe path":             {good[0], fakeRPMFile{name: "/usr/share/doc/../../etc/passwd", mode: 0o100644}},
		"symlink at allowed path": {good[0], fakeRPMFile{name: "/" + licensePath, mode: 0o120777}},
		"missing header entry":    good[:1],
	} {
		if err := validateRPMHeaderFiles(files, inspection); err == nil {
			t.Errorf("%s: header file table accepted", name)
		}
	}
}

func gzipText(t *testing.T, text string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestVerifyMTreeMatchesPackageEntries(t *testing.T) {
	t.Parallel()

	inspection := newTarInspection()
	inspection.dirs["usr"] = true
	inspection.dirs["usr/bin"] = true
	inspection.files[binaryPath] = true
	const header = "#mtree\n./.PKGINFO time=1.0 mode=644 size=1 type=file md5digest=aa sha256digest=bb\n./usr/ time=-62135596800.0 mode=755 type=dir\n"
	const bin = "./usr/bin/ time=1.0 mode=755 type=dir\n./usr/bin/hikyo time=1.0 mode=755 size=3 type=file\n"
	if err := verifyMTree(gzipText(t, header+bin), inspection); err != nil {
		t.Fatalf("genuine .MTREE refused: %v", err)
	}
	for name, text := range map[string]string{
		"extra path":       header + bin + "./etc/passwd time=1.0 mode=644 type=file\n",
		"missing path":     header,
		"duplicate path":   header + bin + bin,
		"absolute path":    header + bin + "/etc/passwd type=file\n",
		"relative path":    header + bin + "etc type=dir\n",
		"backslash escape": header + bin + "./usr/bin/hik\\171o type=file\n",
		"unknown keyword":  header + "./usr/bin/ type=dir uid=0\n./usr/bin/hikyo type=file\n",
		"unknown command":  header + bin + "/remove\n",
		"set command":      "#mtree\n/set uid=0 gname=wheel\n" + header[7:] + bin,
		"unset command":    header + bin + "/unset all\n",
		"unsafe path":      header + bin + "./usr/../etc/passwd type=file\n",
	} {
		if err := verifyMTree(gzipText(t, text), inspection); err == nil {
			t.Errorf("%s: .MTREE accepted", name)
		}
	}
	if err := verifyMTree([]byte("#mtree\n"+header[7:]+bin), inspection); err == nil {
		t.Error("uncompressed .MTREE accepted")
	}
	if err := verifyMTree(gzipText(t, header+bin+"#"+strings.Repeat("x", maxMTreeSize)), inspection); err == nil {
		t.Error("oversized .MTREE accepted")
	}
}

func TestParseFieldsAllowlist(t *testing.T) {
	t.Parallel()

	if _, err := parseFieldsAllowlist([]byte("Package: hikyo\nDescription: line\n more\n"), ":", debFields, false); err != nil {
		t.Fatalf("genuine Debian control refused: %v", err)
	}
	if _, err := parseFieldsAllowlist([]byte("# Generated by nfpm\npkgname = hikyo\n"), " = ", archFields, true); err != nil {
		t.Fatalf("genuine Arch .PKGINFO refused: %v", err)
	}
	for name, tc := range map[string]struct {
		data    string
		sep     string
		allowed map[string]bool
	}{
		"deb relationship":      {"Package: hikyo\nConflicts: other\n", ":", debFields},
		"deb provides":          {"Package: hikyo\nProvides: openssh-server\n", ":", debFields},
		"deb pre-depends":       {"Package: hikyo\nPre-Depends: evil\n", ":", debFields},
		"deb other case":        {"Package: hikyo\nprovides: openssh-server\n", ":", debFields},
		"deb repeated field":    {"Package: hikyo\nPackage: other\n", ":", debFields},
		"deb blank line":        {"Package: hikyo\n\nProvides: other\n", ":", debFields},
		"deb line without key":  {"Package: hikyo\nnonsense\n", ":", debFields},
		"apk replaces":          {"pkgname = hikyo\nreplaces = busybox\n", " = ", apkFields},
		"apk plain depend":      {"pkgname = hikyo\ndepend = libc\n", " = ", apkFields},
		"apk spaced conflict":   {"pkgname = hikyo\ndepend = libc !openssh-server\n", " = ", apkFields},
		"apk provides":          {"pkgname = hikyo\nprovides = cmd:sh\n", " = ", apkFields},
		"apk install_if":        {"pkgname = hikyo\ninstall_if = busybox\n", " = ", apkFields},
		"apk provider_priority": {"pkgname = hikyo\nprovider_priority = 100\n", " = ", apkFields},
		"apk no spaces":         {"pkgname = hikyo\ndepend=!busybox\n", " = ", apkFields},
		"apk repeated field":    {"pkgname = hikyo\npkgname = other\n", " = ", apkFields},
		"apk comment":           {"# note\npkgname = hikyo\n", " = ", apkFields},
		"arch provides":         {"pkgname = hikyo\nprovides = sh\n", " = ", archFields},
		"arch backup":           {"pkgname = hikyo\nbackup = etc/passwd\n", " = ", archFields},
		"arch conflict":         {"pkgname = hikyo\nconflict = pacman\n", " = ", archFields},
	} {
		if _, err := parseFieldsAllowlist([]byte(tc.data), tc.sep, tc.allowed, tc.allowed["pkgbase"]); err == nil {
			t.Errorf("%s: metadata accepted", name)
		}
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
		1054, 1090, // Conflicts and Obsoletes make rpm remove other packages.
		1027, // OLDFILENAMES shadows BASENAMES/DIRNAMES in go-rpmutils but not in rpm.
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

func makeTarWithHeader(t *testing.T, header tar.Header, content []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	if err := writer.WriteHeader(&header); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
