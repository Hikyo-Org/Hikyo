package main

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/sassoftware/go-rpmutils"
)

// These tests drive the per-format verify functions end to end, so removing a
// check from verifyDeb, verifyAPK, verifyArch or verifyRPM fails one of them.

var testIdentity = identity{version: "1.2.3", arch: "amd64"}

type tarFile struct {
	name    string
	mode    int64
	content string
}

func payloadFiles() []tarFile {
	return []tarFile{{binaryPath, 0o755, "binary"}, {licensePath, 0o644, "license"}}
}

func tarOfFiles(t *testing.T, files []tarFile) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for _, f := range files {
		if err := writer.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(f.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func wantError(t *testing.T, err error, substring string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %v, want one containing %q", err, substring)
	}
}

func arMember(name string, data []byte) []byte {
	member := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", name+"/", "0", "0", "0", "100644", len(data))
	out := append([]byte(member), data...)
	if len(data)%2 == 1 {
		out = append(out, '\n')
	}
	return out
}

func debPackage(t *testing.T, control string) string {
	t.Helper()
	data := []byte("!<arch>\n")
	data = append(data, arMember("debian-binary", []byte("2.0\n"))...)
	data = append(data, arMember("control.tar.gz", gzipBytes(t, tarOfFiles(t, []tarFile{{"control", 0o644, control}})))...)
	data = append(data, arMember("data.tar.gz", gzipBytes(t, tarOfFiles(t, payloadFiles())))...)
	return writeTemp(t, "hikyo.deb", data)
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	return gzipText(t, string(data))
}

func TestVerifyDebRefusesRelationships(t *testing.T) {
	t.Parallel()

	// The control file goreleaser v2.17.1 writes for an amd64 nightly.
	const base = "Package: hikyo\nVersion: 1.2.3\nSection: \nPriority: optional\nArchitecture: amd64\nArchitecture-Variant: amd64v1\nMaintainer: A <a@example.com>\nInstalled-Size: 1\nHomepage: https://example.com/\nDescription: x\n more\n"
	if _, err := verifyDeb(debPackage(t, base), testIdentity); err != nil {
		t.Fatalf("genuine Debian package refused: %v", err)
	}
	wrongVariant := strings.Replace(base, "amd64v1", "arm64", 1)
	_, err := verifyDeb(debPackage(t, wrongVariant), testIdentity)
	wantError(t, err, "Architecture-Variant")
	continued := strings.Replace(base, "Priority: optional\n", "Priority: optional\n Provides: openssh-server\n", 1)
	_, err = verifyDeb(debPackage(t, continued), testIdentity)
	wantError(t, err, "not in the allowlist")
	for _, field := range []string{"Conflicts", "Replaces", "Breaks", "Provides", "Pre-Depends", "Depends", "provides"} {
		_, err := verifyDeb(debPackage(t, base+field+": openssh-server\n"), testIdentity)
		wantError(t, err, "not in the allowlist")
	}
}

func apkPackage(t *testing.T, pkginfo string) string {
	t.Helper()
	files := append([]tarFile{{".PKGINFO", 0o644, pkginfo}}, payloadFiles()...)
	return writeTemp(t, "hikyo.apk", gzipBytes(t, tarOfFiles(t, files)))
}

func TestVerifyAPKRefusesRelationships(t *testing.T) {
	t.Parallel()

	const base = "pkgname = hikyo\npkgver = 1.2.3\narch = amd64\nsize = 1\npkgdesc = x\nurl = https://example.com/\nmaintainer = A <a@example.com>\nlicense = MPL-2.0\ndatahash = 00\n"
	if _, err := verifyAPK(apkPackage(t, base), testIdentity); err != nil {
		t.Fatalf("genuine APK refused: %v", err)
	}
	for name, extra := range map[string]string{
		"replaces":                    "replaces = busybox\n",
		"conflict":                    "depend = !busybox\n",
		"conflict hidden by last":     "depend = !busybox\ndepend = libc\n",
		"conflict among spaced items": "depend = libc !openssh-server\n",
		"plain depend":                "depend = libc\n",
		"provides":                    "provides = cmd:sh\n",
		"install_if":                  "install_if = busybox\n",
		"provider_priority":           "provider_priority = 100\n",
		"separator without spaces":    "depend=  !busybox\n",
	} {
		_, err := verifyAPK(apkPackage(t, base+extra), testIdentity)
		if err == nil {
			t.Errorf("%s: APK accepted", name)
		}
	}
	_, err := verifyAPK(apkPackage(t, base+"depend = libc !openssh-server\n"), testIdentity)
	wantError(t, err, "not in the allowlist")
}

func archPackage(t *testing.T, pkginfo string, mtree *string) string {
	t.Helper()
	files := append([]tarFile{{".PKGINFO", 0o644, pkginfo}}, payloadFiles()...)
	if mtree != nil {
		files = append(files, tarFile{".MTREE", 0o644, string(gzipText(t, *mtree))})
	}
	var out bytes.Buffer
	zw, err := zstd.NewWriter(&out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(tarOfFiles(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, "hikyo.pkg.tar.zst", out.Bytes())
}

func TestVerifyArchRefusesRelationshipsAndUnlistedPaths(t *testing.T) {
	t.Parallel()

	const (
		base  = "# Generated by nfpm\npkgname = hikyo\npkgbase = hikyo\npkgver = 1.2.3\narch = amd64\nsize = 1\nbuilddate = 1\npackager = Unknown Packager\n"
		mtree = "#mtree\n./.PKGINFO time=1.0 mode=644 size=1 type=file\n./usr/bin/hikyo time=1.0 mode=755 size=6 type=file\n./usr/share/doc/hikyo/LICENSE time=1.0 mode=644 size=7 type=file\n"
	)
	good := mtree
	if _, err := verifyArch(archPackage(t, base, &good), testIdentity); err != nil {
		t.Fatalf("genuine Arch package refused: %v", err)
	}
	for _, field := range []string{"conflict", "replaces", "provides", "backup", "depend"} {
		_, err := verifyArch(archPackage(t, base+field+" = pacman\n", &good), testIdentity)
		wantError(t, err, "not in the allowlist")
	}
	extra := mtree + "./etc/passwd time=1.0 mode=644 type=file\n"
	_, err := verifyArch(archPackage(t, base, &extra), testIdentity)
	wantError(t, err, ".MTREE")
	_, err = verifyArch(archPackage(t, base, nil), testIdentity)
	wantError(t, err, "no .MTREE")
}

type rpmEntry struct {
	tag, dataType, count int32
	data                 []byte
}

func rpmStrings(tag int, values ...string) rpmEntry {
	var data []byte
	for _, value := range values {
		data = append(append(data, value...), 0)
	}
	return rpmEntry{int32(tag), rpmutils.RPM_STRING_ARRAY_TYPE, int32(len(values)), data}
}

func rpmUint32s(tag int, values ...uint32) rpmEntry {
	var data bytes.Buffer
	for _, value := range values {
		_ = binary.Write(&data, binary.BigEndian, value)
	}
	return rpmEntry{int32(tag), rpmutils.RPM_INT32_TYPE, int32(len(values)), data.Bytes()}
}

func rpmHeaderBytes(entries []rpmEntry) []byte {
	var table, data bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&table, binary.BigEndian, [4]int32{e.tag, e.dataType, int32(data.Len()), e.count})
		data.Write(e.data)
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, [4]uint32{0x8eade801, 0, uint32(len(entries)), uint32(data.Len())})
	out.Write(table.Bytes())
	out.Write(data.Bytes())
	return out.Bytes()
}

func cpioEntry(name string, mode int, content string) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", 0, mode, 0, 0, 1, 0, len(content), 0, 0, 0, 0, len(name)+1, 0)
	out.WriteString(name + "\x00")
	for out.Len()%4 != 0 {
		out.WriteByte(0)
	}
	out.WriteString(content)
	for out.Len()%4 != 0 {
		out.WriteByte(0)
	}
	return out.Bytes()
}

