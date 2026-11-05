//go:build android && isorig

package main

/*
#cgo LDFLAGS: -llog
#include <android/log.h>
#include <stdlib.h>
static void iso_log(const char* m) { __android_log_write(ANDROID_LOG_INFO, "IsoGo", m); }
*/
import "C"

import (
	"log"
	"unsafe"
)

type logcat struct{}

func (logcat) Write(p []byte) (int, error) {
	c := C.CString(string(p))
	defer C.free(unsafe.Pointer(c))
	C.iso_log(c)
	return len(p), nil
}

// Named to sort after log_android.go, so this init runs later: the rig reads everything through `adb logcat`.
func init() { log.SetOutput(logcat{}) }
