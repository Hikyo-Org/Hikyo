//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Cocoa -framework WebKit -mmacosx-version-min=13.0
#include <stdint.h>
#include <stdlib.h>
int hikyoInstallDelegates(void);
int hikyoHardenWindow(void *nswindow);
int hikyoConfirm(const char *title, const char *message, const char *label);
void hikyoAlert(const char *title, const char *message);
char *hikyoSavePanel(const char *name, const char *source, const char *mime, int *replace);
int hikyoAppActive(void);
void hikyoShowCode(long id, const char *url, const char *code);
void hikyoDismissCode(long id);
long hikyoPasteboardCount(void);
long hikyoPasteboardCountAtActivation(void);
void hikyoClearPasteboardIf(long count);
int hikyoWipeStorage(const char *host);
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
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
// closing is what CloseWindow waits for.
type closing struct {
	gone chan struct{}
	id   uint
}

type macNative struct {
	app     *application.App
	manager *shell.Manager

	mu      sync.Mutex
	windows map[string]*application.WebviewWindow
	// opening holds a host while its window is being made, so two opens of one
	// host can't both create one. It is separate from mu, which the window
	// callbacks take during creation. Close waits on it.
	opening map[string]chan struct{}
	// closed holds, per host, the channel closed by the window's closing handler
	// and the window id; CloseWindow waits on both.
	closed map[string]closing
}

var _ shell.Native = (*macNative)(nil)

// the one native instance the exported callbacks reach.
var theNative atomic.Pointer[macNative]

func newMacNative() *macNative {
	n := &macNative{windows: map[string]*application.WebviewWindow{}, opening: map[string]chan struct{}{}, closed: map[string]closing{}}
	theNative.Store(n)
	return n
}

func (n *macNative) OpenWindow(host, title string, bind func(string)) error {
	if !delegatesInstalled {
		return errors.New("the native window handlers are missing; refusing to open a window")
	}
	release := n.reserve(host)
	defer release()
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
		n.manager.UnbindWindow(id)
		w.Close()
		return errors.New("could not harden the window's web view; refusing to open it")
	}
	gone := make(chan struct{})
	n.mu.Lock()
	n.windows[host] = w
	n.closed[host] = closing{gone: gone, id: w.ID()}
	n.mu.Unlock()
	var once sync.Once
	// Wails' own listener runs concurrently with this one, so this only says the
	// close was requested; CloseWindow also waits for Wails to drop the window,
	// which it does after the native close has run.
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		n.mu.Lock()
		if n.windows[host] == w {
			delete(n.windows, host)
		}
		n.mu.Unlock()
		n.manager.UnbindWindow(id)
		pendingDownloads.Drop(host)
		once.Do(func() { close(gone) })
	})
	installProbe(w, host)
	w.SetURL(shell.PageScheme + "://" + host + "/")
	return nil
}

// reserve waits until no other open or close of host is running, then holds
// the host until release is called.
func (n *macNative) reserve(host string) (release func()) {
	for {
		n.mu.Lock()
		wait, busy := n.opening[host]
		if !busy {
			done := make(chan struct{})
			n.opening[host] = done
			n.mu.Unlock()
			return func() {
				n.mu.Lock()
				delete(n.opening, host)
				n.mu.Unlock()
				close(done)
			}
		}
		n.mu.Unlock()
		<-wait
	}
}

func (n *macNative) CloseWindow(host string) error {
	// An open still creating this host's window finishes first, so the close
	// below sees it and no window outlives the removal. The host stays reserved
	// until the window's teardown has run, so a wipe or a reopen can't overlap
	// a renderer that is still alive.
	defer n.reserve(host)()
	n.mu.Lock()
	w, c := n.windows[host], n.closed[host]
	n.mu.Unlock()
	if c.gone == nil {
		return nil
	}
	if w != nil {
		w.Close() // queues WindowClosing; it does not wait for it
	}
	// The entries stay until the teardown is seen, so a retry after a timeout
	// waits again instead of finding nothing to wait for.
	deadline := time.After(10 * time.Second)
	select {
	case <-c.gone:
	case <-deadline:
		return errors.New("the window did not finish closing")
	}
	for {
		if _, exists := n.app.Window.GetByID(c.id); !exists {
			break
		}
		select {
		case <-deadline:
			return errors.New("the window did not finish closing")
		case <-time.After(20 * time.Millisecond):
		}
	}
	n.mu.Lock()
	delete(n.windows, host)
	delete(n.closed, host)
	n.mu.Unlock()
	return nil
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

func (n *macNative) SavePanel(name, source, mimeType string) (string, bool, bool) {
	cName, cSource, cMIME := C.CString(name), C.CString(source), C.CString(mimeType)
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cSource))
	defer C.free(unsafe.Pointer(cMIME))
	var replace C.int
	chosen := C.hikyoSavePanel(cName, cSource, cMIME, &replace)
	if chosen == nil {
		return "", false, false
	}
	defer C.free(unsafe.Pointer(chosen))
	return C.GoString(chosen), replace == 1, true
}

