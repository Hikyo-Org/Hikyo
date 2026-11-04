package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
int spikeInstallDelegates(void);
*/
import "C"

import (
	"log"
)

// Wails v3.1.0 gives its WKWebView delegate no navigation policy, download or popup
// handling. WebKit caches which methods a delegate has when it is set, so the methods
// are added to Wails' class here, before app.Run creates any window.
func init() {
	log.Printf("native handlers installed: %d of 5", C.spikeInstallDelegates())
}

//export spikeOpenURL
func spikeOpenURL(raw *C.char) {
	if err := openBrowser(C.GoString(raw)); err != nil {
		log.Printf("refused external open: %v", err)
	}
}

//export spikeDownloadPath
func spikeDownloadPath(suggested *C.char) *C.char {
	p := downloadPath(C.GoString(suggested))
	log.Printf("download destination %s", p)
	return C.CString(p) // freed by the caller
}

//export spikeSaveURL
func spikeSaveURL(raw *C.char) {
	u := C.GoString(raw)
	go func() {
		p, n, err := saveThroughProxy(u)
		if err != nil {
			log.Printf("proxy download of %s failed: %v", u, err)
			return
		}
		log.Printf("proxy download saved %s (%d bytes)", p, n)
	}()
}

//export spikeLog
func spikeLog(msg *C.char) {
	log.Printf("native: %s", C.GoString(msg))
}
