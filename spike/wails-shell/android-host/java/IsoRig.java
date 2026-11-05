package com.wails.app;

import android.app.ActivityManager;
import android.content.Context;
import android.net.Uri;
import android.os.Debug;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.Log;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.ServiceWorkerClient;
import android.webkit.ServiceWorkerController;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;

import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.Socket;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.TreeSet;

/**
 * Rig for the connection-isolation candidates. The mode comes from a control port on the host, so one APK tests them all:
 *   none               the host as it was: a request's connection is the host it names
 *   perview            one WebView per connection; the view argument is the identity
 *   perview-profile    the same, with one androidx.webkit Profile per connection
 *   pinned             one WebView, with the connection recorded natively and switched before the load
 *   pinned-drain       one WebView, refusing everything through about:blank during a switch
 * Flags after '+': sw (shared ServiceWorkerClient), swdeny (a client that refuses everything), blocknet (setBlockNetworkLoads),
 * csp (the proxy adds its own CSP), tlsok (accept the rig's self-signed redirector on 127.0.0.1).
 * A product takes the chosen behaviour from this file and none of its control port.
 */
final class IsoRig {
    static final String TAG = "IsoRig";
    static final String C0 = "conn-0.wails.localhost", C1 = "conn-1.wails.localhost";
    private static final int CTL_PORT = 18202;

    static volatile String mode = "none";
    static volatile String current = C0; // the pinned connection; null refuses everything
    static boolean sw, swdeny, blocknet, csp, tlsok;
    private static final Map<WebView, String> views = Collections.synchronizedMap(new IdentityHashMap<>());
    private static final Map<String, Long> loadStart = Collections.synchronizedMap(new java.util.HashMap<>());
    private static MainActivity activity;
    private static WebView first, active;
    private static volatile String drainTarget;
    private static long switchStart;

    static boolean perView() { return mode.startsWith("perview"); }
    static boolean pinned() { return mode.startsWith("pinned"); }
    static boolean profiles() { return mode.equals("perview-profile"); }

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