var (
	codeSeq     atomic.Int64
	codeCancels sync.Map // id -> func()
	codeOpens   sync.Map // id -> func()
)

func (n *macNative) ShowDeviceCode(url, code string, cancel, open func()) func() {
	id := codeSeq.Add(1)
	codeCancels.Store(id, cancel)
	codeOpens.Store(id, open)
	u, c := C.CString(url), C.CString(code)
	defer C.free(unsafe.Pointer(u))
	defer C.free(unsafe.Pointer(c))
	C.hikyoShowCode(C.long(id), u, c)
	return func() {
		codeCancels.Delete(id)
		codeOpens.Delete(id)
		C.hikyoDismissCode(C.long(id))
	}
}

func (n *macNative) AppActive() bool        { return C.hikyoAppActive() == 1 }
func (n *macNative) PasteboardCount() int64 { return int64(C.hikyoPasteboardCount()) }
func (n *macNative) PasteboardCountAtActivation() int64 {
	return int64(C.hikyoPasteboardCountAtActivation())
}
func (n *macNative) ClearPasteboardIf(count int64) { C.hikyoClearPasteboardIf(C.long(count)) }

// ---- callbacks from Objective-C ----

//export hikyoNavigate
func hikyoNavigate(pageHost, rawURL *C.char, flags C.int, method, filename *C.char) C.int {
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
		Method: C.GoString(method), Filename: C.GoString(filename),
	})
	switch action {
	case shell.NavAllow:
		return 1
	case shell.NavDownload:
		return 2
	}
	return 0
}

// pendingDownloads maps a WKDownload to where it is writing and where the
// file goes when it finishes. Concurrent downloads never share an entry.
var pendingDownloads = shell.NewPendingDownloads()

//export hikyoDownloadDestination
func hikyoDownloadDestination(key C.uintptr_t, pageHost, suggested *C.char) *C.char {
	n := theNative.Load()
	host := C.GoString(pageHost)
	if n == nil || n.manager == nil || (host != shell.AppHost && n.manager.Conn(host) == nil) {
		return nil
	}
	source := "a connection"
	if c := n.manager.Conn(host); c != nil {
		source = c.Info().Name
	}
	final, replace, ok := n.SavePanel(shell.SanitizeFilename(C.GoString(suggested)), source, "")
	if !ok {
		return nil
	}
	// WebKit writes to a fresh file beside the chosen name, so a failed or
	// cancelled download never touches an existing file and the finished one
	// is quarantined before it takes its final name.
	f, err := os.CreateTemp(filepath.Dir(final), ".hikyo-download-*")
	if err != nil {
		log.Printf("download: %v", err)
		return nil
	}
	temp := f.Name()
	_ = f.Close()
	_ = os.Remove(temp) // WKDownload needs a destination that does not exist
	pendingDownloads.Add(uintptr(key), host, temp, final, replace)
	return C.CString(temp) // freed by the caller
}

//export hikyoDownloadFinished
func hikyoDownloadFinished(key C.uintptr_t) {
	temp, final, replace, ok := pendingDownloads.Take(uintptr(key))
	if !ok {
		// The entry expired or its window closed, and its file was removed.
		failDownload("The download took too long or its window was closed, so it was not saved.")
		return
	}
	// Off the main thread: on a volume without hard links PublishDownload copies
	// the whole file, and the alerts post themselves to the main thread.
	go finishDownload(temp, final, replace)
}

func finishDownload(temp, final string, replace bool) {
	if err := shell.Quarantine(temp); err != nil {
		log.Printf("download: could not mark the file as downloaded: %v", err)
		_ = os.Remove(temp)
		failDownload("The file could not be marked as downloaded.")
		return
	}
	saved, err := shell.PublishDownload(temp, final, replace)
	if err != nil {
		log.Printf("download: could not save %s: %v", final, err)
		_ = os.Remove(temp)
		failDownload("The file could not be saved there.")
		return
	}
	if saved != final {
		if n := theNative.Load(); n != nil {
			n.Alert("Download saved under another name", "A file with that name appeared while the download ran, so it was saved as "+filepath.Base(saved)+".")
		}
	}
}

func failDownload(message string) {
	if n := theNative.Load(); n != nil {
		n.Alert("Download failed", message)
	}
}

//export hikyoDownloadFailed
func hikyoDownloadFailed(key C.uintptr_t, message *C.char) {
	if temp, _, _, ok := pendingDownloads.Take(uintptr(key)); ok {
		_ = os.Remove(temp)
	}
	if n := theNative.Load(); n != nil {
		n.Alert("Download failed", fmt.Sprint(C.GoString(message)))
	}
}

//export hikyoCodeOpenBrowser
func hikyoCodeOpenBrowser(id C.long) {
	if open, ok := codeOpens.Load(int64(id)); ok {
		go open.(func())() // off the main thread: the dialog must stay responsive
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
