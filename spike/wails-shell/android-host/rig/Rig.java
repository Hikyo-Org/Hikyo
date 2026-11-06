package com.wails.app;

import android.app.ActivityManager;
import android.content.Context;
import android.net.Uri;
import android.os.Debug;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.Log;
import android.webkit.ServiceWorkerClient;
import android.webkit.ServiceWorkerController;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebView;

import androidx.webkit.ProfileStore;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.Socket;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;

/**
 * Rig for the connection-isolation checks, built in only with ISO_TAGS=isorig. It takes no part in what the shell
 * allows: it reads a control port, runs commands, and logs. The product behaviour is all in Connections.
 *
 * Flags come from the control port's /ctl/mode, joined by '+': sw (a ServiceWorkerClient shared by every profile, the
 * gap the report warns about), swdeny (a client that refuses everything), swprofile (one client per profile, bound to
 * its connection), blocknet (setBlockNetworkLoads), csp (the proxy adds its own CSP), tlsok (accept the rig's
 * self-signed redirector on 127.0.0.1).
 * Commands from /ctl/next: load <url>, switch <host|index>, back, mem <label>, gc, profiles, add <name>, remove <host|index>.
 */
final class Rig {
    static final String TAG = "IsoRig";
    private static final int CTL_PORT = 18502;

    private static boolean sw, swdeny, swprofile, blocknet, csp, tlsok;
    private static MainActivity activity;

    static native String nativeAdd(String name);
    static native void nativeRemove(String host);

    static String ctl(String path) {
        String[] out = new String[1];
        Thread t = new Thread(() -> {
            try (Socket s = new Socket("127.0.0.1", CTL_PORT)) {
                s.setSoTimeout(2000);
                OutputStream o = s.getOutputStream();
                o.write(("GET " + path + " HTTP/1.0\r\nHost: ctl\r\n\r\n").getBytes());
                o.flush();
                BufferedReader r = new BufferedReader(new InputStreamReader(s.getInputStream()));
                String line;
                while ((line = r.readLine()) != null && !line.isEmpty()) { }
                out[0] = r.readLine();
            } catch (Exception e) {
                Log.w(TAG, "ctl " + path + ": " + e);
            }
        });
        t.start();
        try { t.join(3000); } catch (InterruptedException ignored) { }
        return out[0] == null ? "" : out[0].trim();
    }

