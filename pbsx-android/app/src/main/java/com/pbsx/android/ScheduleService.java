package com.pbsx.android;

import android.app.job.JobInfo;
import android.app.job.JobParameters;
import android.app.job.JobScheduler;
import android.app.job.JobService;
import android.content.ComponentName;
import android.content.Context;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.os.Build;

/** Optional constrained periodic jobs; Android decides exact execution time. */
public final class ScheduleService extends JobService {
  private volatile boolean stopped;
  private Operations.Cancellation cancellation;

  static void configure(Context c, int hours, boolean charging) {
    JobScheduler scheduler = c.getSystemService(JobScheduler.class);
    scheduler.cancel(72);
    if (hours == 0) return;
    JobInfo.Builder builder =
        new JobInfo.Builder(72, new ComponentName(c, ScheduleService.class))
            .setPersisted(true)
            .setPeriodic(hours * 60L * 60 * 1000)
            .setRequiresCharging(charging);
    if (Settings.prefs(c).getBoolean("wifi", true) && Build.VERSION.SDK_INT >= 28)
      builder.setRequiredNetwork(
          new NetworkRequest.Builder()
              .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
              .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
              .build());
    else
      builder.setRequiredNetworkType(
          Settings.prefs(c).getBoolean("wifi", true)
              ? JobInfo.NETWORK_TYPE_UNMETERED
              : JobInfo.NETWORK_TYPE_ANY);
    JobInfo job = builder.build();
    if (scheduler.schedule(job) != JobScheduler.RESULT_SUCCESS)
      throw new IllegalStateException("Android refused the schedule");
  }

  @Override
  public boolean onStartJob(JobParameters params) {
    stopped = false;
    if (Operations.running()) return false;
    cancellation = new Operations.Cancellation();
    final Operations.Cancellation operation = cancellation;
    new Thread(
            () -> {
              String result;
              try {
                result =
                    Operations.run(this, null, null, 0, (stage, files, bytes) -> {}, operation);
              } catch (Exception e) {
                result = "Scheduled backup failed: " + e.getMessage();
              }
              Settings.status(this, result);
              if (!stopped) jobFinished(params, false);
            },
            "pbsx-scheduled")
        .start();
    return true;
  }

  @Override
  public boolean onStopJob(JobParameters params) {
    stopped = true;
    if (cancellation != null) cancellation.cancel();
    Settings.status(this, "Scheduled backup paused by Android; no completed snapshot was claimed");
    return true;
  }
}
