package shell

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// handoffPages are the pages that approve a sign-in or a ceremony. The app
// never opens them on a page's say-so (ADR D9): an approval is only ever
// started by the shell's own handoff.
var handoffPages = []string{"/login/cli", "/reauth/cli", "/workspace/approve"}

const maxBrowserPath = 2048

var errRefusedPath = errors.New("that path can't be opened in the browser from the app")

// BrowserURL validates a path with an optional query that a page wants opened
// on the connection's real origin, and returns the absolute URL (ADR D9). It
// refuses a URL, non-canonical input, the handoff and approval pages, and any
// returnTo parameter.
func (c *Conn) BrowserURL(raw string) (string, error) {
	if err := checkBrowserPath(raw); err != nil {
		return "", err
	}
	target, err := url.Parse(c.inst.Origin + raw)
	if err != nil || !c.sameSite(target) || target.User != nil || target.Fragment != "" {
		return "", errRefusedPath
	}
	return target.String(), nil
}

func checkBrowserPath(raw string) error {
	if raw == "" || len(raw) > maxBrowserPath || raw[0] != '/' || strings.HasPrefix(raw, "//") {
		return errRefusedPath
	}
	for _, r := range raw {
		if r == '\\' || r == '#' || unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || r > unicode.MaxASCII {
			return errRefusedPath
		}
	}
	path, query, hasQuery := strings.Cut(raw, "?")
	var decoded []string
	for _, seg := range strings.Split(path[1:], "/") {
		if seg == "" && path != "/" {
			return errRefusedPath
		}
		plain, err := url.PathUnescape(seg)
		if err != nil || plain == "." || plain == ".." || strings.ContainsAny(plain, "/\\") || hasEscape(plain) {
			return errRefusedPath
		}
		for _, r := range plain {
			if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
				return errRefusedPath
			}
		}
		decoded = append(decoded, strings.ToLower(plain))
	}
	normalized := strings.TrimRight("/"+strings.Join(decoded, "/"), "/")
	for _, page := range handoffPages {
		if normalized == page || strings.HasPrefix(normalized, page+"/") {
			return errRefusedPath
		}
	}
	if hasQuery {
		values, err := url.ParseQuery(query)
		if err != nil {
			return errRefusedPath
		}
		for key := range values {
			flat := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if flat == "returnto" || hasEscape(key) {
				return errRefusedPath
			}
		}
	}
	return nil
}

// hasEscape reports a remaining percent escape after one decoding, which is
// what a double-encoded segment looks like.
func hasEscape(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '%' && isHex(s[i+1]) && isHex(s[i+2]) {
			return true
		}
	}
	return false
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// ExternalLink decides whether a link a page activated may leave for the
// system browser (ADR D10): https only, no credentials, and a link to this
// connection's own origin obeys the D9 rules. The fragment is dropped.
func (c *Conn) ExternalLink(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", errRefusedPath
	}
	u.Fragment, u.RawFragment = "", ""
	if c.sameSite(u) {
		rel := u.EscapedPath()
		if rel == "" {
			rel = "/"
		}
		if u.RawQuery != "" {
			rel += "?" + u.RawQuery
		}
		return c.BrowserURL(rel)
	}
	return u.String(), nil
}

// sameSite reports whether u is this connection's origin under any spelling
// (case, default port, trailing dot, IDN or IPv6 form): such a link to the
// instance must not count as "external" and skip the D9 refusals.
func (c *Conn) sameSite(u *url.URL) bool {
	got, err := originOf(u)
	return err == nil && got == c.inst.Origin
}
