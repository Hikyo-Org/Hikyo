//go:build ios

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework WebKit -framework AuthenticationServices
#include <stdlib.h>
void spikeReplacePolicy(void);
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

func replaceNavigationPolicy() { C.spikeReplacePolicy() }

func asesStart(u string) {
	c := C.CString(u)
	defer C.free(unsafe.Pointer(c))
	C.spikeASESStart(c)
}

func asesCancel() { C.spikeASESCancel() }
