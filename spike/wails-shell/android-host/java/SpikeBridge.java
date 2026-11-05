package com.wails.app;

import android.net.Uri;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.IOException;
import java.io.InputStream;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Carries an intercepted WebView request to the Go proxy with its method, query, headers and body,
 * and returns the response with its status, headers and a body that is read as Go writes it.
 * Bodies cannot arrive through shouldInterceptRequest, so spike-shim.js stashes them first (see stash()).
 */
final class SpikeBridge {
    private static final String TAG = "SpikeBridge";
    private static final int MAX_BODY = 64 << 20;
    private static final int MAX_BUFFERED = 8 << 20; // the SDK's own ceiling for a data response
    private static final long MAX_STASHED = 128L << 20;
    private static final long TTL_MS = 60_000;

    private static native long nativeBegin(String method, String url, String headersJson, byte[] body);
    private static native String nativeHead(long id);
    private static native int nativeRead(long id, byte[] buf);
    private static native void nativeClose(long id);

    private static final class Stashed {
        final String origin;
        final byte[] body;
        Stashed(String origin, byte[] body) { this.origin = origin; this.body = body; }
    }

    private static final Map<String, Stashed> stashed = new ConcurrentHashMap<>();
    private static final Handler main = new Handler(Looper.getMainLooper());
    private static long stashedBytes = 0;

    /** Called by the WebMessageListener: the first 36 bytes are the id, the rest is the body. Returns the id when kept. */
    static synchronized String stash(String sourceOrigin, byte[] msg) {
        if (msg.length < 36 || msg.length - 36 > MAX_BODY || stashedBytes + msg.length > MAX_STASHED) return null;
        final String id = new String(msg, 0, 36, java.nio.charset.StandardCharsets.US_ASCII);
        byte[] body = new byte[msg.length - 36];
        System.arraycopy(msg, 36, body, 0, body.length);
        stashed.put(id, new Stashed(sourceOrigin, body));
        stashedBytes += body.length;
        main.postDelayed(() -> take(id, null), TTL_MS);
        return id;
    }

    private static synchronized Stashed take(String id, String origin) {
        Stashed s = stashed.get(id);
        if (s == null || (origin != null && !s.origin.equals(origin))) return null;
        stashed.remove(id);
        stashedBytes -= s.body.length;
        return s;
    }

    static WebResourceResponse fetch(WebResourceRequest request) {
        try {
            String method = request.getMethod();
            Map<String, String> in = request.getRequestHeaders();
            JSONArray headers = new JSONArray();
            byte[] body = null;
            for (Map.Entry<String, String> h : in.entrySet()) {
                if (h.getKey().equalsIgnoreCase("X-Spike-Body")) {
                    Stashed s = take(h.getValue(), in.get("Origin"));
                    if (s == null) return error(400, "unknown or foreign body id");
                    body = s.body;
                } else {
                    headers.put(new JSONArray().put(h.getKey()).put(h.getValue()));
                }
            }
            Uri u = request.getUrl();
            long id = nativeBegin(method, u.toString(), headers.toString(), body);
            JSONObject head = new JSONObject(nativeHead(id));
            int status = head.getInt("status");
            Map<String, String> out = new HashMap<>();
            String ctype = "application/octet-stream";
            long declared = -1;
            JSONArray hs = head.getJSONArray("headers");
            for (int i = 0; i < hs.length(); i++) {
                String k = hs.getJSONArray(i).getString(0), v = hs.getJSONArray(i).getString(1);
                if (k.equalsIgnoreCase("Content-Type")) ctype = v;
                // The WebView derives Content-Length from the stream's available() and adds it itself; a copy of Go's would make it read "0, 207".
                else if (k.equalsIgnoreCase("Content-Length")) declared = parseLength(v);
                else if (!k.equalsIgnoreCase("Transfer-Encoding")) out.merge(k, v, (a, b) -> a + ", " + b);
            }
            if (status >= 300 && status < 400) {
                // WebResourceResponse rejects 3xx, so a redirect cannot be passed on.
                nativeClose(id);
                out.clear();
                out.put("X-Spike-Redirect", status + " " + head.optString("location"));
                return new WebResourceResponse("text/plain", "UTF-8", 502, "Redirect", out, new java.io.ByteArrayInputStream(new byte[0]));
            }
            String mime = ctype.split(";")[0].trim();
            String charset = null;
            for (String p : ctype.split(";")) if (p.trim().toLowerCase().startsWith("charset=")) charset = p.trim().substring(8);
            InputStream stream = new GoStream(id);
            if (!mime.equalsIgnoreCase("text/event-stream")) stream = sized(stream, declared);
            return new WebResourceResponse(mime, charset, status, status < 400 ? "OK" : "Error", out, stream);
        } catch (Exception e) {
            Log.e(TAG, "fetch failed", e);
            return error(502, e.toString());
        }
    }

