package shell

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeFilename reduces a page-supplied name to a safe base name: no path,
// no control or bidirectional-format characters, no leading dots, bounded
// length (ADR D9, D11). The save panel still lets the user change it.
func SanitizeFilename(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == 0x2028, r == 0x2029:
			return -1
		case r == ':':
			return '-' // Finder shows ":" as "/"
		}
		return r
	}, name)
	base := strings.TrimSpace(path.Base(strings.ReplaceAll(name, `\`, "/")))
	base = strings.TrimLeft(base, ". ")
	if base == "" || base == "/" || base == "." {
		return "download"
	}
	ext := filepath.Ext(base)
	if len(ext) > 20 {
		ext = ""
	}
	stem := strings.TrimSuffix(base, ext)
	for len(stem) > 200 {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	if stem == "" {
		return "download" + ext
	}
	return stem + ext
}

// saveDownload always shows the save panel, then writes the file next to the
// chosen name and renames it into place, so cancel or failure writes nothing
// (ADR D11). Exactly one of proxiedPath and data is used.
func (c *Conn) saveDownload(ctx context.Context, w http.ResponseWriter, proxiedPath string, data []byte, name string) *shellErr {
	dest, ok := c.m.native.SavePanel(name, c.Info().Name)
	if !ok {
		return &shellErr{codeCancelled, "the download was cancelled"}
	}
	if err := c.writeDownload(ctx, dest, proxiedPath, data); err != nil {
		return err
	}
	return noContent(w)
}

func (c *Conn) writeDownload(ctx context.Context, dest, proxiedPath string, data []byte) *shellErr {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".hikyo-download-*")
	if err != nil {
		return &shellErr{codeFailed, "the file could not be created there"}
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	var werr error
	if proxiedPath != "" {
		werr = c.fetchInto(ctx, tmp, proxiedPath)
	} else {
		_, werr = tmp.Write(data)
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return &shellErr{codeFailed, "the download failed"}
	}
	// Marked before it gets its final name, so it is never there unmarked.
	if err := setQuarantine(tmp.Name()); err != nil {
		return &shellErr{codeFailed, "the file could not be marked as downloaded"}
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return &shellErr{codeFailed, "the file could not be saved there"}
	}
	return nil
}

var errDownload = errors.New("download refused")

// fetchInto GETs an API path with the connection's bearer. A JSON body is
// scanned like any proxied response: a session token in it is an error.
func (c *Conn) fetchInto(ctx context.Context, dst *os.File, p string) error {
	token := c.bearer()
	if token == "" {
		return errDownload
	}
	req, err := c.inst.newRequest(ctx, http.MethodGet, p, token, nil, "")
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")
	resp, err := c.inst.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
		return errDownload
	}
	if isJSONType(resp.Header.Get("Content-Type")) {
		raw, err := readLimited(resp.Body, maxScannedResponse)
		if err != nil {
			return err
		}
		if view, err := splitToken(raw); view.Token != "" || (err != nil && !errors.Is(err, errNotObject)) {
			return errDownload
		}
		_, err = dst.Write(raw)
		return err
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, maxDownloadFile+1))
	if err == nil && n > maxDownloadFile {
		err = errDownload
	}
	return err
}
