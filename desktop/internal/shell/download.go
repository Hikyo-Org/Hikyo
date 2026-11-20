package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
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

// downloadSource is a proxied download that answered 200: the shell knows it
// has something to save before it shows the save panel.
type downloadSource struct {
	body io.ReadCloser // streamed to the file; nil when head holds the whole body
	head []byte        // a scanned JSON body
	name string        // from Content-Disposition, "" when absent
}

func (d *downloadSource) close() {
	if d.body != nil {
		_ = d.body.Close()
	}
}

// saveDownload shows the save panel, writes the file next to the chosen name
// and renames it into place, so cancel or failure writes nothing (ADR D11).
// Exactly one of proxiedPath and data is used. A proxied download is fetched
// far enough to know its status before the panel appears.
func (c *Conn) saveDownload(ctx context.Context, w http.ResponseWriter, proxiedPath string, data []byte, name, fallbackName, mimeType string) *shellErr {
	if e := c.download(ctx, proxiedPath, data, name, fallbackName, mimeType); e != nil {
		return e
	}
	return noContent(w)
}

// download saves one file. name is the caller's choice, the response's
// Content-Disposition name is next, and fallbackName comes last.
func (c *Conn) download(ctx context.Context, proxiedPath string, data []byte, name, fallbackName, mimeType string) *shellErr {
	var src *downloadSource
	if proxiedPath != "" {
		var e *shellErr
		if src, e = c.openDownload(ctx, proxiedPath); e != nil {
			return e
		}
		defer src.close()
		if name == "" {
			name = src.name
		}
	}
	if name == "" {
		name = fallbackName
	}
	dest, ok := c.m.native.SavePanel(SanitizeFilename(name), c.Info().Name, mimeType)
	if !ok {
		return &shellErr{codeCancelled, "the download was cancelled"}
	}
	return c.writeDownload(dest, src, data)
}

func (c *Conn) writeDownload(dest string, src *downloadSource, data []byte) *shellErr {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".hikyo-download-*")
	if err != nil {
		return &shellErr{codeFailed, "the file could not be created there"}
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	var werr error
	switch {
	case src != nil && src.body != nil:
		var n int64
		n, werr = io.Copy(tmp, io.LimitReader(src.body, maxDownloadFile+1))
		if werr == nil && n > maxDownloadFile {
			werr = errDownload
		}
	case src != nil:
		_, werr = tmp.Write(src.head)
	default:
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

// openDownload GETs an API path with the connection's bearer and maps anything
// but a readable 200 to a specific message. A JSON body is scanned like any
// proxied response: a session token in it is an error.
func (c *Conn) openDownload(ctx context.Context, p string) (*downloadSource, *shellErr) {
	token := c.bearer()
	if token == "" {
		return nil, &shellErr{codeFailed, "this connection needs a new sign-in first"}
	}
	req, err := c.inst.newRequest(ctx, http.MethodGet, p, token, nil, "")
	if err != nil {
		return nil, &shellErr{codeInvalid, "that download address is not valid"}
	}
	req.Header.Set("Accept", "*/*")
	resp, err := c.inst.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, &shellErr{codeCancelled, "the download was cancelled"}
		}
		return nil, &shellErr{codeFailed, "the instance could not be reached"}
	}
	keep := false
	defer func() {
		if !keep {
			_ = resp.Body.Close()
		}
	}()
	if e := downloadStatusError(resp.StatusCode); e != nil {
		return nil, e
	}
	if resp.Header.Get("Content-Encoding") != "" {
		return nil, &shellErr{codeFailed, "the instance used an encoding the app can't read"}
	}
	src := &downloadSource{}
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		src.name = params["filename"]
	}
	if isJSONType(resp.Header.Get("Content-Type")) {
		raw, err := readLimited(resp.Body, maxScannedResponse)
		if err != nil {
			return nil, &shellErr{codeFailed, "the download could not be read"}
		}
		if view, err := splitToken(raw); view.Token != "" || (err != nil && !errors.Is(err, errNotObject)) {
			return nil, &shellErr{codeRefused, "the instance sent something the app won't save"}
		}
		src.head = raw
		return src, nil
	}
	src.body, keep = resp.Body, true
	return src, nil
}

// downloadStatusError says why a status other than 200 saved nothing.
func downloadStatusError(status int) *shellErr {
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized:
		return &shellErr{codeFailed, "the instance did not accept the session; sign in again and retry"}
	case status == http.StatusForbidden:
		return &shellErr{codeRefused, "your account may not download this"}
	case status == http.StatusNotFound:
		return &shellErr{codeFailed, "that file is not available on the instance"}
	case status == http.StatusTooManyRequests:
		return &shellErr{codeRateLimited, "the instance is rate limiting this app; try again shortly"}
	case status >= 500:
		return &shellErr{codeFailed, fmt.Sprintf("the instance could not produce this download (it answered %d)", status)}
	default:
		return &shellErr{codeFailed, fmt.Sprintf("the instance answered %d instead of a file", status)}
	}
}