type rpmFile struct {
	path       string
	mode       int
	flags      uint32
	content    string
	headerOnly bool
}

func genuineRPMFiles() []rpmFile {
	files := []rpmFile{}
	for _, dir := range []string{"usr", "usr/bin", "usr/share", "usr/share/doc", "usr/share/doc/hikyo"} {
		files = append(files, rpmFile{path: dir, mode: 0o040755})
	}
	return append(files,
		rpmFile{path: binaryPath, mode: 0o100755, content: "binary"},
		rpmFile{path: licensePath, mode: 0o100644, flags: rpmutils.RPMFILE_LICENSE, content: "license"},
	)
}

// rpmPackage builds a minimal RPM whose BASENAMES/DIRNAMES table lists files
// and whose cpio payload carries the entries that are not headerOnly. override
// replaces or adds header entries after the file table is generated.
func rpmPackage(t *testing.T, files []rpmFile, override ...rpmEntry) string {
	t.Helper()
	return rpmPackageWithDuplicates(t, files, nil, nil, override...)
}

// rpmPackageWithDuplicates also appends duplicates to the header index after the
// regular entries, repeating tags the way a crafted package could, and puts sig
// in the signature header.
func rpmPackageWithDuplicates(t *testing.T, files []rpmFile, sig, duplicates []rpmEntry, override ...rpmEntry) string {
	t.Helper()
	var bases, dirs, users, digests, links []string
	var dirIndexes, sizes, modes, flags, mtimes []uint32
	dirIndex := map[string]uint32{}
	var payloadData bytes.Buffer
	for _, f := range files {
		dir, base := path.Split("/" + f.path)
		if _, ok := dirIndex[dir]; !ok {
			dirIndex[dir] = uint32(len(dirs))
			dirs = append(dirs, dir)
		}
		bases = append(bases, base)
		dirIndexes = append(dirIndexes, dirIndex[dir])
		sizes = append(sizes, uint32(len(f.content)))
		modes = append(modes, uint32(f.mode))
		flags = append(flags, f.flags)
		mtimes = append(mtimes, 0)
		users, digests, links = append(users, "root"), append(digests, ""), append(links, "")
		if !f.headerOnly {
			payloadData.Write(cpioEntry("."+"/"+f.path, f.mode, f.content))
		}
	}
	payloadData.Write(cpioEntry("TRAILER!!!", 0, ""))
	entries := map[int]rpmEntry{}
	for _, e := range []rpmEntry{
		rpmStrings(rpmutils.NAME, "hikyo"), rpmStrings(rpmutils.VERSION, "1.2.3"),
		rpmStrings(rpmutils.RELEASE, "1"), rpmStrings(rpmutils.ARCH, "amd64"),
		rpmStrings(rpmutils.BASENAMES, bases...), rpmUint32s(rpmutils.DIRINDEXES, dirIndexes...),
		rpmStrings(rpmutils.DIRNAMES, dirs...), rpmUint32s(rpmutils.FILESIZES, sizes...),
		rpmUint32s(rpmutils.FILEMODES, modes...), rpmUint32s(rpmutils.FILEFLAGS, flags...),
		rpmUint32s(rpmutils.FILEMTIMES, mtimes...), rpmStrings(rpmutils.FILEUSERNAME, users...),
		rpmStrings(rpmutils.FILEGROUPNAME, users...), rpmStrings(rpmutils.FILEDIGESTS, digests...),
		rpmStrings(rpmutils.FILELINKTOS, links...),
	} {
		entries[int(e.tag)] = e
	}
	for _, e := range override {
		entries[int(e.tag)] = e
	}
	ordered := make([]rpmEntry, 0, len(entries))
	for tag := int32(0); tag < 6000; tag++ {
		if e, ok := entries[int(tag)]; ok {
			ordered = append(ordered, e)
		}
	}
	ordered = append(ordered, duplicates...)
	lead := make([]byte, 96)
	binary.BigEndian.PutUint32(lead, 0xedabeedb)
	data := append(lead, rpmHeaderBytes(sig)...)
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	data = append(data, rpmHeaderBytes(ordered)...)
	data = append(data, payloadData.Bytes()...)
	return writeTemp(t, "hikyo.rpm", data)
}

