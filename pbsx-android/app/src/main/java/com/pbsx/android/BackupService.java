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
  private final android.os.Handler handler =
      new android.os.Handler(android.os.Looper.getMainLooper());
  private final Runnable refreshNotification =
      new Runnable() {
        public void run() {
          if (!working) return;
          if (Operations.isActive(cancellation))
            getSystemService(NotificationManager.class)
                .notify(ID, notification(TransferState.current().summary(BackupService.this)));
          handler.postDelayed(this, 1000);
        }
      };

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
    Notification.Builder builder =
        new Notification.Builder(this, "backups")
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle("PBS-X Backup")
            .setContentText(text)
            .setStyle(new Notification.BigTextStyle().bigText(text))
            .setContentIntent(open)
            .setOngoing(working)
            .setOnlyAlertOnce(true);
    if (working) {
      if (Operations.canPause())
        builder.addAction(
            new Notification.Action.Builder(
                    null,
                    "Pause",
                    PendingIntent.getService(
                        this,
                        2,
                        new Intent(this, BackupService.class).setAction("pause"),
                        PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT))
                .build());
      builder.addAction(new Notification.Action.Builder(null, "Cancel", cancel).build());
    }
    if (working) {
      TransferState.Snapshot state = TransferState.current();
      boolean known = Operations.isActive(cancellation) && state.hasFileProgress();
      builder.setProgress(100, known ? state.percent() : 0, !known);
      builder.setSubText(known ? state.barLabel() : "Transfer in progress");
    }
    return builder.build();
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
    if ("pause".equals(intent.getAction())) {
      if (cancellation != null) cancellation.pause("Paused by you", false);
      else Operations.pause("Paused by you", false);
      if (!working) stopSelf();
      return START_NOT_STICKY;
    }
    if ("cancel".equals(intent.getAction())) {
      if (cancellation != null) cancellation.cancel();
      else Operations.cancel();
      if (!working) stopSelf();
      return START_NOT_STICKY;
    }
    if (working) return START_NOT_STICKY;
    working = true;
    cancellation = new Operations.Cancellation();
    startForeground(ID, notification("Preparing transfer…"));
    handler.post(refreshNotification);
    Settings.status(this, "Operation running…");
    new Thread(
            () -> {
              String result;
              try {
                BackupJob job;
                String saved = intent.getStringExtra("job_config");
                if (saved != null) job = BackupJob.from(new org.json.JSONObject(saved));
                else job = JobStore.get(this, intent.getStringExtra("job_id"));
                if ("discard".equals(intent.getAction()))
                  result = Operations.discardPending(this, job.id);
                else
                  result =
                      Operations.runGroupJob(
                          this,
                          job,
                          intent.getStringExtra("destination"),
                          intent.getStringExtra("source"),
                          intent.getStringExtra("source_type") == null
                              ? "host"
                              : intent.getStringExtra("source_type"),
                          intent.getLongExtra("snapshot", 0),
                          intent.getStringExtra("selection") == null
                              ? "[\".\"]"
                              : intent.getStringExtra("selection"),
                          intent.getStringExtra("mode") == null
                              ? "fail"
                              : intent.getStringExtra("mode"),
                          (stage, files, bytes) -> {
                            long now = System.currentTimeMillis();
                            if (now - lastUpdate > 1000) {
                              lastUpdate = now;
                              String progress = stage;
                              getSystemService(NotificationManager.class)
                                  .notify(ID, notification(progress));
                            }
                          },
                          cancellation);
              } catch (Exception e) {
                result = "Failed: " + e.getMessage();
              }
              working = false;
              handler.removeCallbacks(refreshNotification);
              Settings.status(this, result);
              try {
                ScheduleService.configureAll(this);
              } catch (Exception e) {
                Settings.status(this, result + " · Could not update schedules: " + e.getMessage());
              }
              stopForeground(STOP_FOREGROUND_REMOVE);
              if (cancellation.paused()) getSystemService(NotificationManager.class).cancel(ID);
              else getSystemService(NotificationManager.class).notify(ID, notification(result));
              stopSelf();
            },
            "pbsx-operation")
        .start();
    return START_NOT_STICKY;
  }

  @Override
  public void onTimeout(int startId, int type) {
    if (cancellation != null) {
      if (TransferState.current().restore) cancellation.cancel();
      else cancellation.pause("Paused: Android background transfer time limit reached", false);
    }
    Settings.status(this, "Android background time limit reached; backup checkpoint retained");
    stopForeground(STOP_FOREGROUND_REMOVE);
    stopSelf();
  }

  @Override
  public void onDestroy() {
    handler.removeCallbacks(refreshNotification);
    if (working && cancellation != null) {
      if (TransferState.current().restore) cancellation.cancel();
      else cancellation.pause("Paused: Android stopped the transfer service", false);
    }
    super.onDestroy();
  }

  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
