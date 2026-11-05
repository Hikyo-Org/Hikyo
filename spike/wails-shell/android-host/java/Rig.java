package com.wails.app;

import android.net.Uri;
import android.webkit.WebResourceRequest;
import android.webkit.WebView;

/** The hooks the isolation rig uses. The product build has none; android-host/rig/Rig.java replaces this file when ISO_TAGS has isorig. */
final class Rig {
    static void start(MainActivity a) { }
    static void ready() { }
    static void configure(WebView v, String host) { }
    static void signal(WebView v, WebResourceRequest r, String via) { }
    static void navigation(WebView v, WebResourceRequest r, String owner, boolean allowed) { }
    static void pageFinished(WebView v, String url) { }
    /** True when the rig has taken the link, so the app does not leave for the browser. */
    static boolean external(Uri u, WebResourceRequest r) { return false; }
    static boolean trustLoopbackTls(String url) { return false; }
}