func TestVerifyRPMRefusesHeaderTricks(t *testing.T) {
	t.Parallel()

	id := identity{version: "0:1.2.3-1", arch: "amd64"}
	if _, err := verifyRPM(rpmPackage(t, genuineRPMFiles()), id); err != nil {
		t.Fatalf("genuine RPM refused: %v", err)
	}
	_, err := verifyRPM(rpmPackage(t, genuineRPMFiles(), rpmStrings(rpmutils.POSTIN, "start-service")), id)
	wantError(t, err, "forbidden side-effect tag 1024")
	_, err = verifyRPM(rpmPackage(t, genuineRPMFiles(), rpmStrings(rpmutils.CONFLICTNAME, "openssh")), id)
	wantError(t, err, "forbidden side-effect tag 1054")

	// go-rpmutils prefers OLDFILENAMES (the genuine paths) while rpm prefers
	// BASENAMES/DIRNAMES, which also list a ghost /etc/passwd.
	ghost := append(genuineRPMFiles(), rpmFile{path: "etc/passwd", mode: 0o100644, flags: rpmutils.RPMFILE_GHOST, headerOnly: true})
	var oldNames []string
	for _, f := range genuineRPMFiles() {
		oldNames = append(oldNames, "/"+f.path)
	}
	_, err = verifyRPM(rpmPackage(t, ghost, rpmStrings(rpmutils.OLDFILENAMES, oldNames...)), id)
	wantError(t, err, "forbidden side-effect tag 1027")

	_, err = verifyRPM(rpmPackage(t, ghost), id)
	wantError(t, err, "RPM header lists")

	files := genuineRPMFiles()
	dirIndexes := make([]uint32, len(files)-1)
	_, err = verifyRPM(rpmPackage(t, files, rpmUint32s(rpmutils.DIRINDEXES, dirIndexes...)), id)
	wantError(t, err, "directory indexes")
	outOfRange := make([]uint32, len(files))
	outOfRange[0] = 99
	_, err = verifyRPM(rpmPackage(t, files, rpmUint32s(rpmutils.DIRINDEXES, outOfRange...)), id)
	wantError(t, err, "out of range")
}