    /** Runs before anything else touches the first WebView, because setProfile has to. */
    static void start(MainActivity a, WebView view) {
        activity = a;
        first = active = view;
        String m = ctl("/ctl/mode");
        if (!m.isEmpty()) {
            String[] parts = m.split("\\+");
            mode = parts[0];
            for (int i = 1; i < parts.length; i++) {
                if (parts[i].equals("sw")) sw = true;
                if (parts[i].equals("blocknet")) blocknet = true;
                if (parts[i].equals("csp")) csp = true;
                if (parts[i].equals("swdeny")) swdeny = true;
                if (parts[i].equals("tlsok")) tlsok = true;
            }
        }
        Log.i(TAG, "MODE " + m + " multiProfile=" + WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE)
                + " webview=" + (WebView.getCurrentWebViewPackage() == null ? "?" : WebView.getCurrentWebViewPackage().versionName));
        SpikeBridge.nativeRig(csp ? "csp" : "");
        views.put(view, C0);
        if (profiles()) WebViewCompat.setProfile(view, "conn-0");
    }

    /** Called when the first WebView is configured: flags, the second WebView, the service worker clients, and the command loop. */
    static void ready() {
        if (blocknet) first.getSettings().setBlockNetworkLoads(true);
        if (sw) installSharedServiceWorkerClient();
        if (swdeny) installDenyingServiceWorkerClient();
        if (profiles()) installProfileServiceWorkerClient(first, C0);
        if (perView()) secondView();
        Thread t = new Thread(IsoRig::commandLoop, "isorig-ctl");
        t.setDaemon(true);
        t.start();
    }

    private static void secondView() {
        WebView v = new WebView(activity);
        views.put(v, C1);
        if (profiles()) {
            WebViewCompat.setProfile(v, "conn-1");
            installProfileServiceWorkerClient(v, C1);
        }
        WebSettings s = v.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setDatabaseEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        s.setSupportMultipleWindows(true);
        if (blocknet) s.setBlockNetworkLoads(true);
        v.setWebViewClient(first.getWebViewClient());
        v.setWebChromeClient(first.getWebChromeClient());
        activity.installView(v);
        v.setVisibility(View.INVISIBLE);
        ((ViewGroup) first.getParent()).addView(v, new ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        loadStart.put(C1, SystemClock.elapsedRealtime());
        v.loadUrl("https://" + C1 + "/");
    }

    private static void installSharedServiceWorkerClient() {
        ServiceWorkerController.getInstance().setServiceWorkerClient(new ServiceWorkerClient() {
            @Override public WebResourceResponse shouldInterceptRequest(WebResourceRequest r) {
                // No view here, so the only identity available is the host the request names: this is the gap.
                signal(null, r, "sw-shared");
                return MainActivity.isWailsHostPublic(r.getUrl().getHost()) ? SpikeBridge.fetch(r, r.getUrl().getHost()) : null;
            }
        });
        if (blocknet) ServiceWorkerController.getInstance().getServiceWorkerWebSettings().setBlockNetworkLoads(true);
    }

    private static void installDenyingServiceWorkerClient() {
        ServiceWorkerController.getInstance().setServiceWorkerClient(new ServiceWorkerClient() {
            @Override public WebResourceResponse shouldInterceptRequest(WebResourceRequest r) {
                signal(null, r, "sw-deny");
                return new WebResourceResponse("text/plain", "UTF-8", 403, "Refused", new java.util.HashMap<>(), new java.io.ByteArrayInputStream(new byte[0]));
            }
        });
    }

    private static void installProfileServiceWorkerClient(WebView v, String owner) {
        if (!sw) return;
        WebViewCompat.getProfile(v).getServiceWorkerController().setServiceWorkerClient(new ServiceWorkerClient() {
            @Override public WebResourceResponse shouldInterceptRequest(WebResourceRequest r) {
                signal(null, r, "sw-profile-" + owner);
                return MainActivity.isWailsHostPublic(r.getUrl().getHost()) ? SpikeBridge.fetch(r, owner) : null;
            }
        });
    }

    /** The connection this request belongs to, as far as the mode can tell; "" fails closed in Go. */
    static String owner(WebView view, WebResourceRequest r) {
        String o;
        if (perView()) o = views.get(view);
        else if (pinned()) o = current;
        else o = r.getUrl().getHost();
        return o == null ? "" : o;
    }

    static boolean allowNavigation(WebView view, WebResourceRequest r) {
        String host = r.getUrl().getHost();
        if (!MainActivity.isWailsHostPublic(host)) return true;
        if (perView() || pinned()) {
            String o = owner(view, r);
            if (!host.equals(o)) {
                Log.i(TAG, "NAV-REFUSED " + r.getUrl() + " owner=" + o + " main=" + r.isForMainFrame() + " gesture=" + r.hasGesture() + " redirect=" + r.isRedirect());
                return false;
            }
        }
        Log.i(TAG, "NAV-ALLOWED " + r.getUrl() + " main=" + r.isForMainFrame() + " gesture=" + r.hasGesture() + " redirect=" + r.isRedirect());
        return true;
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
                + " owner=" + (view == null ? "?" : owner(view, r)) + " keys=" + new TreeSet<>(h.keySet()) + vals);
    }

    static WebView activeView() { return active; }
    static String ownerOfView(WebView v) { String o = views.get(v); return o == null ? "" : o; }

    static void pageFinished(WebView view, String url) {
        Uri u = Uri.parse(url);
        Long started = u.getHost() == null ? null : loadStart.remove(u.getHost());
        if (started != null) Log.i(TAG, "LOAD " + u.getHost() + " ms=" + (SystemClock.elapsedRealtime() - started));
        if (mode.equals("pinned-drain") && drainTarget != null && "about:blank".equals(url)) {
            current = drainTarget;
            String target = drainTarget;
            drainTarget = null;
            view.loadUrl("https://" + target + "/");
        }
        if (switchStart != 0 && u.getHost() != null && u.getHost().equals(current)) {
            Log.i(TAG, "SWITCH mode=" + mode + " to=" + u.getHost() + " ms=" + (SystemClock.elapsedRealtime() - switchStart));
            switchStart = 0;
        }
    }

    private static void switchTo(String host) {
        long t0 = SystemClock.elapsedRealtime();
        if (perView()) {
            synchronized (views) {
                for (Map.Entry<WebView, String> e : views.entrySet()) {
                    e.getKey().setVisibility(e.getValue().equals(host) ? View.VISIBLE : View.INVISIBLE);
                    if (e.getValue().equals(host)) active = e.getKey();
                }
            }
            active.post(() -> active.postVisualStateCallback(t0, new WebView.VisualStateCallback() {
                @Override public void onComplete(long id) {
                    Log.i(TAG, "SWITCH mode=" + mode + " to=" + host + " ms=" + (SystemClock.elapsedRealtime() - t0));
                }
            }));
        } else if (mode.equals("pinned")) {
            switchStart = t0;
            current = host;
            first.loadUrl("https://" + host + "/");
        } else if (mode.equals("pinned-drain")) {
            switchStart = t0;
            current = null;
            drainTarget = host;
            first.stopLoading();
            first.loadUrl("about:blank");
        } else {
            switchStart = t0;
            first.loadUrl("https://" + host + "/");
        }
    }

    /** Removing a connection: destroy its view, optionally delete its profile, and open it again to see what storage survived. */
    private static void recreate(String how) {
        WebView old = null;
        synchronized (views) {
            for (Map.Entry<WebView, String> e : views.entrySet()) if (e.getValue().equals(C1)) old = e.getKey();
        }
        views.remove(old);
        ((ViewGroup) old.getParent()).removeView(old);
        old.destroy();
        new Handler(Looper.getMainLooper()).postDelayed(() -> {
            if (how.equals("delete")) {
                try {
                    Log.i(TAG, "DELPROFILE conn-1 -> " + androidx.webkit.ProfileStore.getInstance().deleteProfile("conn-1"));
                } catch (RuntimeException e) {
                    Log.i(TAG, "DELPROFILE conn-1 threw " + e);
                }
            }
            secondView();
        }, 500);
    }

    private static void mem(String label) {
        Debug.MemoryInfo m = new Debug.MemoryInfo();
        Debug.getMemoryInfo(m);
        ActivityManager.MemoryInfo am = new ActivityManager.MemoryInfo();
        ((ActivityManager) activity.getSystemService(Context.ACTIVITY_SERVICE)).getMemoryInfo(am);
        synchronized (views) {
            for (Map.Entry<WebView, String> e : views.entrySet()) {
                Log.i(TAG, "RENDERER " + e.getValue() + " process=" + System.identityHashCode(WebViewCompat.getWebViewRenderProcess(e.getKey())) + " profile=" + (WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE) ? WebViewCompat.getProfile(e.getKey()).getName() : "?"));
            }
        }
        Log.i(TAG, "MEM " + label + " mode=" + mode + " pssKB=" + m.getTotalPss() + " javaKB=" + m.dalvikPss + " nativeKB=" + m.nativePss + " availMB=" + (am.availMem >> 20));
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
                    switch (p[0]) {
                        case "load": active.loadUrl(arg); break;
                        case "switch": switchTo(arg); break;
                        case "back": active.goBack(); break;
                        case "mem": mem(arg); break;
                        case "gc": Runtime.getRuntime().gc(); break;
                        case "recreate": recreate(arg); break;
                        default: Log.w(TAG, "unknown command " + cmd);
                    }
                });
            }
            SystemClock.sleep(100);
        }
    }
}
