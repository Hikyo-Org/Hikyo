package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
)

// Extract only flat, regular archive members. Even signed archives cannot cause
// path traversal, link extraction, unbounded expansion or duplicate executables.
func extractNightlyBinary(name string, raw []byte) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractNightlyZip(raw)
	}
	if !strings.HasSuffix(name, ".tar.gz") {
		return nil, errors.New("selfupdate: unsupported nightly archive")
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxBinaryBytes + (16 << 20)}
	reader := tar.NewReader(limited)
	seen := map[string]bool{}
	var binary []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !safeName(header.Name) || seen[header.Name] || len(seen) >= 32 || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 0 || header.Size > maxBinaryBytes {
			return nil, errors.New("selfupdate: unsafe nightly archive member")
		}
		seen[header.Name] = true
		if header.Name == "hikyo" {
			binary, err = io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
			if err != nil || int64(len(binary)) != header.Size || len(binary) == 0 {
				return nil, errors.New("selfupdate: unreadable nightly executable")
			}
		}
	}
	// Consume the bounded gzip tail too so its checksum is verified.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, errors.New("selfupdate: nightly archive exceeds expansion bound")
	}
	if binary == nil {
		return nil, errors.New("selfupdate: nightly archive has no executable")
	}
	return binary, nil
}

func extractNightlyZip(raw []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) > 32 {
		return nil, errors.New("selfupdate: nightly archive member bound exceeded")
	}
	seen := map[string]bool{}
	var binary []byte
	var total uint64
	for _, file := range reader.File {
		if !safeName(file.Name) || seen[file.Name] || !file.Mode().IsRegular() || file.UncompressedSize64 > maxBinaryBytes {
			return nil, errors.New("selfupdate: unsafe nightly archive member")
		}
		seen[file.Name] = true
		total += file.UncompressedSize64
		if total > maxBinaryBytes+(16<<20) {
			return nil, errors.New("selfupdate: nightly archive exceeds expansion bound")
		}
		input, err := file.Open()
		if err != nil {
			return nil, err
		}
		if file.Name == "hikyo.exe" {
			binary, err = io.ReadAll(io.LimitReader(input, maxBinaryBytes+1))
			if err == nil && (len(binary) == 0 || uint64(len(binary)) != file.UncompressedSize64) {
				err = errors.New("selfupdate: unreadable nightly executable")
			}
		} else {
			var count int64
			count, err = io.Copy(io.Discard, io.LimitReader(input, maxBinaryBytes+1))
			if err == nil && uint64(count) != file.UncompressedSize64 {
				err = errors.New("selfupdate: unreadable nightly archive member")
			}
		}
		if err := errors.Join(err, input.Close()); err != nil {
			return nil, err
		}
	}
	if binary == nil {
		return nil, errors.New("selfupdate: nightly archive has no executable")
	}
	return binary, nil
}
