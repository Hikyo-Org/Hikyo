#!/usr/bin/env python3
"""TLS health only, no audit content. Separate watchdog must observe failures."""

import argparse
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path
import ssl
import time


def healthy(path):
    try:
        result = json.loads(Path(path).read_bytes())
        age = (time.time_ns() - result["checked_ns"]) / 1e9
        return result["healthy"] is True and 0 <= age <= 120
    except (OSError, ValueError, KeyError, TypeError):
        return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("snapshot")
    parser.add_argument("certificate")
    parser.add_argument("key")
    parser.add_argument("--bind", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8449)
    args = parser.parse_args()

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            ok = self.path == "/health" and healthy(args.snapshot)
            self.send_response(200 if ok else 503)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", "5")
            self.end_headers()
            self.wfile.write(b"PASS\n" if ok else b"HOLD\n")

        def log_message(self, _format, *_args):
            pass

    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(args.certificate, args.key)

    class TLSServer(HTTPServer):
        def get_request(self):
            connection, address = super().get_request()
            connection.settimeout(5)
            try:
                return context.wrap_socket(connection, server_side=True), address
            except (OSError, ssl.SSLError):
                connection.close()
                raise

    server = TLSServer((args.bind, args.port), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
