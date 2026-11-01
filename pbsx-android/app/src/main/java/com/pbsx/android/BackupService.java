package com.pbsx.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.os.IBinder;

public final class BackupService extends Service {
  private Operations.Cancellation cancellation;
  private static final int ID = 71;
  private volatile boolean working;
  private volatile long lastUpdate;

  private Notification notification(String text) {
    PendingIntent open =
        PendingIntent.getActivity(
            this,
            0,
            new Intent(this, MainActivity.class),
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    PendingIntent cancel =
        PendingIntent.getService(
            this,
            1,
            new Intent(this, BackupService.class).setAction("cancel"),
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    return new Notification.Builder(this, "backups")
        .setSmallIcon(android.R.drawable.stat_sys_upload)
        .setContentTitle("PBS-X Backup")
        .setContentText(text)
        .setContentIntent(open)
        .setOngoing(working)
        .setOnlyAlertOnce(true)
        .addAction(new Notification.Action.Builder(null, "Cancel", cancel).build())
        .build();
  }

  @Override
  public void onCreate() {
    super.onCreate();
    getSystemService(NotificationManager.class)
        .createNotificationChannel(
            new NotificationChannel(
                "backups", "Backup progress", NotificationManager.IMPORTANCE_LOW));
  }

  @Override
  public int onStartCommand(Intent intent, int flags, int startId) {
    if (intent == null) {
      stopSelf();
      return START_NOT_STICKY;
    }
    if ("cancel".equals(intent.getAction())) {
      Operations.cancel();
      if (!working) stopSelf();
      return START_NOT_STICKY;
    }
    if (working) return START_NOT_STICKY;
    working = true;
    cancellation = new Operations.Cancellation();
    startForeground(ID, notification("Starting…"));
    Settings.status(this, "Operation running…");
    new Thread(
            () -> {
              String result;
              try {
                result =
                    Operations.run(
                        this,
                        intent.getStringExtra("destination"),
                        intent.getStringExtra("source"),
                        intent.getLongExtra("snapshot", 0),
                        (stage, files, bytes) -> {
                          long now = System.currentTimeMillis();
                          if (now - lastUpdate > 1000) {
                            lastUpdate = now;
                            String progress =
                                stage
                                    + " · "
                                    + android.text.format.Formatter.formatFileSize(this, bytes);
                            Settings.status(this, progress);
                            getSystemService(NotificationManager.class)
                                .notify(ID, notification(progress));
                          }
                        },
                        cancellation);
              } catch (Exception e) {
                result = "Failed: " + e.getMessage();
              }
              working = false;
              Settings.status(this, result);
              stopForeground(STOP_FOREGROUND_REMOVE);
              getSystemService(NotificationManager.class).notify(ID, notification(result));
              stopSelf();
            },
            "pbsx-operation")
        .start();
    return START_NOT_STICKY;
  }

  @Override
  public void onTimeout(int startId, int type) {
    if (cancellation != null) cancellation.cancel();
    Settings.status(
        this, "Stopped: Android background transfer time limit reached; retry manually");
    stopForeground(STOP_FOREGROUND_REMOVE);
    stopSelf();
  }

  @Override
  public void onDestroy() {
    if (working && cancellation != null) cancellation.cancel();
    super.onDestroy();
  }

  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
