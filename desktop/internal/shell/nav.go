package shell

import (
	"log"
	"net/http"
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
	// Method is the HTTP method of the request that led here; empty means GET.
	Method string
	// Filename is the link's download attribute, when WebKit reports one.
	Filename string
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
			conn.startProxiedDownload(u, req)
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
			// An API path is JSON, not a page: a main-frame navigation into it
			// (the 404 body that would replace the app) goes the download way.
			if conn != nil && req.MainFrame && IsAPIPath(u.Path) {
				conn.startProxiedDownload(u, req)
				return NavCancel
			}
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

// startProxiedDownload saves a same-connection URL the page navigated to, or
// says why it won't. Only a GET is saved: the response to a form POST can't be
// replayed as a request the page never made.
func (c *Conn) startProxiedDownload(u *url.URL, req NavRequest) {
	if req.Method != "" && !strings.EqualFold(req.Method, http.MethodGet) {
		c.say("The app saves downloads that start from a link, not from a form submission, because it would have to send the request again.")
		return
	}
	c.m.background(func() { c.saveProxiedURL(u, req.Filename) })
}

// saveProxiedURL fetches a same-connection URL with the connection's bearer
// and, once the instance has answered 200, shows the save panel. Every refusal
// is shown to the person: a click that does nothing looks like a broken app.
func (c *Conn) saveProxiedURL(u *url.URL, name string) {
	p := u.EscapedPath()
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	say := c.say
	if e := c.checkDownloadPath(p); e != nil {
		say(e.message)
		return
	}
	if !c.downloadLimit.allow() {
		say("Too many downloads at once; try again shortly.")
		return
	}
	release, ok := c.downloadSlot.take()
	if !ok {
		say("Another download is already waiting for a place to be saved.")
		return
	}
	defer release()
	fallback := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if e := c.download(c.life(), p, nil, name, fallback, ""); e != nil && e.code != codeCancelled {
		say(e.message)
	}
}

// say shows why a download was not saved. A page can trigger refusals in a
// loop, so the alerts themselves are rate limited.
func (c *Conn) say(message string) {
	if c.alertLimit.allow() {
		c.m.native.Alert("Download not saved", message)
	}
}
