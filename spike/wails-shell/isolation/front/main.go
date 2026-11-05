// Scratch instance for the connection-isolation rig. It stands in for one
// connection's instance and logs what arrives: its log is the ground truth for
// "was a request sent with this connection's bearer". The bearers are fake.
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed attack.js
var attackJS []byte

const attackHTML = `<!doctype html><meta charset=utf-8><title>attack</title><body><pre id=o></pre><script>window.RIG=%s</script><script src=/attack.js></script>`

// The benign page reports what storage it can see, and whether it came back from the back/forward cache.
const benignHTML = `<!doctype html><meta charset=utf-8><title>benign</title><body><p>benign page of %s</p><script>
const seen=localStorage.getItem('who');localStorage.setItem('who',location.host);
addEventListener('pageshow',e=>fetch('/report?run=benign&v=pageshow&r='+encodeURIComponent('persisted='+e.persisted+' seen='+seen+' host='+location.host)).catch(()=>{}));
</script>`

var gif, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

const swJS = `self.addEventListener('install',e=>{self.skipWaiting();e.waitUntil(probe('install'))});
self.addEventListener('activate',e=>e.waitUntil(probe('activate')));
function probe(p){const q=new URL(location.href).searchParams;const u=q.get('victim')+'/v/sw-fetch-'+p+'?kind=img&run='+q.get('run');
return fetch(u,{mode:'no-cors'}).then(r=>fetch('/report?run='+q.get('run')+'&v=sw-fetch-'+p+'&r=resolved-'+r.type),e=>fetch('/report?run='+q.get('run')+'&v=sw-fetch-'+p+'&r=rejected-'+e)).catch(()=>{})}`

const workerJS = `const q=new URL(location.href).searchParams;const u=q.get('victim')+'/v/'+q.get('v')+'?kind=img&run='+q.get('run');
fetch(u,{mode:'no-cors'}).then(r=>'resolved-'+r.type,e=>'rejected-'+e).then(s=>fetch('/report?run='+q.get('run')+'&v='+q.get('v')+'&r='+encodeURIComponent(s)));`

