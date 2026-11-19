package com.pbsx.android;

import android.app.job.*;
import android.content.ComponentName;
import android.content.Context;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.os.Build;
import android.os.PersistableBundle;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

/** Independent constrained schedules for each saved local job. */
public final class ScheduleService extends JobService {
  private final Map<Integer, Operations.Cancellation> active = new HashMap<>();

  static synchronized void configureAll(Context c) throws Exception {
    Vault.data(c);
    JobScheduler scheduler = c.getSystemService(JobScheduler.class);
    scheduler.cancel(72);
    Map<Integer, JobInfo> pending = new HashMap<>();
    for (JobInfo j : scheduler.getAllPendingJobs())
      if (j.getService().getClassName().equals(ScheduleService.class.getName()))
        pending.put(j.getId(), j);
    Set<Integer> wanted = new HashSet<>();
    for (BackupJob j : JobStore.all(c)) {
      if (Settings.prefs(c).getBoolean("imported_schedule_hold", false)) continue;
      if (!j.scheduled()) {
        ScheduleState.remove(c, j.id);
        continue;
      }
      long due = ScheduleState.due(c, j, false);
      wanted.add(j.schedulerId);
      String hash =
          android.util.Base64.encodeToString(
              java.security.MessageDigest.getInstance("SHA-256")
                  .digest(j.json().toString().getBytes(java.nio.charset.StandardCharsets.UTF_8)),
              android.util.Base64.NO_WRAP);
      JobInfo old = pending.get(j.schedulerId);
      if (old != null
          && hash.equals(old.getExtras().getString("config_hash", ""))
          && due == old.getExtras().getLong("due", 0)) continue;
      PersistableBundle extras = new PersistableBundle();
      extras.putString("job_id", j.id);
      extras.putString("config_hash", hash);
      extras.putLong("due", due);
      JobInfo.Builder b =
          new JobInfo.Builder(j.schedulerId, new ComponentName(c, ScheduleService.class))
              .setPersisted(true)
              .setMinimumLatency(Math.max(0, due - System.currentTimeMillis()))
              .setBackoffCriteria(30000, JobInfo.BACKOFF_POLICY_EXPONENTIAL)
              .setRequiresCharging(j.charging)
              .setExtras(extras);
      if (j.wifi && Build.VERSION.SDK_INT >= 28)
        b.setRequiredNetwork(
            new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build());
      else
        b.setRequiredNetworkType(
            j.wifi ? JobInfo.NETWORK_TYPE_UNMETERED : JobInfo.NETWORK_TYPE_ANY);
      if (scheduler.schedule(b.build()) != JobScheduler.RESULT_SUCCESS)
        throw new Exception("Android refused the schedule for " + j.name);
    }
    for (int id : pending.keySet()) if (!wanted.contains(id)) scheduler.cancel(id);
  }

  private static synchronized void completed(Context c, String id, long occurrence)
      throws Exception {
    // Reload after the transfer: a deleted/disabled job must not regain a
    // schedule, and a stale callback must not advance a newer occurrence.
    for (BackupJob current : JobStore.all(c)) {
      if (current.id.equals(id) && current.scheduled() && ScheduleState.isDue(c, id, occurrence)) {
        ScheduleState.due(c, current, true);
        break;
      }
    }
    configureAll(c);
  }

  @Override
  public boolean onStartJob(JobParameters params) {
    Operations.Cancellation token = new Operations.Cancellation();
    synchronized (active) {
      active.put(params.getJobId(), token);
    }
    new Thread(
            () -> {
              boolean retry = false;
              BackupJob job = null;
              try {
                job = JobStore.get(this, params.getExtras().getString("job_id", ""));
                if (job.scheduled()
                    && ScheduleState.isDue(this, job.id, params.getExtras().getLong("due", 0))
                    && !Settings.prefs(this).getBoolean("imported_schedule_hold", false))
                  Settings.status(
                      this,
                      Operations.runJob(
                          this,
                          job,
                          null,
                          null,
                          0,
                          "[\".\"]",
                          "fail",
                          (stage, files, bytes) -> {},
                          token));
              } catch (Operations.BusyException e) {
                retry = true;
              } catch (Exception e) {
                Settings.status(this, "Scheduled backup failed: " + e.getMessage());
              }
              synchronized (active) {
                if (active.get(params.getJobId()) == token) {
                  active.remove(params.getJobId());
                  jobFinished(params, retry);
                  if (!retry && job != null && job.scheduled()) {
                    try {
                      completed(this, job.id, params.getExtras().getLong("due", 0));
                    } catch (Exception e) {
                      Settings.status(
                          this, "Could not schedule the next backup: " + e.getMessage());
                    }
                  }
                }
              }
            },
            "pbsx-scheduled-" + params.getJobId())
        .start();
    return true;
  }

  @Override
  public boolean onStopJob(JobParameters params) {
    synchronized (active) {
      Operations.Cancellation token = active.remove(params.getJobId());
      if (token != null) token.cancel();
    }
    return true;
  }
}