    private static long parseLength(String v) {
        try { return Long.parseLong(v.trim()); } catch (NumberFormatException e) { return -1; }
    }

    /**
     * The WebView reports available() as the response's Content-Length, and GoStream cannot know it. A body that ends within
     * MAX_BUFFERED is read whole, so the page sees its exact length. A longer one streams on, reporting the length Go declared
     * (or 0 when it sent none, i.e. chunked), so its length is only as good as the instance's. Never used for event streams.
     */
    private static InputStream sized(InputStream in, long declared) throws IOException {
        java.io.ByteArrayOutputStream head = new java.io.ByteArrayOutputStream();
        byte[] buf = new byte[65536];
        for (int n; head.size() <= MAX_BUFFERED && (n = in.read(buf, 0, buf.length)) >= 0; ) head.write(buf, 0, n);
        byte[] prefix = head.toByteArray();
        if (prefix.length <= MAX_BUFFERED) { in.close(); return new java.io.ByteArrayInputStream(prefix); }
        return new Prefixed(prefix, in, declared);
    }

    /** A buffered prefix followed by the rest of a stream. */
    private static final class Prefixed extends InputStream {
        private final InputStream prefix, rest;
        private final long declared;
        private long served;
        Prefixed(byte[] prefix, InputStream rest, long declared) {
            this.prefix = new java.io.ByteArrayInputStream(prefix); this.rest = rest; this.declared = declared;
        }
        @Override public int read() throws IOException { byte[] one = new byte[1]; return read(one, 0, 1) < 0 ? -1 : one[0] & 0xff; }
        @Override public int read(byte[] b, int off, int len) throws IOException {
            int n = prefix.read(b, off, len);
            if (n < 0) n = rest.read(b, off, len);
            if (n > 0) served += n;
            return n;
        }
        @Override public int available() { return declared < 0 ? 0 : (int) Math.min(Integer.MAX_VALUE, Math.max(0, declared - served)); }
        @Override public void close() throws IOException { rest.close(); }
    }

    /** Saves a GET of a wails URL to a file through the proxy and returns its size. */
    static long download(String url, java.io.File out) throws Exception {
        long id = nativeBegin("GET", url, "[]", null);
        try (InputStream in = new GoStream(id); java.io.FileOutputStream fos = new java.io.FileOutputStream(out)) {
            JSONObject head = new JSONObject(nativeHead(id));
            if (head.getInt("status") != 200) throw new IOException("status " + head.getInt("status"));
            byte[] buf = new byte[65536];
            long total = 0;
            for (int n; (n = in.read(buf, 0, buf.length)) > 0; ) { fos.write(buf, 0, n); total += n; }
            return total;
        }
    }

    private static WebResourceResponse error(int status, String msg) {
        return new WebResourceResponse("text/plain", "UTF-8", status, "Error", new HashMap<>(),
                new java.io.ByteArrayInputStream(msg.getBytes(java.nio.charset.StandardCharsets.UTF_8)));
    }

    /** Reads what Go has written so far and blocks for more, so the page sees chunks as they are flushed. */
    private static final class GoStream extends InputStream {
        private final long id;
        private boolean closed;
        GoStream(long id) { this.id = id; }

        @Override public int read() throws IOException {
            byte[] one = new byte[1];
            int n = read(one, 0, 1);
            return n < 0 ? -1 : one[0] & 0xff;
        }

        @Override public int read(byte[] b, int off, int len) throws IOException {
            if (closed) return -1;
            if (off != 0 || len != b.length) {
                byte[] tmp = new byte[len];
                int n = nativeRead(id, tmp);
                if (n > 0) System.arraycopy(tmp, 0, b, off, n);
                return n;
            }
            return nativeRead(id, b);
        }

        @Override public void close() {
            if (!closed) { closed = true; nativeClose(id); }
        }
    }
}
