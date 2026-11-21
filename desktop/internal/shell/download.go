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
	body   io.ReadCloser // streamed to the file; nil when head holds the whole body
	head   []byte        // a scanned JSON body
	name   string        // from Content-Disposition, "" when absent
	cancel context.CancelFunc
}

func (d *downloadSource) close() {
	if d.body != nil {
		_ = d.body.Close()
	}
	if d.cancel != nil {
		d.cancel()
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
	dest, replace, ok := c.m.native.SavePanel(SanitizeFilename(name), c.Info().Name, mimeType)
	// A request cancelled while the panel was open saves nothing, however the
	// panel was answered.
	if !ok || ctx.Err() != nil {
		return &shellErr{codeCancelled, "the download was cancelled"}
	}
	// replace is the panel's own answer; a file that appeared later is never
	// replaced (PublishDownload).
	return c.writeDownload(ctx, dest, replace, src, data)
}

func (c *Conn) writeDownload(ctx context.Context, dest string, replaceApproved bool, src *downloadSource, data []byte) *shellErr {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".hikyo-download-*")
	if err != nil {
		return &shellErr{codeFailed, "the file could not be created there"}
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	var werr error
	switch {
	case src != nil && src.body != nil:
		var n int64
		// The idle clock starts now, not when the headers arrived: the save
		// panel may have been open for a while.
		idle := newIdleReader(src.body, downloadIdle, src.cancel)
		n, werr = io.Copy(tmp, io.LimitReader(idle, maxDownloadFile+1))
		idle.stop()
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
	if ctx.Err() != nil {
		return &shellErr{codeCancelled, "the download was cancelled"}
	}
	saved, err := PublishDownload(tmp.Name(), dest, replaceApproved)
	if err != nil {
		return &shellErr{codeFailed, "the file could not be saved there"}
	}
	if saved != dest {
		c.m.native.Alert("Download saved under another name", "A file with that name appeared while the download ran, so it was saved as "+filepath.Base(saved)+".")
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
	ctx, cancel := context.WithCancel(ctx)
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	// The bearer is added at send time and a stale one is retried once, as for
	// any proxied read (ADR D4). The transport follows no redirect.
	req, err := c.inst.newRequest(ctx, http.MethodGet, p, "", nil, "")
	if err != nil {
		return nil, &shellErr{codeInvalid, "that download address is not valid"}
	}
	req.Header.Set("Accept", "*/*")
	resp, err := bearerTransport{c}.RoundTrip(req)
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
		idle := newIdleReader(resp.Body, downloadIdle, cancel)
		raw, err := readLimited(idle, maxScannedResponse)
		idle.stop()
		if err != nil {
			return nil, &shellErr{codeFailed, "the download could not be read"}
		}
		view, err := splitToken(raw)
		if view.Token != "" || (err != nil && !errors.Is(err, errNotObject)) {
			// Same as a proxied response: a session the shell did not ask for
			// is ended, not just dropped.
			if view.Token != "" && view.Token != c.bearer() {
				c.revokeAsync(view.Token)
			}
			return nil, &shellErr{codeRefused, "the instance sent something the app won't save"}
		}
		src.head = raw
		return src, nil
	}
	src.body, src.cancel, keep, ok = resp.Body, cancel, true, true
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
