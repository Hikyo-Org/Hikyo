//go:build ios

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo isonopolicy CFLAGS: -DISO_NOPOLICY
#cgo LDFLAGS: -framework UIKit -framework WebKit -framework AuthenticationServices
#include <stdlib.h>
int spikeReplacePolicy(void);
void spikeInstallSwitcher(const char *const *labels, int n);
void spikeASESStart(const char *url);
void spikeASESCancel(void);
*/
import "C"

import (
	"log"
	"unsafe"
)

//export spikeLog
func spikeLog(msg *C.char) { log.Printf("native: %s", C.GoString(msg)) }

//export spikeOpenURL
func spikeOpenURL(raw *C.char) {
	if err := openBrowser(C.GoString(raw)); err != nil {
		log.Printf("refused external open: %v", err)
	}
}

// The navigation policy is what stops script, frame and form navigation to another connection, so it is installed
// before any view exists and a missing delegate class stops the app instead of leaving it open.
func init() {
	if C.spikeReplacePolicy() == 0 {
		log.Fatal("navigation policy not installed")
	}
}

// installSwitcher puts a control over the root view that shows one connection's view at a time. Each view stays alive,
// so a page keeps its state while another connection is in front.
func installSwitcher(labels []string) {
	cs := make([]*C.char, len(labels))
	for i, l := range labels {
		cs[i] = C.CString(l)
		defer C.free(unsafe.Pointer(cs[i]))
	}
	if len(cs) > 0 {
		C.spikeInstallSwitcher(&cs[0], C.int(len(cs)))
	}
}

func asesStart(u string) {
	c := C.CString(u)
	defer C.free(unsafe.Pointer(c))
	C.spikeASESStart(c)
}

func asesCancel() { C.spikeASESCancel() }