type entry struct {
	TS      string            `json:"ts"`
	Front   string            `json:"front"`
	Method  string            `json:"method"`
	URI     string            `json:"uri"`
	Bearer  string            `json:"bearer"`
	Headers map[string]string `json:"headers"`
	Body    int               `json:"body_len"`
	BodySHA string            `json:"body_sha,omitempty"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18200", "listen address")
	name := flag.String("name", "front", "name in the log")
	logPath := flag.String("log", "", "JSONL log")
	attack := flag.Bool("attack", false, "serve the attack page at /")
	victimFlag := flag.String("victim", "", "origin of the victim connection, for the headers a browser acts on by itself")
	rigJSON := flag.String("rig", "{}", "JSON handed to attack.js as window.RIG: run, thennav, only, direct")
	control := flag.String("control", "", "address of the command queue the Android rig polls")
	spa := flag.String("spa", "", "directory of a built SPA to serve instead of the attack and benign pages, for memory and switching measurements")
	tlsAddr := flag.String("tls", "", "address of an HTTPS listener with a self-signed certificate, so a redirect hop can be tested where cleartext is blocked")
	flag.Parse()
	if *control != "" {
		go serveControl(*control)
	}
	f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	enc := json.NewEncoder(f)
	record := func(r *http.Request, body []byte) {
		h := map[string]string{}
		for k, vs := range r.Header {
			if k != "Authorization" {
				h[k] = strings.Join(vs, ", ")
			}
		}
		e := entry{time.Now().Format("15:04:05.000"), *name, r.Method, r.URL.RequestURI(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h, len(body), ""}
		if len(body) > 0 {
			sum := sha256.Sum256(body)
			e.BodySHA = hex.EncodeToString(sum[:4])
		}
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(e)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		record(r, body)
		q := r.URL.Query()
		// Same headers as internal/server/spa.go, so the hardened vectors see what a real instance sends.
		if q.Get("hard") == "1" {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
		}
		if *spa != "" {
			if _, err := os.Stat(*spa + r.URL.Path); err != nil || r.URL.Path == "/" {
				http.ServeFile(w, r, *spa+"/index.html")
				return
			}
			http.ServeFile(w, r, *spa+r.URL.Path)
			return
		}
		switch {
		case r.URL.Path == "/redir":
			w.Header().Set("Location", q.Get("to"))
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/attack.js":
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(attackJS)
		case r.URL.Path == "/sw.js":
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, swJS)
		case r.URL.Path == "/worker.js":
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, workerJS)
		case r.URL.Path == "/report":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/echo":
			// The bearer-echo route for the race tests: same data a same-origin caller would read.
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, *name)
		case strings.HasPrefix(r.URL.Path, "/v/"):
			if s := q.Get("status"); s == "404" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch q.Get("kind") {
			case "img":
				w.Header().Set("Content-Type", "image/gif")
				w.Write(gif)
			case "js":
				w.Header().Set("Content-Type", "text/javascript")
				io.WriteString(w, "/*ok*/")
			case "css":
				w.Header().Set("Content-Type", "text/css")
				io.WriteString(w, "/*ok*/")
			case "attach":
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", `attachment; filename="rig.bin"`)
				io.WriteString(w, *name)
			default:
				// A document: it reports where it landed and, for navigation vectors, returns to the attacker.
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				back, _ := json.Marshal(q.Get("back"))
				fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>landed</title><body>landed on %s<script>
fetch('/report?run=%s&v=%s&r='+encodeURIComponent('landed:'+location.host+' main='+(top===self))).catch(()=>{});
const b=%s;if(b)setTimeout(()=>{location.href=b},800);
</script>`, *name, q.Get("run"), strings.TrimPrefix(r.URL.Path, "/v/"), back)
			}
		case r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/attack":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if *attack || r.URL.Path == "/attack" {
				// csp-report and link-header are browser-issued requests the page never scripts.
				if vh := *victimFlag; vh != "" && q.Get("phase") != "nav" {
					var rig struct{ Run string }
					_ = json.Unmarshal([]byte(*rigJSON), &rig)
					w.Header().Set("Content-Security-Policy-Report-Only", "img-src 'none'; report-uri "+vh+"/v/csp-report?run="+rig.Run)
					w.Header().Add("Link", "<"+vh+"/v/link-header?kind=img&run="+rig.Run+">; rel=preload; as=image")
				}
				fmt.Fprintf(w, attackHTML, *rigJSON)
				return
			}
			fmt.Fprintf(w, benignHTML, *name)
		default:
			http.NotFound(w, r)
		}
	})
	if *tlsAddr != "" {
		ln, err := net.Listen("tcp", *tlsAddr)
		if err != nil {
			log.Fatal(err)
		}
		srv := httptest.NewUnstartedServer(http.DefaultServeMux)
		srv.Listener = ln
		srv.StartTLS()
	}
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// serveControl is the Android rig's control port, reached over adb reverse: the host reads its mode and polls for commands.
func serveControl(addr string) {
	var mu sync.Mutex
	var mode string
	var queue []string
	mux := http.NewServeMux()
	mux.HandleFunc("/ctl/mode", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if m := r.URL.Query().Get("set"); m != "" {
			mode = m
		}
		fmt.Fprintln(w, mode)
	})
	mux.HandleFunc("/ctl/push", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		queue = append(queue, r.URL.Query().Get("cmd"))
	})
	mux.HandleFunc("/ctl/next", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if len(queue) > 0 {
			fmt.Fprintln(w, queue[0])
			queue = queue[1:]
		} else {
			fmt.Fprintln(w)
		}
	})
	log.Fatal(http.ListenAndServe(addr, mux))
}
