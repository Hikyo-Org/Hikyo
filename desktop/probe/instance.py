import http.server, json, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith('/api/v1/meta'):
            b=json.dumps({"server_version":"1.1.0","api_revision":20,"protocol_capabilities":["desktop-app/1"]}).encode()
            self.send_response(200); self.send_header('Content-Type','application/json')
        elif self.path.startswith('/api/v1/export'):
            b=b"a,b\n1,2\n"; self.send_response(200); self.send_header('Content-Type','text/csv'); self.send_header('Content-Disposition','attachment; filename="audit-x.csv"')
        elif self.path.startswith('/api/'):
            b=json.dumps({"error":"not_found"}).encode(); self.send_response(404); self.send_header('Content-Type','application/json')
        else:
            b=b"<html><body><h1>probe page</h1><a id=l href='/api/v1/nothing'>x</a></body></html>"; self.send_response(200); self.send_header('Content-Type','text/html')
        self.send_header('Content-Length',str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self,*a): sys.stderr.write("%s %s auth=%s\n"%(self.command,self.path,self.headers.get('Authorization','-')[:12]))
http.server.ThreadingHTTPServer(('127.0.0.1',18765),H).serve_forever()