func TestVerifyRPMRefusesDuplicateTagsAndMalformedHeaders(t *testing.T) {
	t.Parallel()

	id := identity{version: "0:1.2.3-1", arch: "amd64"}
	files := genuineRPMFiles()
	var bases []string
	for _, f := range files {
		_, base := path.Split("/" + f.path)
		bases = append(bases, base)
	}
	// go-rpmutils keeps the last copy of a repeated tag; rpm may act on another.
	for name, dup := range map[string]rpmEntry{
		"BASENAMES": rpmStrings(rpmutils.BASENAMES, bases...),
		"NAME":      rpmStrings(rpmutils.NAME, "hikyo"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := verifyRPM(rpmPackageWithDuplicates(t, files, nil, []rpmEntry{dup}), id)
			wantError(t, err, "repeats tag")
		})
	}
	sigSize := rpmUint32s(1000, 1) // RPMSIGTAG_SIZE
	_, err := verifyRPM(rpmPackageWithDuplicates(t, files, []rpmEntry{sigSize, sigSize}, nil), id)
	wantError(t, err, "repeats tag")

	// A short FILEMODES array makes go-rpmutils panic; verifyRPM must report it.
	_, err = verifyRPM(rpmPackage(t, files, rpmUint32s(rpmutils.FILEMODES, 0o040755)), id)
	wantError(t, err, "malformed RPM header")
}

func TestValidateRPMNameTableRequiresCanonicalNames(t *testing.T) {
	t.Parallel()

	id := identity{version: "0:1.2.3-1", arch: "amd64"}
	files := genuineRPMFiles()
	// Index 4 holds "/usr/bin/", the directory of the binary.
	dirs := []string{"/", "/usr/", "/usr/share/", "/usr/share/doc/", "/usr/bin/", "/usr/share/doc/hikyo/"}
	if _, err := verifyRPM(rpmPackage(t, files, rpmStrings(rpmutils.DIRNAMES, dirs...)), id); err != nil {
		t.Fatalf("test directory table is not genuine: %v", err)
	}
	for _, dir := range []string{"/x/../usr/bin/", "/usr//bin/", "/usr/bin", "usr/bin/", "/usr/./bin/"} {
		withDir := append([]string{}, dirs...)
		withDir[4] = dir
		_, err := verifyRPM(rpmPackage(t, files, rpmStrings(rpmutils.DIRNAMES, withDir...)), id)
		wantError(t, err, "not canonical")
	}
	var bases []string
	for _, f := range files {
		_, base := path.Split("/" + f.path)
		bases = append(bases, base)
	}
	for _, base := range []string{"", ".", "..", "bin/hikyo"} {
		withBase := append([]string{}, bases...)
		withBase[5] = base
		_, err := verifyRPM(rpmPackage(t, files, rpmStrings(rpmutils.BASENAMES, withBase...)), id)
		wantError(t, err, "not a single path element")
	}
}