    static void start(MainActivity a) {
        activity = a;
        String m = ctl("/ctl/mode");
        for (String flag : m.split("\\+")) {
            sw |= flag.equals("sw");
            swdeny |= flag.equals("swdeny");
            swprofile |= flag.equals("swprofile");
            blocknet |= flag.equals("blocknet");
            csp |= flag.equals("csp");
            tlsok |= flag.equals("tlsok");
        }
        Log.i(TAG, "FLAGS " + m + " multiProfile=" + WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE)
                + " webview=" + (WebView.getCurrentWebViewPackage() == null ? "?" : WebView.getCurrentWebViewPackage().versionName));
        SpikeBridge.nativeRig(csp ? "csp" : "");
        if (sw) shareServiceWorkerClient(false);
        if (swdeny) shareServiceWorkerClient(true);
    }

    static void ready() {
        for (String h : hosts()) Log.i(TAG, "HOST " + h);
        Thread t = new Thread(Rig::commandLoop, "isorig-ctl");
        t.setDaemon(true);
        t.start();
    }

    /** Called for each view the product makes, including the first (after start). */
    static void configure(WebView v, String host) {
        if (blocknet) v.getSettings().setBlockNetworkLoads(true);
        if (swprofile && WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE)) {
            WebViewCompat.getProfile(v).getServiceWorkerController().setServiceWorkerClient(new ServiceWorkerClient() {
                @Override public WebResourceResponse shouldInterceptRequest(WebResourceRequest r) {
                    signal(null, r, "sw-profile-" + host);
                    return MainActivity.isWailsHostPublic(r.getUrl().getHost()) ? SpikeBridge.fetch(r, host) : null;
                }
            });
        }
    }

    private static void shareServiceWorkerClient(boolean deny) {
        ServiceWorkerController.getInstance().setServiceWorkerClient(new ServiceWorkerClient() {
            @Override public WebResourceResponse shouldInterceptRequest(WebResourceRequest r) {
                // No view here, so the only identity available is the host the request names: this is the gap.
                signal(null, r, deny ? "sw-deny" : "sw-shared");
                if (deny) return new WebResourceResponse("text/plain", "UTF-8", 403, "Refused", new java.util.HashMap<>(), new java.io.ByteArrayInputStream(new byte[0]));
                return MainActivity.isWailsHostPublic(r.getUrl().getHost()) ? SpikeBridge.fetch(r, r.getUrl().getHost()) : null;
            }
        });
        if (blocknet) ServiceWorkerController.getInstance().getServiceWorkerWebSettings().setBlockNetworkLoads(true);
    }

    static void navigation(WebView view, WebResourceRequest r, String owner, boolean allowed) {
        Log.i(TAG, (allowed ? "NAV-ALLOWED " : "NAV-REFUSED ") + r.getUrl() + " owner=" + owner + " main=" + r.isForMainFrame()
                + " gesture=" + r.hasGesture() + " redirect=" + r.isRedirect());
    }

    /** Logs every signal the callback gets. Only the header names are logged for anything but the headers under test. */
    static void signal(WebView view, WebResourceRequest r, String via) {
        Map<String, String> h = r.getRequestHeaders();
        StringBuilder vals = new StringBuilder();
        for (String k : new String[]{"Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Sec-Fetch-User", "X-Requested-With", "Ping-From", "Ping-To"}) {
            for (Map.Entry<String, String> e : h.entrySet()) if (e.getKey().equalsIgnoreCase(k)) vals.append(' ').append(k).append('=').append(e.getValue());
        }
        Log.i(TAG, "SIGNAL via=" + via + " " + r.getMethod() + " " + r.getUrl() + " main=" + r.isForMainFrame() + " gesture=" + r.hasGesture()
                + " redirect=" + r.isRedirect() + " view=" + (view == null ? "none" : Integer.toHexString(System.identityHashCode(view)))
                + " owner=" + (view == null ? "?" : Connections.owner(view)) + " keys=" + new TreeSet<>(h.keySet()) + vals);
    }

    static void pageFinished(WebView view, String url) {
        Log.i(TAG, "PAGE " + Connections.owner(view) + " " + Uri.parse(url).getHost() + " " + Uri.parse(url).getPath());
    }

    /** The rig never leaves the app: an external open is only logged. */
    static boolean external(Uri u, WebResourceRequest r) {
        Log.i(TAG, "EXTERNAL " + u + " main=" + r.isForMainFrame() + " gesture=" + r.hasGesture() + " redirect=" + r.isRedirect());
        return true;
    }

    static boolean trustLoopbackTls(String url) { return tlsok && url.startsWith("https://127.0.0.1:"); }

    private static List<String> hosts() { return SpikeBridge.hosts(); }

    private static String resolve(String arg) {
        try {
            return hosts().get(Integer.parseInt(arg.trim()));
        } catch (NumberFormatException | IndexOutOfBoundsException e) {
            return arg.trim();
        }
    }

    private static void mem(String label) {
        Debug.MemoryInfo m = new Debug.MemoryInfo();
        Debug.getMemoryInfo(m);
        ActivityManager.MemoryInfo am = new ActivityManager.MemoryInfo();
        ((ActivityManager) activity.getSystemService(Context.ACTIVITY_SERVICE)).getMemoryInfo(am);
        Log.i(TAG, "MEM " + label + " pssKB=" + m.getTotalPss() + " javaKB=" + m.dalvikPss + " nativeKB=" + m.nativePss + " availMB=" + (am.availMem >> 20));
    }

    private static void commandLoop() {
        Handler main = new Handler(Looper.getMainLooper());
        while (true) {
            String cmd = ctl("/ctl/next");
            if (!cmd.isEmpty()) {
                Log.i(TAG, "CMD " + cmd);
                String[] p = cmd.split(" ", 2);
                String arg = p.length > 1 ? p[1] : "";
                main.post(() -> {
                    long t0 = SystemClock.elapsedRealtime();
                    switch (p[0]) {
                        case "load": Connections.active().loadUrl(arg); break;
                        case "switch": {
                            String host = resolve(arg);
                            Connections.show(host);
                            Connections.active().postVisualStateCallback(t0, new WebView.VisualStateCallback() {
                                @Override public void onComplete(long id) {
                                    Log.i(TAG, "SWITCH to=" + host + " ms=" + (SystemClock.elapsedRealtime() - t0));
                                }
                            });
                            break;
                        }
                        case "back": Connections.active().goBack(); break;
                        case "mem": mem(arg); break;
                        case "gc": Runtime.getRuntime().gc(); break;
                        case "profiles":
                            Log.i(TAG, "PROFILES " + (WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE) ? ProfileStore.getInstance().getAllProfileNames() : "unsupported"));
                            break;
                        case "add": {
                            // The Go shell makes the connection, its host and its window; the host list then holds the new one.
                            String host = nativeAdd(arg);
                            Log.i(TAG, "ADDED " + arg + " host=" + host + " view=" + (host.isEmpty() ? "none" : Connections.add(host)));
                            break;
                        }
                        case "remove": {
                            String host = resolve(arg);
                            Connections.remove(host);
                            nativeRemove(host);
                            Log.i(TAG, "REMOVED " + host);
                            break;
                        }
                        default: Log.w(TAG, "unknown command " + cmd);
                    }
                });
            }
            SystemClock.sleep(100);
        }
    }
}
