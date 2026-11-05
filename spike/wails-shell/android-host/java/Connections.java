package com.wails.app;

import android.content.Context;
import android.content.SharedPreferences;
import android.net.Uri;
import android.util.Log;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;

import androidx.webkit.ProfileStore;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import java.util.Collections;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * One WebView and one Profile per connection. A request's connection is the host of the view that sent it, never
 * anything the page says. The Go shell owns the list of connections and their hosts; this class owns the views.
 *
 * A view not in the map has no owner, so everything it asks for is refused. A second connection is refused when
 * MULTI_PROFILE is missing, because it would share the first one's storage.
 */
final class Connections {
    private static final String TAG = "Connections";
    private static final String PREFS = "connections", PENDING = "profilesToDelete";

    private static final Map<WebView, String> owners = Collections.synchronizedMap(new IdentityHashMap<>());
    private static final Map<String, WebView> views = Collections.synchronizedMap(new LinkedHashMap<>());
    private static MainActivity activity;
    private static WebView template, active;
    private static List<String> hosts;

    private static boolean multiProfile() { return WebViewFeature.isFeatureSupported(WebViewFeature.MULTI_PROFILE); }

    /** The profile is named by the connection's random host label, which is never reused, so a new connection cannot inherit a deleted one's data. */
    private static String profileName(String host) {
        int dot = host.indexOf('.');
        return dot < 0 ? host : host.substring(0, dot);
    }

    /**
     * Runs before anything else touches the first WebView, because setProfile has to. Returns the host that view serves,
     * or null when the shell has no connection, which leaves the app with nothing to load.
     */
    static String start(MainActivity a, WebView first) {
        activity = a;
        hosts = SpikeBridge.hosts();
        Rig.start(a);
        if (hosts.isEmpty()) {
            Log.e(TAG, "no connections");
            return null;
        }
        if (multiProfile()) {
            deletePendingProfiles();
            WebViewCompat.setProfile(first, profileName(hosts.get(0)));
        } else if (hosts.size() > 1) {
            Log.e(TAG, "MULTI_PROFILE unsupported: only the first of " + hosts.size() + " connections is opened");
        }
        template = active = first;
        owners.put(first, hosts.get(0));
        views.put(hosts.get(0), first);
        return hosts.get(0);
    }

    /** Called once the first view is configured: makes a view for each other connection, and keeps the first in front. */
    static void ready() {
        Rig.configure(template, hosts.get(0));
        for (int i = 1; i < hosts.size(); i++) add(hosts.get(i));
        Rig.ready();
    }

    /** Opens a connection in its own view and profile. False when it was refused. */
    static boolean add(String host) {
        synchronized (views) {
            if (views.containsKey(host)) return false;
            if (!multiProfile()) {
                Log.e(TAG, "MULTI_PROFILE unsupported: a second connection would share a profile, refused");
                return false;
            }
            WebView v = new WebView(activity);
            try {
                WebViewCompat.setProfile(v, profileName(host));
            } catch (RuntimeException e) {
                Log.e(TAG, "no profile for a new connection, refused", e);
                v.destroy();
                return false;
            }
            owners.put(v, host);
            views.put(host, v);
            WebSettings s = v.getSettings(), t = template.getSettings();
            s.setJavaScriptEnabled(true);
            s.setDomStorageEnabled(true);
            s.setDatabaseEnabled(true);
            s.setAllowFileAccess(false);
            s.setAllowContentAccess(false);
            s.setMediaPlaybackRequiresUserGesture(false);
            s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
            s.setSupportMultipleWindows(t.supportMultipleWindows());
            v.setWebViewClient(template.getWebViewClient());
            v.setWebChromeClient(template.getWebChromeClient());
            activity.installView(v, host);
            Rig.configure(v, host);
            v.setVisibility(View.INVISIBLE);
            ((ViewGroup) template.getParent()).addView(v, new ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
            v.loadUrl("https://" + host + "/");
            return true;
        }
    }

    /** Puts a connection's view in front. Native switches flip visibility, because loadUrl would skip the navigation check. */
    static void show(String host) {
        synchronized (views) {
            WebView target = views.get(host);
            if (target == null) return;
            for (WebView v : views.values()) v.setVisibility(v == target ? View.VISIBLE : View.INVISIBLE);
            active = target;
            target.requestFocus();
        }
    }

    /** Destroys the connection's view and tries to delete its profile; a profile that cannot go yet is deleted at the next launch. */
    static void remove(String host) {
        synchronized (views) {
            WebView v = views.remove(host);
            if (v == null) return;
            owners.remove(v);
            ((ViewGroup) v.getParent()).removeView(v);
            v.destroy();
            if (active == v) {
                active = null;
                for (String other : views.keySet()) { show(other); break; }
            }
        }
        if (multiProfile()) deleteProfile(profileName(host));
    }

    private static void deleteProfile(String name) {
        try {
            ProfileStore.getInstance().deleteProfile(name);
        } catch (RuntimeException e) {
            // The profile is still loaded in this process, so its data is deleted at the next launch, before a view exists.
            Log.w(TAG, "profile delete postponed: " + e);
            SharedPreferences p = activity.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
            Set<String> pending = new HashSet<>(p.getStringSet(PENDING, new HashSet<>()));
            pending.add(name);
            p.edit().putStringSet(PENDING, pending).apply();
        }
    }

    private static void deletePendingProfiles() {
        SharedPreferences p = activity.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        Set<String> left = new HashSet<>();
        for (String name : p.getStringSet(PENDING, new HashSet<>())) {
            try {
                ProfileStore.getInstance().deleteProfile(name);
            } catch (RuntimeException e) {
                Log.w(TAG, "profile delete still postponed: " + e);
                left.add(name);
            }
        }
        p.edit().putStringSet(PENDING, left).apply();
    }

    static WebView active() { return active; }

    /** The host of the connection this view serves. "" for a view that is not one of ours: Go refuses a request with no owner. */
    static String owner(WebView view) {
        String o = owners.get(view);
        return o == null ? "" : o;
    }

    /** A connection host may only be navigated to by its own view. */
    static boolean allowNavigation(WebView view, WebResourceRequest r) {
        Uri u = r.getUrl();
        String owner = owner(view);
        boolean ok = !owner.isEmpty() && owner.equals(u.getHost());
        Rig.navigation(view, r, owner, ok);
        return ok;
    }
}
