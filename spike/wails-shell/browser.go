package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// bearerClient never follows a redirect. Go keeps Authorization on same-domain
// redirects, and the bearer belongs to the instance's origin alone.
var bearerClient = &http.Client{
	Timeout:       5 * time.Minute,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// openExternal is the only place the shell opens a URL, and instance code
// chooses those URLs, so it takes absolute http or https with a host and no
// credentials, like internal/cli's browserCommand. HIKYO_SPIKE_BROWSER=print logs instead.
func openExternal(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("browser URL must be an absolute http or https URL without credentials")
	}
	if os.Getenv("HIKYO_SPIKE_BROWSER") == "print" {
		log.Printf("BROWSER %s", u)
		return nil
	}
	return openURL(u.String())
}

// downloadPath picks where a download lands: a base name only, never an existing file.
// shortcut: a fixed directory and a numeric suffix; a product shows a save panel, when downloads are productised.
func downloadPath(suggested string) (string, error) {
	dir := os.Getenv("HIKYO_SPIKE_DOWNLOAD_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Downloads")
	}
	// The name comes from instance code: no control or bidi characters, and short enough for the filesystem.
	base := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, suggested)
	base = strings.TrimLeft(path.Base(strings.ReplaceAll(base, `\`, "/")), ".")
	if base == "" || base == "/" {
		base = "download"
	}
	ext := filepath.Ext(base)
	if len(ext) > 20 {
		ext = ""
	}
	stem := []rune(strings.TrimSuffix(base, ext))
	for len(string(stem)) > 200 {
		stem = stem[:len(stem)-1]
	}
	name := string(stem) + ext
	for i := 1; i <= 1000; i++ {
		_, err := os.Lstat(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			return filepath.Join(dir, name), nil
		}
		if err != nil {
			return "", err
		}
		name = string(stem) + " (" + strconv.Itoa(i) + ")" + ext
	}
	return "", errors.New("too many files with that name")
}

// connections lets a native callback find the connection a wails:// URL belongs to; main sets it.
var connections map[string]*conn

// saveThroughProxy fetches a same-connection wails:// URL with the connection's bearer and saves
// it. WKDownload cannot load that scheme, so this is the fallback for <a download> to an API URL.
// shortcut: no progress or cancel, a fixed directory, and the name is Content-Disposition or the last path segment because the policy callback never sees the download attribute; a product streams to a save panel and takes the name from the click.
func saveThroughProxy(raw string) (string, int64, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wails" || connections[u.Host] == nil {
		return "", 0, errors.New("not a URL of a known connection")
	}
	c := connections[u.Host]
	target := c.origin.ResolveReference(&url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery})
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer())
	resp, err := bearerClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("instance answered %d", resp.StatusCode)
	}
	name := path.Base(u.Path)
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	dest, err := downloadPath(name)
	if err != nil {
		return "", 0, err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return dest, n, err
}
