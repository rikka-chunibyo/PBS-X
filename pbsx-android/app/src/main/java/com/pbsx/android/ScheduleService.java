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

  static void configureAll(Context c) throws Exception {
    synchronized (KeyProfiles.class) {
      synchronized (JobStore.class) {
        configureLocked(c);
      }
    }
  }

  private static void configureLocked(Context c) throws Exception {
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
      org.json.JSONObject run = PendingBackups.get(c, j.id);
      if (PendingBackups.cleanupOnly(run)) run = null;
      if (run != null && !run.optBoolean("automatic")) continue;
      BackupJob constraints = run == null ? j : BackupJob.from(run.getJSONObject("job"));
      long due = ScheduleState.due(c, j, false);
      wanted.add(j.schedulerId);
      // Only changes to the actual OS constraints/due time should replace a job.
      // Creating a checkpoint or editing next-run sources must not stop its worker.
      String hash =
          android.util.Base64.encodeToString(
              java.security.MessageDigest.getInstance("SHA-256")
                  .digest(
                      (j.id + "/" + constraints.wifi + "/" + constraints.charging)
                          .getBytes(java.nio.charset.StandardCharsets.UTF_8)),
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
              .setRequiresCharging(constraints.charging)
              .setExtras(extras);
      if (constraints.wifi && Build.VERSION.SDK_INT >= 28)
        b.setRequiredNetwork(
            new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build());
      else
        b.setRequiredNetworkType(
            constraints.wifi ? JobInfo.NETWORK_TYPE_UNMETERED : JobInfo.NETWORK_TYPE_ANY);
      if (scheduler.schedule(b.build()) != JobScheduler.RESULT_SUCCESS)
        throw new Exception("Android refused the schedule for " + j.name);
    }
    for (int id : pending.keySet()) if (!wanted.contains(id)) scheduler.cancel(id);
  }

  static void completed(Context c, String id, long occurrence) throws Exception {
    advanceCompletedOccurrence(c, id, occurrence);
    configureAll(c);
  }

  static void advanceCompletedOccurrence(Context c, String id, long occurrence) throws Exception {
    synchronized (KeyProfiles.class) {
      synchronized (JobStore.class) {
        // A deleted/disabled job cannot regain a schedule, and an old
        // completion cannot advance an occurrence that has already changed.
        for (BackupJob current : JobStore.all(c)) {
          if (current.id.equals(id) && current.scheduled()) {
            ScheduleState.advanceIfDue(c, current, occurrence);
            return;
          }
        }
      }
    }
  }

  @Override
  public boolean onStartJob(JobParameters params) {
    Operations.Cancellation token = new Operations.Cancellation();
    token.scheduled = true;
    token.occurrence = params.getExtras().getLong("due", 0);
    synchronized (active) {
      active.put(params.getJobId(), token);
    }
    new Thread(
            () -> {
              boolean retry = false;
              boolean recoveringEarlierOccurrence = false;
              BackupJob job = null;
              try {
                job = JobStore.get(this, params.getExtras().getString("job_id", ""));
                if (job.scheduled()
                    && ScheduleState.isDue(this, job.id, token.occurrence)
                    && !Settings.prefs(this).getBoolean("imported_schedule_hold", false)) {
                  org.json.JSONObject saved = PendingBackups.get(this, job.id);
                  long savedOccurrence = saved == null ? 0 : saved.optLong("occurrence");
                  recoveringEarlierOccurrence =
                      PendingBackups.canResume(saved)
                          && savedOccurrence > 0
                          && savedOccurrence != token.occurrence;
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
                }
              } catch (Operations.BusyException e) {
                retry = true;
              } catch (Exception e) {
                Settings.status(this, "Scheduled backup failed: " + e.getMessage());
              }
              boolean owns;
              synchronized (active) {
                owns = active.get(params.getJobId()) == token;
                if (owns) active.remove(params.getJobId());
              }
              if (owns) {
                boolean advance = false;
                try {
                  org.json.JSONObject pending =
                      job == null ? null : PendingBackups.get(this, job.id);
                  if (PendingBackups.cleanupOnly(pending)) pending = null;
                  boolean stillDue =
                      job != null
                          && job.scheduled()
                          && ScheduleState.isDue(this, job.id, token.occurrence);
                  if (!stillDue) retry = false;
                  else if (pending != null || token.paused())
                    retry = pending == null || pending.optBoolean("automatic");
                  else if (recoveringEarlierOccurrence) retry = true;
                  advance = !retry && pending == null && stillDue;
                } catch (Exception e) {
                  Settings.status(this, "Could not update schedule: " + e.getMessage());
                  retry = false;
                }
                jobFinished(params, retry);
                if (!retry) {
                  try {
                    if (advance) completed(this, job.id, token.occurrence);
                    else configureAll(this);
                  } catch (Exception e) {
                    Settings.status(this, "Could not update schedule: " + e.getMessage());
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
      if (token != null)
        token.pause("Paused: Android stopped the scheduled job or its constraints changed", true);
    }
    return true;
  }
}
