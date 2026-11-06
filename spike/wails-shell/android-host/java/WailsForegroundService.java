package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;
import android.util.Log;

import androidx.annotation.Nullable;
import androidx.core.app.NotificationCompat;

/**
 * Replaces Wails' dataSync service with a shortService one (about 3 minutes, no extra permission), started while the app is
 * still visible, just before the browser hop, so the process is not frozen while the user signs in. Go starts and stops it with
 * Android.StartForegroundService / StopForegroundService. The timeout is logged, because the spike measures what happens past it.
 * shortService exists from API 34, so below that the service starts untyped and is not a measured case.
 */
public class WailsForegroundService extends android.app.Service {
    public static final String ACTION_START = "com.wails.app.FGS_START";
    private static final String TAG = "SpikeFgs";
    private static final String CHANNEL_ID = "spike_hop";
    private static final int NOTIFICATION_ID = 0x57A1;

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            nm.createNotificationChannel(new NotificationChannel(CHANNEL_ID, "Browser sign-in", NotificationManager.IMPORTANCE_LOW));
        }
        String title = intent != null && intent.getStringExtra("title") != null ? intent.getStringExtra("title") : "Signing in";
        String text = intent != null && intent.getStringExtra("text") != null ? intent.getStringExtra("text") : "Waiting for the browser";
        PendingIntent contentIntent = null;
        Intent launch = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (launch != null) {
            contentIntent = PendingIntent.getActivity(this, 0, launch,
                    Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0);
        }
        Notification n = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setSmallIcon(android.R.drawable.ic_popup_sync).setContentTitle(title).setContentText(text).setOngoing(true)
                .setContentIntent(contentIntent).build();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SHORT_SERVICE);
        } else {
            startForeground(NOTIFICATION_ID, n);
        }
        Log.i(TAG, "FGS-START " + System.currentTimeMillis());
        return START_NOT_STICKY;
    }

    /** Called by the system when the short-service budget is spent; the service has to stop itself now. Both forms are handled, and logged apart. */
    @Override
    public void onTimeout(int startId) {
        Log.w(TAG, "FGS-TIMEOUT(startId) " + System.currentTimeMillis());
        stopSelf();
    }

    @Override
    public void onTimeout(int startId, int fgsType) {
        Log.w(TAG, "FGS-TIMEOUT(startId,type) " + System.currentTimeMillis());
        stopSelf();
    }

    @Override
    public void onDestroy() { Log.i(TAG, "FGS-STOP " + System.currentTimeMillis()); }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) { return null; }
}
