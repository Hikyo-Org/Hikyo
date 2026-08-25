// Command verify-native-packages proves that each native Linux package is an
// inert wrapper around the already-built release binary and repository license.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/sassoftware/go-rpmutils"
)

const (
	binaryPath  = "usr/bin/hikyo"
	licensePath = "usr/share/doc/hikyo/LICENSE"
)

type identity struct {
	filename string
	version  string
	arch     string
}

type payload struct {
	binary  []byte
	license []byte
}

type tarInspection struct {
	payload  payload
	metadata map[string][]byte
	files    map[string]bool
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/release/verify-native-packages.go DIST VERSION")
		os.Exit(2)
	}
	if err := verifyAll(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "native package verification: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("native package verification: eight packages contain only the canonical binary and LICENSE")
}

func verifyAll(dist, version string) error {
	repoRoot, err := repositoryRoot()
	if err != nil {
		return err
	}
	license, err := os.ReadFile(filepath.Join(repoRoot, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read repository LICENSE: %w", err)
	}

	for _, arch := range []string{"amd64", "arm64"} {
		canonical, err := readCanonicalArchive(dist, version, arch)
		if err != nil {
			return err
		}
		for _, format := range []string{"deb", "rpm", "apk", "archlinux"} {
			id, err := packageIdentity(repoRoot, version, format, arch)
			if err != nil {
				return err
			}
			packagePath := filepath.Join(dist, id.filename)
			var got payload
			switch format {
			case "deb":
				got, err = verifyDeb(packagePath, id)
			case "rpm":
				got, err = verifyRPM(packagePath, id)
			case "apk":
				got, err = verifyAPK(packagePath, id)
			case "archlinux":
				got, err = verifyArch(packagePath, id)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", id.filename, err)
			}
			if !bytes.Equal(got.binary, canonical) {
				return fmt.Errorf("%s: packaged binary differs from canonical Linux archive", id.filename)
			}
			if !bytes.Equal(got.license, license) {
				return fmt.Errorf("%s: packaged LICENSE differs from repository LICENSE", id.filename)
			}
			if runtime.GOOS == "linux" && runtime.GOARCH == arch {
				if err := verifyExecutable(got.binary, version); err != nil {
					return fmt.Errorf("%s: %w", id.filename, err)
				}
			}
		}
	}
	return nil
}

func repositoryRoot() (string, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot resolve verifier source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..")), nil
}

func packageIdentity(repoRoot, version, format, arch string) (identity, error) {
	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "release", "package-identity.sh"), version, format, arch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return identity{}, fmt.Errorf("resolve %s/%s identity: %s", format, arch, strings.TrimSpace(string(out)))
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return identity{}, fmt.Errorf("resolve %s/%s identity: invalid helper output", format, arch)
	}
	return identity{filename: parts[0], version: parts[1], arch: parts[2]}, nil
}

func readCanonicalArchive(dist, version, arch string) ([]byte, error) {
	archiveArch := arch
	if arch == "amd64" {
		archiveArch = "x86_64"
	}
	name := fmt.Sprintf("hikyo_%s_Linux_%s.tar.gz", version, archiveArch)
	f, err := os.Open(filepath.Join(dist, name))
	if err != nil {
		return nil, fmt.Errorf("open canonical archive %s: %w", name, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open canonical archive %s: %w", name, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var binary []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read canonical archive %s: %w", name, err)
		}
		entry, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return nil, fmt.Errorf("canonical archive %s: %w", name, err)
		}
		if entry != "hikyo" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("canonical archive %s: hikyo is not a regular file", name)
		}
		if binary != nil {
			return nil, fmt.Errorf("canonical archive %s: duplicate hikyo binary", name)
		}
		binary, err = io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read canonical binary %s: %w", name, err)
		}
	}
	if binary == nil {
		return nil, fmt.Errorf("canonical archive %s has no hikyo binary", name)
	}
	return binary, nil
}

