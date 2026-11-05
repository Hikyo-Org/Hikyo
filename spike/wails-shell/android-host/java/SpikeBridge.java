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
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
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
    private static final long MAX_STASHED = 128L << 20;
    private static final long TTL_MS = 60_000;

    private static native long nativeBegin(String method, String url, String headersJson, byte[] body, String owner);
    static native void nativeRig(String flags);
    private static native String nativeHosts();
    private static native String nativeHead(long id);
    private static native int nativeRead(long id, byte[] buf);
    private static native void nativeClose(long id);

    private static final class Stashed {
        final String owner;
        final byte[] body;
        Stashed(String owner, byte[] body) { this.owner = owner; this.body = body; }
    }

    private static final Map<String, Stashed> stashed = new ConcurrentHashMap<>();
    private static final Handler main = new Handler(Looper.getMainLooper());
    private static final Map<String, Long> stashedBy = new java.util.HashMap<>();
    private static long stashedBytes = 0;

    /** The hosts of the connections, in order, from the Go shell. Empty when the shell is not up, which leaves no connection to show. */
    static List<String> hosts() {
        List<String> out = new ArrayList<>();
        try {
            JSONArray a = new JSONArray(nativeHosts());
            for (int i = 0; i < a.length(); i++) out.add(a.getString(i));
        } catch (Exception e) {
            Log.e(TAG, "no connection list", e);
        }
        return out;
    }

    /** A page picks its own ids, so an entry is keyed by owner and id, and a repeated id replaces the entry and gives its bytes back. */
    private static String key(String owner, String id) { return owner + "/" + id; }

    /** Called by the WebMessageListener of the view that owns the connection: the first 36 bytes are the id, the rest is the body. Returns the id when kept. */
    static synchronized String stash(String owner, byte[] msg) {
        if (owner.isEmpty() || msg.length < 36 || msg.length - 36 > MAX_BODY) return null;
        final String id = new String(msg, 0, 36, java.nio.charset.StandardCharsets.US_ASCII);
        final String k = key(owner, id);
        Stashed old = stashed.remove(k);
        if (old != null) giveBack(owner, old.body.length);
        // One connection cannot use the whole budget, so a page that floods bodies only starves itself.
        if (stashedBy.getOrDefault(owner, 0L) + msg.length > MAX_STASHED / 2 || stashedBytes + msg.length > MAX_STASHED) return null;
        byte[] body = new byte[msg.length - 36];
        System.arraycopy(msg, 36, body, 0, body.length);
        Stashed s = new Stashed(owner, body);
        stashed.put(k, s);
        stashedBy.merge(owner, (long) body.length, Long::sum);
        stashedBytes += body.length;
        main.postDelayed(() -> expire(k, s), TTL_MS);
        return id;
    }

    private static void giveBack(String owner, int n) {
        stashedBytes -= n;
        stashedBy.merge(owner, (long) -n, Long::sum);
    }

    /** A body is only handed to a request from the view that stashed it. */
    private static synchronized Stashed take(String id, String owner) {
        Stashed s = stashed.remove(key(owner, id));
        if (s != null) giveBack(owner, s.body.length);
        return s;
    }

    /** Drops an entry nobody took. It compares the entry itself, because the page may have replaced the id since. */
    private static synchronized void expire(String k, Stashed s) {
        if (stashed.remove(k, s)) giveBack(s.owner, s.body.length);
    }

    /** owner is the connection this request's page belongs to, as the host establishes it. Go refuses a request whose host differs. */
    static WebResourceResponse fetch(WebResourceRequest request, String owner) {
        try {
            String method = request.getMethod();
            Map<String, String> in = request.getRequestHeaders();
            JSONArray headers = new JSONArray();
            byte[] body = null;
            for (Map.Entry<String, String> h : in.entrySet()) {
                if (h.getKey().equalsIgnoreCase("X-Spike-Body")) {
                    Stashed s = take(h.getValue(), owner);
                    if (s == null) return error(400, "unknown or foreign body id");
                    body = s.body;
                } else {
                    headers.put(new JSONArray().put(h.getKey()).put(h.getValue()));
                }
            }
            Uri u = request.getUrl();
            long id = nativeBegin(method, u.toString(), headers.toString(), body, owner);
            JSONObject head = new JSONObject(nativeHead(id));
            int status = head.getInt("status");
            Map<String, String> out = new HashMap<>();
            String ctype = "application/octet-stream";
            JSONArray hs = head.getJSONArray("headers");
            for (int i = 0; i < hs.length(); i++) {
                String k = hs.getJSONArray(i).getString(0), v = hs.getJSONArray(i).getString(1);
                if (k.equalsIgnoreCase("Content-Type")) ctype = v;
                // Keep Content-Length: without it the WebView reports 0 to the page, and the SDK client then discards the body.
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
            return new WebResourceResponse(mime, charset, status, status < 400 ? "OK" : "Error", out, new GoStream(id));
        } catch (Exception e) {
            Log.e(TAG, "fetch failed", e);
            return error(502, e.toString());
        }
    }

    /** Saves a GET of a wails URL to a file through the proxy and returns its size. */
    static long download(String url, java.io.File out, String owner) throws Exception {
        long id = nativeBegin("GET", url, "[]", null, owner);
        try (InputStream in = new GoStream(id)) {
            JSONObject head = new JSONObject(nativeHead(id));
            if (head.getInt("status") != 200) throw new IOException("status " + head.getInt("status"));
            try (java.io.FileOutputStream fos = new java.io.FileOutputStream(out)) {
                byte[] buf = new byte[65536];
                long total = 0;
                for (int n; (n = in.read(buf, 0, buf.length)) > 0; ) { fos.write(buf, 0, n); total += n; }
                return total;
            }
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
