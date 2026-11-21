package com.pbsx.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.os.IBinder;

public final class BackupService extends Service {
  private static volatile BackupService owner;
  private Operations.Cancellation cancellation;
  private static final int ID = 71;
  private volatile boolean working;
  private int lastStartId;
  private boolean notificationsAllowed;
  private String commandToken;

  static boolean busy() {
    BackupService service = owner;
    return service != null && service.working;
  }

  private volatile long lastUpdate;
  private final android.os.Handler handler =
      new android.os.Handler(android.os.Looper.getMainLooper());
  private final Runnable refreshNotification =
      new Runnable() {
        public void run() {
          if (!working || !notificationsAllowed || owner != BackupService.this) return;
          if (Operations.isActive(cancellation))
            getSystemService(NotificationManager.class)
                .notify(ID, notification(TransferState.current().summary(BackupService.this)));
          handler.postDelayed(this, 1000);
        }
      };

  Intent controlIntent(String action) {
    return new Intent(this, BackupService.class)
        .setAction(action)
        .setData(android.net.Uri.parse("pbsx-operation:" + commandToken))
        .putExtra("operation_token", commandToken);
  }

  private PendingIntent control(String action, int requestCode) {
    return PendingIntent.getService(
        this,
        requestCode,
        controlIntent(action),
        PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
  }

  private void progressNotification(Operations.Cancellation token, String text) {
    handler.post(
        () -> {
          if (owner == this
              && cancellation == token
              && working
              && notificationsAllowed
              && Operations.isActive(token))
            getSystemService(NotificationManager.class).notify(ID, notification(text));
        });
  }

  private Notification notification(String text) {
    PendingIntent open =
        PendingIntent.getActivity(
            this,
            0,
            new Intent(this, MainActivity.class),
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
            new Notification.Action.Builder(null, "Pause", control("pause", 2)).build());
      builder.addAction(
          new Notification.Action.Builder(null, "Cancel", control("cancel", 1)).build());
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
    lastStartId = startId;
    if (intent == null) {
      stopSelf(startId);
      return START_NOT_STICKY;
    }
    if ("pause".equals(intent.getAction()) || "cancel".equals(intent.getAction())) {
      if (!working
          || owner != this
          || commandToken == null
          || !commandToken.equals(intent.getStringExtra("operation_token"))) {
        if (!working) stopSelf(startId);
        return START_NOT_STICKY;
      }
    }
    if ("pause".equals(intent.getAction())) {
      if (cancellation != null) cancellation.pause("Paused by you", false);
      else Operations.pause("Paused by you", false);
      if (!working) stopSelf(startId);
      return START_NOT_STICKY;
    }
    if ("cancel".equals(intent.getAction())) {
      if (cancellation != null) cancellation.cancel();
      else Operations.cancel();
      if (!working) stopSelf(startId);
      return START_NOT_STICKY;
    }
    if (working) return START_NOT_STICKY;
    working = true;
    cancellation = new Operations.Cancellation();
    commandToken = java.util.UUID.randomUUID().toString();
    notificationsAllowed = true;
    owner = this;
    final Operations.Cancellation token = cancellation;
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
                            if (!Operations.isActive(token)) return;
                            long now = System.currentTimeMillis();
                            if (now - lastUpdate > 1000) {
                              lastUpdate = now;
                              progressNotification(token, stage);
                            }
                          },
                          token);
              } catch (Exception e) {
                result = "Failed: " + e.getMessage();
              }
              try {
                ScheduleService.configureAll(this);
              } catch (Exception e) {
                result += " · Could not update schedules: " + e.getMessage();
              }
              final String finished = result;
              // Serialize cleanup with onStartCommand. A stale worker must not
              // remove a newly resumed task's notification or stop its service.
              handler.post(
                  () -> {
                    if (owner != this || cancellation != token) return;
                    working = false;
                    handler.removeCallbacks(refreshNotification);
                    Settings.status(this, finished);
                    stopForeground(STOP_FOREGROUND_REMOVE);
                    if (!notificationsAllowed || token.paused())
                      getSystemService(NotificationManager.class).cancel(ID);
                    else
                      getSystemService(NotificationManager.class)
                          .notify(ID, notification(finished));
                    stopSelf(lastStartId);
                  });
            },
            "pbsx-operation")
        .start();
    return START_NOT_STICKY;
  }

  @Override
  public void onTimeout(int startId, int type) {
    notificationsAllowed = false;
    handler.removeCallbacks(refreshNotification);
    if (cancellation != null) {
      if (TransferState.current().restore) cancellation.cancel();
      else cancellation.pause("Paused: Android background transfer time limit reached", false);
    }
    Settings.status(this, "Android background time limit reached; backup checkpoint retained");
    stopForeground(STOP_FOREGROUND_REMOVE);
    if (owner == this) getSystemService(NotificationManager.class).cancel(ID);
    stopSelf();
  }

  @Override
  public void onDestroy() {
    notificationsAllowed = false;
    handler.removeCallbacks(refreshNotification);
    if (working && cancellation != null) {
      if (TransferState.current().restore) cancellation.cancel();
      else cancellation.pause("Paused: Android stopped the transfer service", false);
    }
    if (owner == this) {
      owner = null;
      if (working) getSystemService(NotificationManager.class).cancel(ID);
    }
    super.onDestroy();
  }

  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