func verifyDeb(filename string, id identity) (payload, error) {
	members, err := readAR(filename)
	if err != nil {
		return payload{}, err
	}
	if string(members["debian-binary"]) != "2.0\n" {
		return payload{}, errors.New("invalid debian-binary marker")
	}
	if len(members) != 3 {
		return payload{}, fmt.Errorf("unexpected Debian archive members: %v", mapKeys(members))
	}
	controlData, controlName, err := oneCompressedMember(members, "control.tar")
	if err != nil {
		return payload{}, err
	}
	data, dataName, err := oneCompressedMember(members, "data.tar")
	if err != nil {
		return payload{}, err
	}
	if controlName != "control.tar.gz" || dataName != "data.tar.gz" {
		return payload{}, fmt.Errorf("unsupported Debian member compression: %s, %s", controlName, dataName)
	}
	controlTar, err := ungzip(controlData)
	if err != nil {
		return payload{}, fmt.Errorf("read Debian control archive: %w", err)
	}
	control, err := inspectDebControl(tar.NewReader(bytes.NewReader(controlTar)))
	if err != nil {
		return payload{}, err
	}
	fields := parseFields(control)
	if fields["Package"] != "hikyo" || fields["Version"] != id.version || fields["Architecture"] != id.arch {
		return payload{}, fmt.Errorf("Debian identity mismatch: Package=%q Version=%q Architecture=%q", fields["Package"], fields["Version"], fields["Architecture"])
	}
	dataTar, err := ungzip(data)
	if err != nil {
		return payload{}, fmt.Errorf("read Debian data archive: %w", err)
	}
	inspection := newTarInspection()
	if err := inspectTar(tar.NewReader(bytes.NewReader(dataTar)), inspection, nil); err != nil {
		return payload{}, err
	}
	return finishPayload(inspection)
}

func verifyAPK(filename string, id identity) (payload, error) {
	f, err := os.Open(filename)
	if err != nil {
		return payload{}, err
	}
	defer f.Close()
	inspection := newTarInspection()
	if err := inspectGzipTarMembers(f, inspection, map[string]bool{".PKGINFO": true}); err != nil {
		return payload{}, err
	}
	pkginfo, ok := inspection.metadata[".PKGINFO"]
	if !ok {
		return payload{}, errors.New("APK has no .PKGINFO")
	}
	fields := parseEquals(pkginfo)
	if fields["pkgname"] != "hikyo" || fields["pkgver"] != id.version || fields["arch"] != id.arch {
		return payload{}, fmt.Errorf("APK identity mismatch: pkgname=%q pkgver=%q arch=%q", fields["pkgname"], fields["pkgver"], fields["arch"])
	}
	return finishPayload(inspection)
}

func verifyArch(filename string, id identity) (payload, error) {
	f, err := os.Open(filename)
	if err != nil {
		return payload{}, err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return payload{}, fmt.Errorf("open Arch zstd payload: %w", err)
	}
	defer zr.Close()
	inspection := newTarInspection()
	if err := inspectTar(tar.NewReader(zr), inspection, map[string]bool{".PKGINFO": true, ".MTREE": true}); err != nil {
		return payload{}, err
	}
	pkginfo, ok := inspection.metadata[".PKGINFO"]
	if !ok {
		return payload{}, errors.New("Arch package has no .PKGINFO")
	}
	fields := parseEquals(pkginfo)
	if fields["pkgname"] != "hikyo" || fields["pkgver"] != id.version || fields["arch"] != id.arch {
		return payload{}, fmt.Errorf("Arch identity mismatch: pkgname=%q pkgver=%q arch=%q", fields["pkgname"], fields["pkgver"], fields["arch"])
	}
	return finishPayload(inspection)
}

