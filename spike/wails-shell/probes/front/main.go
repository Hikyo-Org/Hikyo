// Test front for the mobile hosts: serves a few measuring routes under /__t/ and proxies everything else to a disposable
// instance, so the real UI and the test routes share one origin. Loopback only. Throwaway, like the rest of the spike.
//
//	go run ./probes/front -listen 127.0.0.1:18110 -instance http://127.0.0.1:18100
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func num(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18110", "front address")
	instance := flag.String("instance", "http://127.0.0.1:18100", "the instance to proxy to")
	flag.Parse()
	target, err := url.Parse(*instance)
	if err != nil {
		log.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { pr.SetURL(target) }, FlushInterval: -1}

	mux := http.NewServeMux()
	// /__t/sized?n=BYTES&mode=cl|chunked : a JSON body of exactly n bytes, with or without a declared length.
	mux.HandleFunc("/__t/sized", func(w http.ResponseWriter, r *http.Request) {
		n := max(num(r, "n", 100), 10)
		body := `{"d":"` + strings.Repeat("x", n-8) + `"}`
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("mode") == "cl" {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			io.WriteString(w, body)
			return
		}
		// Chunked: write in two parts with a flush between, which stops Go from adding a Content-Length.
		io.WriteString(w, body[:len(body)/2])
		w.(http.Flusher).Flush()
		io.WriteString(w, body[len(body)/2:])
	})
	// /__t/echo : the request as the instance saw it, with the body's length and SHA-256.
	mux.HandleFunc("/__t/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method, "query": r.URL.RawQuery, "len": len(body), "sha256": hex.EncodeToString(sum[:]),
			"contentType": r.Header.Get("Content-Type"), "authorization": r.Header.Get("Authorization") != "", "cookie": r.Header.Get("Cookie") != "",
		})
	})
	// /__t/status?code=N : that status, with a Location for a 3xx.
	mux.HandleFunc("/__t/status", func(w http.ResponseWriter, r *http.Request) {
		code := num(r, "code", 200)
		w.Header().Set("Content-Type", "text/plain")
		if code >= 300 && code < 400 {
			w.Header().Set("Location", "/__t/echo")
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, "status %d", code)
	})
	// /__t/sse?n=EVENTS&gap=MS : n events gap ms apart, each stamped with the milliseconds since the stream began.
	mux.HandleFunc("/__t/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		start := time.Now()
		for i := 0; i < num(r, "n", 5); i++ {
			if i > 0 {
				time.Sleep(time.Duration(num(r, "gap", 800)) * time.Millisecond)
			}
			fmt.Fprintf(w, "data: {\"i\":%d,\"sent\":%d}\n\n", i, time.Since(start).Milliseconds())
			w.(http.Flusher).Flush()
		}
	})
	// /__t/download?mb=N&mode=cl|chunked : N MiB of bytes as an attachment, with or without a declared length. Byte i of a chunk is i mod 256, so a download can be checked by size.
	mux.HandleFunc("/__t/download", func(w http.ResponseWriter, r *http.Request) {
		size := num(r, "mb", 20) << 20
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="spike.bin"`)
		if r.URL.Query().Get("mode") == "cl" {
			w.Header().Set("Content-Length", strconv.Itoa(size))
		}
		chunk := make([]byte, 64<<10)
		for i := range chunk {
			chunk[i] = byte(i)
		}
		for sent := 0; sent < size; sent += len(chunk) {
			w.Write(chunk[:min(len(chunk), size-sent)])
		}
	})
	mux.Handle("/", proxy)
	log.Printf("front on %s -> %s", *listen, *instance)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
