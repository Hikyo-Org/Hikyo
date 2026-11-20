package shell

import (
	"context"
	"log"
	"net/url"
	"strings"
)

// NavAction is what the native navigation delegate does with a request.
type NavAction int

const (
	// NavCancel refuses the navigation.
	NavCancel NavAction = iota
	// NavAllow lets the window load it.
	NavAllow
	// NavDownload lets WebKit download it (a blob of the window's own origin).
	NavDownload
)

// NavRequest describes one navigation, as the native delegate sees it.
type NavRequest struct {
	// PageHost is the host of the page making the request; empty before the
	// window's first load.
	PageHost string
	URL      string
	// MainFrame, NewWindow, Download and UserGesture come from WKNavigationAction.
	MainFrame, NewWindow, Download, UserGesture bool
}

// Navigate decides a navigation (ADR D10, D11). A window may navigate only
// within its own connection. An https link leaves for the system browser on a
// user gesture, rate limited and under D9's rules. Every other scheme is
// cancelled.
func (m *Manager) Navigate(req NavRequest) NavAction {
	u, err := url.Parse(req.URL)
	if err != nil {
		return NavCancel
	}
	scheme := strings.ToLower(u.Scheme)
	ownHost := req.PageHost
	conn := m.conn(ownHost)
	known := ownHost == AppHost || conn != nil
	switch {
	case req.Download:
		// A download has no first-load case: an unknown page host is refused.
		switch {
		case scheme == PageScheme && conn != nil && strings.EqualFold(u.Host, ownHost):
			m.background(func() { conn.saveProxiedURL(u) })
		case scheme == "blob" && known && strings.HasPrefix(req.URL, "blob:"+PageScheme+"://"+ownHost+"/"):
			if conn != nil && !conn.downloadLimit.allow() {
				return NavCancel
			}
			return NavDownload
		}
		return NavCancel
	case scheme == PageScheme:
		if ownHost == "" {
			// The window's first load: the shell opened it on a host it knows.
			if h := strings.ToLower(u.Host); h == AppHost || m.conn(h) != nil {
				return NavAllow
			}
			return NavCancel
		}
		if strings.EqualFold(u.Host, ownHost) {
			return NavAllow
		}
		return NavCancel
	case scheme == "about" && req.URL == "about:blank":
		return NavAllow
	case scheme == "https" && conn != nil && (req.MainFrame || req.NewWindow) && (req.UserGesture || req.NewWindow):
		target, err := conn.ExternalLink(req.URL)
		if err != nil || !conn.linkLimit.allow() {
			return NavCancel
		}
		if err := m.opts.OpenURL(target); err != nil {
			log.Printf("connection %s: opening a link failed: %v", conn.host, err)
		}
		return NavCancel
	}
	return NavCancel
}

// saveProxiedURL saves a same-connection URL the page navigated to with the
// download attribute: a save panel first, then the proxied fetch.
func (c *Conn) saveProxiedURL(u *url.URL) {
	p := u.EscapedPath()
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	if e := c.checkDownloadPath(p); e != nil || !c.downloadLimit.allow() {
		return
	}
	release, ok := c.downloadSlot.take()
	if !ok {
		return
	}
	defer release()
	name := SanitizeFilename(u.Path[strings.LastIndex(u.Path, "/")+1:])
	dest, ok := c.m.native.SavePanel(name, c.Info().Name)
	if !ok {
		return
	}
	if e := c.writeDownload(context.Background(), dest, p, nil); e != nil {
		c.m.native.Alert("Download failed", e.message)
	}
}