func verifyRPM(filename string, id identity) (payload, error) {
	f, err := os.Open(filename)
	if err != nil {
		return payload{}, err
	}
	defer f.Close()
	rpm, err := rpmutils.ReadRpm(f)
	if err != nil {
		return payload{}, err
	}
	nevra, err := rpm.Header.GetNEVRA()
	if err != nil {
		return payload{}, err
	}
	metadataVersion := fmt.Sprintf("%s:%s-%s", nevra.Epoch, nevra.Version, nevra.Release)
	if nevra.Name != "hikyo" || metadataVersion != id.version || nevra.Arch != id.arch {
		return payload{}, fmt.Errorf("RPM identity mismatch: Name=%q EVR=%q Arch=%q", nevra.Name, metadataVersion, nevra.Arch)
	}
	if tag, found := firstForbiddenRPMHook(rpm.Header.HasTag); found {
		return payload{}, fmt.Errorf("RPM contains forbidden script tag %d", tag)
	}
	reader, err := rpm.PayloadReaderExtended()
	if err != nil {
		return payload{}, err
	}
	inspection := newTarInspection()
	for {
		info, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return payload{}, err
		}
		entry, err := cleanArchivePath(info.Name())
		if err != nil {
			return payload{}, err
		}
		kind := info.Mode() & 0o170000
		switch kind {
		case 0o040000:
			if !allowedPayloadDirectory(entry) {
				return payload{}, fmt.Errorf("unexpected RPM directory %q", entry)
			}
		case 0o100000:
			content, err := io.ReadAll(reader)
			if err != nil {
				return payload{}, err
			}
			if err := validatePayloadMode(entry, int64(info.Mode())); err != nil {
				return payload{}, err
			}
			if err := recordPayloadFile(inspection, entry, content); err != nil {
				return payload{}, err
			}
		default:
			return payload{}, fmt.Errorf("unexpected RPM payload type for %q", entry)
		}
	}
	return finishPayload(inspection)
}

func forbiddenRPMScriptTags() []int {
	return []int{
		rpmutils.PREIN, rpmutils.POSTIN, rpmutils.PREUN, rpmutils.POSTUN,
		rpmutils.TRIGGERSCRIPTS, rpmutils.VERIFYSCRIPT,
		rpmutils.PREINPROG, rpmutils.POSTINPROG, rpmutils.PREUNPROG,
		rpmutils.POSTUNPROG, rpmutils.TRIGGERSCRIPTPROG, rpmutils.VERIFYSCRIPTPROG,
		1151, 1152, 1153, 1154, // Pre/post-transaction scripts and interpreters.
		1171,       // Trigger-pre-install. go-rpmutils v0.4.0 has the obsolete 1170 value.
		5066, 5067, // File-trigger scripts and interpreters.
		5076, 5077, // Transaction file-trigger scripts and interpreters.
		5103, 5104, 5105, 5106, // Pre/post-untransaction scripts and interpreters.
		5109, // Native sysusers metadata creates accounts during installation.
	}
}

func firstForbiddenRPMHook(hasTag func(int) bool) (int, bool) {
	for _, tag := range forbiddenRPMScriptTags() {
		if hasTag(tag) {
			return tag, true
		}
	}
	return 0, false
}

func readAR(filename string) (map[string][]byte, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || string(data[:8]) != "!<arch>\n" {
		return nil, errors.New("invalid ar magic")
	}
	members := make(map[string][]byte)
	for offset := 8; offset < len(data); {
		if len(data)-offset < 60 || string(data[offset+58:offset+60]) != "`\n" {
			return nil, errors.New("invalid ar member header")
		}
		header := data[offset : offset+60]
		name := strings.TrimSuffix(strings.TrimSpace(string(header[:16])), "/")
		size, err := strconv.Atoi(strings.TrimSpace(string(header[48:58])))
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid ar member size for %q", name)
		}
		start := offset + 60
		end := start + size
		if end > len(data) || name == "" {
			return nil, errors.New("truncated or unnamed ar member")
		}
		if _, exists := members[name]; exists {
			return nil, fmt.Errorf("duplicate ar member %q", name)
		}
		members[name] = data[start:end]
		offset = end + size%2
	}
	return members, nil
}

func oneCompressedMember(members map[string][]byte, prefix string) ([]byte, string, error) {
	for name, data := range members {
		if strings.HasPrefix(name, prefix) {
			return data, name, nil
		}
	}
	return nil, "", fmt.Errorf("missing %s member", prefix)
}

func ungzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func inspectDebControl(tr *tar.Reader) ([]byte, error) {
	var control []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entry, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			if entry != "." {
				return nil, fmt.Errorf("unexpected Debian control directory %q", entry)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("unexpected Debian control entry type for %q", entry)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		switch entry {
		case "control":
			if control != nil {
				return nil, errors.New("duplicate Debian control file")
			}
			control = content
		case "md5sums":
		default:
			return nil, fmt.Errorf("forbidden Debian control file %q", entry)
		}
	}
	if control == nil {
		return nil, errors.New("Debian package has no control file")
	}
	return control, nil
}

func inspectGzipTarMembers(r io.Reader, inspection *tarInspection, metadata map[string]bool) error {
	buffered := bufio.NewReader(r)
	members := 0
	for {
		if _, err := buffered.Peek(1); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}
		zr, err := gzip.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("open APK gzip member %d: %w", members+1, err)
		}
		zr.Multistream(false)
		if err := inspectTar(tar.NewReader(zr), inspection, metadata); err != nil {
			zr.Close()
			return err
		}
		if _, err := io.Copy(io.Discard, zr); err != nil {
			zr.Close()
			return err
		}
		if err := zr.Close(); err != nil {
			return err
		}
		members++
	}
	if members == 0 {
		return errors.New("APK has no gzip members")
	}
	return nil
}

func newTarInspection() *tarInspection {
	return &tarInspection{metadata: make(map[string][]byte), files: make(map[string]bool)}
}

func inspectTar(tr *tar.Reader, inspection *tarInspection, metadata map[string]bool) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entry, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeDir {
			if !allowedPayloadDirectory(entry) {
				return fmt.Errorf("unexpected package directory %q", entry)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return fmt.Errorf("unexpected package entry type for %q", entry)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if metadata != nil && metadata[entry] {
			if _, exists := inspection.metadata[entry]; exists {
				return fmt.Errorf("duplicate package metadata %q", entry)
			}
			inspection.metadata[entry] = content
			continue
		}
		if err := validatePayloadMode(entry, hdr.Mode); err != nil {
			return err
		}
		if err := recordPayloadFile(inspection, entry, content); err != nil {
			return err
		}
	}
}

func validatePayloadMode(entry string, mode int64) error {
	want := int64(0o644)
	if entry == binaryPath {
		want = 0o755
	}
	if mode&0o7777 != want {
		return fmt.Errorf("package payload %q has mode %#o, want %#o", entry, mode&0o7777, want)
	}
	return nil
}

func recordPayloadFile(inspection *tarInspection, entry string, content []byte) error {
	if inspection.files[entry] {
		return fmt.Errorf("duplicate package payload %q", entry)
	}
	inspection.files[entry] = true
	switch entry {
	case binaryPath:
		inspection.payload.binary = content
	case licensePath:
		inspection.payload.license = content
	default:
		return fmt.Errorf("unexpected package payload %q", entry)
	}
	return nil
}

func finishPayload(inspection *tarInspection) (payload, error) {
	if len(inspection.files) != 2 || !inspection.files[binaryPath] || !inspection.files[licensePath] {
		return payload{}, fmt.Errorf("package payload must contain exactly %s and %s", binaryPath, licensePath)
	}
	return inspection.payload, nil
}

func cleanArchivePath(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return ".", nil
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(name, "/") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func allowedPayloadDirectory(entry string) bool {
	switch entry {
	case ".", "usr", "usr/bin", "usr/share", "usr/share/doc", "usr/share/doc/hikyo":
		return true
	default:
		return false
	}
}

func parseFields(data []byte) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return fields
}

func parseEquals(data []byte) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return fields
}

func verifyExecutable(binary []byte, version string) error {
	dir, err := os.MkdirTemp("", "hikyo-native-package-exec.*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	filename := filepath.Join(dir, "hikyo")
	if err := os.WriteFile(filename, binary, 0o700); err != nil {
		return err
	}
	out, err := exec.Command(filename, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("packaged binary version command failed: %s", strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != version {
		return fmt.Errorf("packaged binary reports unexpected version: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func mapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
