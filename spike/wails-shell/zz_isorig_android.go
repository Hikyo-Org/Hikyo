//go:build android && isorig

package main

/*
#cgo LDFLAGS: -llog
#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
static jstring iso_jnew(JNIEnv* e, const char* c) { return (*e)->NewStringUTF(e, c); }
static void iso_log(const char* m) { __android_log_write(ANDROID_LOG_INFO, "IsoGo", m); }
*/
import "C"

import (
	"log"
	"unsafe"
)

// The rig adds and removes connections at runtime, which is what a connection manager will do. Java learns the new host from the return value.
//
//export Java_com_wails_app_Rig_nativeAdd
func Java_com_wails_app_Rig_nativeAdd(env *C.JNIEnv, cls C.jclass, jname C.jstring) C.jstring {
	name := goString(env, jname)
	s, ok := loadSessions()[name]
	host := ""
	if ok {
		s.Host = "" // a new connection gets a new host, as D2 has it
		var err error
		if host, err = openConnection(name, s, ""); err != nil {
			log.Printf("rig: add %s: %v", name, err)
		}
	}
	c := C.CString(host)
	defer C.free(unsafe.Pointer(c))
	return C.iso_jnew(env, c)
}

//export Java_com_wails_app_Rig_nativeRemove
func Java_com_wails_app_Rig_nativeRemove(env *C.JNIEnv, cls C.jclass, jhost C.jstring) {
	connections.remove(goString(env, jhost))
}

type logcat struct{}

func (logcat) Write(p []byte) (int, error) {
	c := C.CString(string(p))
	defer C.free(unsafe.Pointer(c))
	C.iso_log(c)
	return len(p), nil
}

// Named to sort after log_android.go, so this init runs later: the rig reads everything through `adb logcat`.
func init() { log.SetOutput(logcat{}) }
