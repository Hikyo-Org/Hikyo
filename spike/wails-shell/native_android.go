//go:build android

package main

/*
#include <jni.h>
#include <stdlib.h>
static const char* jstr(JNIEnv* e, jstring s) { return (*e)->GetStringUTFChars(e, s, NULL); }
static void jrel(JNIEnv* e, jstring s, const char* c) { (*e)->ReleaseStringUTFChars(e, s, c); }
static jstring jnew(JNIEnv* e, const char* c) { return (*e)->NewStringUTF(e, c); }
static jsize jlen(JNIEnv* e, jarray a) { return (*e)->GetArrayLength(e, a); }
static void jget(JNIEnv* e, jbyteArray a, jsize n, void* buf) { (*e)->GetByteArrayRegion(e, a, 0, n, (jbyte*)buf); }
static void jset(JNIEnv* e, jbyteArray a, jsize n, void* buf) { (*e)->SetByteArrayRegion(e, a, 0, n, (jbyte*)buf); }
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// The Java host's SpikeBridge calls these four functions instead of Wails' nativeServeAsset, which only does GET with no body and buffers the reply.
// A request is served by the shell handler on a goroutine whose writes go into a pipe that Java reads as they come.

type nativeStream struct {
	headReady chan struct{}
	header    http.Header
	status    int
	pr        *io.PipeReader
	pw        *io.PipeWriter
	cancel    context.CancelFunc
	wroteHead bool
}

func (s *nativeStream) Header() http.Header { return s.header }
func (s *nativeStream) WriteHeader(code int) {
	if !s.wroteHead {
		s.wroteHead, s.status = true, code
		close(s.headReady)
	}
}
func (s *nativeStream) Write(p []byte) (int, error) {
	s.WriteHeader(http.StatusOK)
	return s.pw.Write(p)
}
func (s *nativeStream) Flush() {} // the pipe is unbuffered, so every Write is already delivered to the reader

var (
	streams  sync.Map // int64 -> *nativeStream
	streamID atomic.Int64
)

func goString(env *C.JNIEnv, s C.jstring) string {
	c := C.jstr(env, s)
	defer C.jrel(env, s, c)
	return C.GoString(c)
}

//export Java_com_wails_app_SpikeBridge_nativeBegin
func Java_com_wails_app_SpikeBridge_nativeBegin(env *C.JNIEnv, cls C.jclass, jmethod, jurl, jheaders C.jstring, jbody C.jbyteArray) C.jlong {
	select {
	case <-nativeReady:
	case <-time.After(10 * time.Second):
		log.Print("native: shell not ready, serving anyway")
	}
	method, rawURL, rawHeaders := goString(env, jmethod), goString(env, jurl), goString(env, jheaders)
	var body []byte
	if jbody != 0 {
		if n := C.jlen(env, C.jarray(jbody)); n > 0 {
			body = make([]byte, n)
			C.jget(env, jbody, n, unsafe.Pointer(&body[0]))
		}
	}
	var pairs [][2]string
	if err := json.Unmarshal([]byte(rawHeaders), &pairs); err != nil {
		log.Printf("native: bad headers: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	u, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("native: bad url %q", rawURL)
	}
	req, _ := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if len(body) == 0 {
		req.Body = http.NoBody
	}
	for _, p := range pairs {
		req.Header.Add(p[0], p[1])
	}
	req.Header.Del("Accept-Encoding") // the proxy's transport asks for gzip itself and decodes it; a gzip body would reach the page undecoded
	req.Host, req.RemoteAddr, req.RequestURI = u.Host, "127.0.0.1:0", u.RequestURI()
	// shortcut: a WebView request carries no window identity, so the id is the one registered for this host and the shell's window check only proves the host is known; one WebView cannot be held to one connection here.
	for id, host := range nativeWindows {
		if host == u.Host {
			req.Header.Set("x-wails-window-id", id)
		}
	}
	pr, pw := io.Pipe()
	s := &nativeStream{headReady: make(chan struct{}), header: http.Header{}, pr: pr, pw: pw, cancel: cancel}
	id := streamID.Add(1)
	streams.Store(id, s)
	go func() {
		defer pw.Close()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("native: handler panic: %v", r)
				s.WriteHeader(http.StatusBadGateway)
			}
		}()
		nativeHandler.ServeHTTP(s, req)
		s.WriteHeader(http.StatusOK)
	}()
	return C.jlong(id)
}

func lookup(id C.jlong) *nativeStream {
	v, ok := streams.Load(int64(id))
	if !ok {
		return nil
	}
	return v.(*nativeStream)
}

//export Java_com_wails_app_SpikeBridge_nativeHead
func Java_com_wails_app_SpikeBridge_nativeHead(env *C.JNIEnv, cls C.jclass, id C.jlong) C.jstring {
	s := lookup(id)
	if s == nil {
		return C.jnew(env, C.CString(`{"status":502,"headers":[]}`))
	}
	<-s.headReady
	var hs [][2]string
	for k, vs := range s.header {
		for _, v := range vs {
			hs = append(hs, [2]string{k, v})
		}
	}
	out, _ := json.Marshal(map[string]any{"status": s.status, "headers": hs, "location": s.header.Get("Location")})
	c := C.CString(string(out))
	defer C.free(unsafe.Pointer(c))
	return C.jnew(env, c)
}

//export Java_com_wails_app_SpikeBridge_nativeRead
func Java_com_wails_app_SpikeBridge_nativeRead(env *C.JNIEnv, cls C.jclass, id C.jlong, jbuf C.jbyteArray) C.jint {
	s := lookup(id)
	if s == nil {
		return -1
	}
	buf := make([]byte, C.jlen(env, C.jarray(jbuf)))
	n, err := s.pr.Read(buf)
	if n > 0 {
		C.jset(env, jbuf, C.jsize(n), unsafe.Pointer(&buf[0]))
		return C.jint(n)
	}
	if err != nil {
		return -1
	}
	return 0
}

//export Java_com_wails_app_SpikeBridge_nativeClose
func Java_com_wails_app_SpikeBridge_nativeClose(env *C.JNIEnv, cls C.jclass, id C.jlong) {
	if v, ok := streams.LoadAndDelete(int64(id)); ok {
		s := v.(*nativeStream)
		s.cancel()
		s.pr.Close()
	}
}
