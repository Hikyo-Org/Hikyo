//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Cocoa -framework WebKit -mmacosx-version-min=13.0
#include <stdlib.h>
int hikyoInstallDelegates(void);
int hikyoHardenWindow(void *nswindow);
int hikyoConfirm(const char *title, const char *message, const char *label);
void hikyoAlert(const char *title, const char *message);
char *hikyoSavePanel(const char *name);
void hikyoShowCode(long id, const char *url, const char *code);
void hikyoDismissCode(long id);
long hikyoPasteboardCount(void);
void hikyoClearPasteboard(void);
int hikyoWipeStorage(const char *host);
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
)

// installedDelegates is how many native handler methods Wails' window
// delegate must have after install (ADR D14). A window is not opened without
// all of them: a missing navigation policy would let a page navigate freely.
const installedDelegates = 5

var delegatesInstalled = func() bool {
	n := int(C.hikyoInstallDelegates())
	if n != installedDelegates {
		log.Printf("native handlers installed: %d of %d; windows are disabled", n, installedDelegates)
		return false
	}
	return true
}()

// macNative is the shell.Native for macOS, plus the window registry.
type macNative struct {
	app     *application.App
	manager *shell.Manager

	mu      sync.Mutex
	windows map[string]*application.WebviewWindow
}

var _ shell.Native = (*macNative)(nil)

// the one native instance the exported callbacks reach.
var theNative atomic.Pointer[macNative]

func newMacNative() *macNative {
	n := &macNative{windows: map[string]*application.WebviewWindow{}}
	theNative.Store(n)
	return n
}

func (n *macNative) OpenWindow(host, title string, bind func(string)) error {
	if !delegatesInstalled {
		return errors.New("the native window handlers are missing; refusing to open a window")
	}
	n.mu.Lock()
	if w, ok := n.windows[host]; ok {
		n.mu.Unlock()
		w.Restore()
		w.Focus()
		return nil
	}
	n.mu.Unlock()
	// The window is created blank, bound to its host and hardened, and only
	// then sent to its page, so no request from it is ever unbound.
	w := n.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: title, Width: 1280, Height: 860, MinWidth: 800, MinHeight: 560,
		DevToolsEnabled: false, AllowSimpleEventEmit: false,
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone:    application.PermissionDeny,
			application.PermissionCamera:        application.PermissionDeny,
			application.PermissionGeolocation:   application.PermissionDeny,
			application.PermissionNotifications: application.PermissionDeny,
			application.PermissionClipboardRead: application.PermissionDeny,
		},
	})
	id := strconv.FormatUint(uint64(w.ID()), 10)
	bind(id)
	if C.hikyoHardenWindow(w.NativeWindow()) != 1 {
		w.Close()
		return errors.New("could not harden the window's web view; refusing to open it")
	}
	n.mu.Lock()
	n.windows[host] = w
	n.mu.Unlock()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		n.mu.Lock()
		if n.windows[host] == w {
			delete(n.windows, host)
		}
		n.mu.Unlock()
		n.manager.UnbindWindow(id)
	})
	installProbe(w)
	w.SetURL(shell.PageScheme + "://" + host + "/")
	return nil
}

func (n *macNative) CloseWindow(host string) {
	n.mu.Lock()
	w := n.windows[host]
	delete(n.windows, host)
	n.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (n *macNative) WipeStorage(host string) error {
	cHost := C.CString(host)
	defer C.free(unsafe.Pointer(cHost))
	if C.hikyoWipeStorage(cHost) < 0 {
		return errors.New("the web view's data store did not answer")
	}
	return nil
}

func (n *macNative) Confirm(title, message, label string) bool {
	t, m, l := C.CString(title), C.CString(message), C.CString(label)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	defer C.free(unsafe.Pointer(l))
	return C.hikyoConfirm(t, m, l) == 1
}

func (n *macNative) Alert(title, message string) {
	t, m := C.CString(title), C.CString(message)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	C.hikyoAlert(t, m)
}

func (n *macNative) SavePanel(name string) (string, bool) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	chosen := C.hikyoSavePanel(cName)
	if chosen == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(chosen))
	return C.GoString(chosen), true
}

var (
	codeSeq     atomic.Int64
	codeCancels sync.Map // id -> func()
)

func (n *macNative) ShowDeviceCode(url, code string, cancel func()) func() {
	id := codeSeq.Add(1)
	codeCancels.Store(id, cancel)
	u, c := C.CString(url), C.CString(code)
	defer C.free(unsafe.Pointer(u))
	defer C.free(unsafe.Pointer(c))
	C.hikyoShowCode(C.long(id), u, c)
	return func() {
		codeCancels.Delete(id)
		C.hikyoDismissCode(C.long(id))
	}
}

func (n *macNative) PasteboardCount() int64 { return int64(C.hikyoPasteboardCount()) }
func (n *macNative) ClearPasteboard()       { C.hikyoClearPasteboard() }

// ---- callbacks from Objective-C ----

//export hikyoNavigate
func hikyoNavigate(pageHost, rawURL *C.char, flags C.int) C.int {
	n := theNative.Load()
	if n == nil || n.manager == nil {
		return 0
	}
	host := C.GoString(pageHost)
	// A window that has not left its blank first page has no host of its own.
	if host != shell.AppHost && n.manager.Conn(host) == nil {
		host = ""
	}
	action := n.manager.Navigate(shell.NavRequest{
		PageHost: host, URL: C.GoString(rawURL),
		MainFrame: flags&1 != 0, NewWindow: flags&2 != 0, Download: flags&4 != 0, UserGesture: flags&8 != 0,
	})
	switch action {
	case shell.NavAllow:
		return 1
	case shell.NavDownload:
		return 2
	}
	return 0
}

var pendingDownload atomic.Pointer[string]

//export hikyoDownloadDestination
func hikyoDownloadDestination(pageHost, suggested *C.char) *C.char {
	n := theNative.Load()
	host := C.GoString(pageHost)
	if n == nil || n.manager == nil || (host != shell.AppHost && n.manager.Conn(host) == nil) {
		return nil
	}
	path, ok := n.SavePanel(shell.SanitizeFilename(C.GoString(suggested)))
	if !ok {
		return nil
	}
	pendingDownload.Store(&path)
	return C.CString(path) // freed by the caller
}

//export hikyoDownloadFinished
func hikyoDownloadFinished() {
	if p := pendingDownload.Swap(nil); p != nil {
		if err := shell.Quarantine(*p); err != nil {
			log.Printf("download: could not mark %s as quarantined: %v", *p, err)
		}
	}
}

//export hikyoDownloadFailed
func hikyoDownloadFailed(message *C.char) {
	pendingDownload.Store(nil)
	if n := theNative.Load(); n != nil {
		n.Alert("Download failed", fmt.Sprint(C.GoString(message)))
	}
}

//export hikyoCodeCancelled
func hikyoCodeCancelled(id C.long) {
	if cancel, ok := codeCancels.LoadAndDelete(int64(id)); ok {
		cancel.(func())()
	}
}

//export hikyoLog
func hikyoLog(msg *C.char) { log.Printf("native: %s", C.GoString(msg)) }
