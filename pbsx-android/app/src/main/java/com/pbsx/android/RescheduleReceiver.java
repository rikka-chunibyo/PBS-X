package com.pbsx.android;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** Refresh per-job schedules after an app update or boot, including legacy migration. */
public final class RescheduleReceiver extends BroadcastReceiver {
  @Override
  public void onReceive(Context context, Intent intent) {
    String action = intent.getAction();
    if (!Intent.ACTION_BOOT_COMPLETED.equals(action)
        && !Intent.ACTION_MY_PACKAGE_REPLACED.equals(action)
        && !Intent.ACTION_TIME_CHANGED.equals(action)
        && !Intent.ACTION_TIMEZONE_CHANGED.equals(action)) return;
    PendingResult result = goAsync();
    new Thread(
            () -> {
              try {
                if (Intent.ACTION_TIME_CHANGED.equals(action)
                    || Intent.ACTION_TIMEZONE_CHANGED.equals(action)) {
                  for (BackupJob job : JobStore.all(context)) {
                    if (job.calendar && !job.id.equals(Operations.activeJobId())) {
                      ScheduleState.refreshClock(context, job);
                      context
                          .getSystemService(android.app.job.JobScheduler.class)
                          .cancel(job.schedulerId);
                    }
                  }
                }
                ScheduleService.configureAll(context);
              } catch (Exception e) {
                Settings.status(context, "Could not refresh schedules: " + e.getMessage());
              } finally {
                result.finish();
              }
            },
            "pbsx-schedules")
        .start();
  }
}
