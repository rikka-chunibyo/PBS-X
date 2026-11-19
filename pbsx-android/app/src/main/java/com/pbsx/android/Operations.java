package com.pbsx.android;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import com.pbsx.core.mobilecore.Client;
import com.pbsx.core.mobilecore.Mobilecore;
import com.pbsx.core.mobilecore.Progress;
import java.util.concurrent.atomic.AtomicBoolean;
import org.json.JSONObject;

final class Operations {
  private static final AtomicBoolean running = new AtomicBoolean();
  private static volatile Cancellation active;
  private static volatile String activeName = "";
  private static volatile String activeJobId = "";
  private static volatile String activeRunId = "";

  static boolean canPause() {
    return active != null && !TransferState.current().restore;
  }

  static String activeJobId() {
    return activeJobId;
  }

  static String activeRunId() {
    return activeRunId;
  }

  static boolean isActive(Cancellation token) {
    return running.get() && active == token;
  }

  static String activeName() {
    return activeName;
  }

  static final class BusyException extends Exception {
    BusyException() {
      super("Another backup or restore is already running");
    }
  }

  static final class PauseException extends Exception {
    PauseException(String reason) {
      super(reason);
    }
  }

  static final class Cancellation {
    private final java.util.concurrent.atomic.AtomicInteger mode =
        new java.util.concurrent.atomic.AtomicInteger();
    private volatile Client client;
    private volatile boolean automatic;
    volatile boolean scheduled;
    volatile long occurrence;
    private volatile String reason = "Paused by you";

    boolean cancelled() {
      return mode.get() == 2;
    }

    boolean paused() {
      return mode.get() == 1;
    }

    synchronized void cancel() {
      mode.set(2);
      Client c = client;
      if (c != null) c.cancel();
    }

    synchronized void pause(String reason, boolean automatic) {
      if (mode.get() != 0) return;
      this.reason = reason;
      this.automatic = automatic;
      mode.set(1);
      Client c = client;
      if (c != null) c.pause();
    }

    void check() throws Exception {
      if (cancelled()) throw new Exception("Operation cancelled");
      if (paused()) throw new PauseException(reason);
    }
  }

  static void pause(String reason, boolean automatic) {
    Cancellation operation = active;
    if (operation != null && !TransferState.current().restore) operation.pause(reason, automatic);
  }

  interface SettingsChange {
    void run() throws Exception;
  }

  static void changeSettings(SettingsChange action) throws Exception {
    if (!running.compareAndSet(false, true)) throw new BusyException();
    try {
      action.run();
    } finally {
      running.set(false);
    }
  }

  static boolean running() {
    return running.get();
  }

  static void cancel() {
    Cancellation operation = active;
    if (operation != null) operation.cancel();
  }

  static Client client(Context context, String config, Cancellation cancellation) throws Exception {
    return client(context, config, cancellation, Settings.prefs(context).getBoolean("wifi", true));
  }

  static Client client(Context context, String config, Cancellation cancellation, boolean wifiOnly)
      throws Exception {
    checkNetwork(context, wifiOnly);
    Client client = Mobilecore.newClient(config);
    ConnectivityManager manager = context.getSystemService(ConnectivityManager.class);
    final Network network = wifiOnly ? manager.getActiveNetwork() : null;
    if (wifiOnly) {
      if (network == null) throw new Exception("Wi-Fi connection was lost");
      NetworkCapabilities caps = manager.getNetworkCapabilities(network);
      if (caps == null || !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))
        throw new Exception("Wi-Fi connection was lost");
    }
    client.setSocketBinder(
        fd -> {
          if (cancellation != null) cancellation.check();
          if (network != null) {
            NetworkCapabilities caps = manager.getNetworkCapabilities(network);
            if (caps == null || !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))
              throw new Exception("Wi-Fi connection was lost");
            try (ParcelFileDescriptor copy = ParcelFileDescriptor.fromFd((int) fd)) {
              network.bindSocket(copy.getFileDescriptor());
            }
          }
        });
    return client;
  }

  static void checkNetwork(Context c) throws Exception {
    checkNetwork(c, Settings.prefs(c).getBoolean("wifi", true));
  }

  static void checkNetwork(Context c, boolean wifiOnly) throws Exception {
    ConnectivityManager manager = c.getSystemService(ConnectivityManager.class);
    NetworkCapabilities caps = manager.getNetworkCapabilities(manager.getActiveNetwork());
    if (caps == null || !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET))
      throw new Exception("No network connection");
    if (wifiOnly && !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))
      throw new Exception("Wi-Fi-only is enabled; connect to Wi-Fi or change Settings");
  }

  static String run(
      Context context,
      String destination,
      String source,
      long snapshot,
      Progress progress,
      Cancellation cancellation)
      throws Exception {
    java.util.List<BackupJob> jobs = JobStore.all(context);
    BackupJob job;
    if (jobs.isEmpty()) {
      if (destination == null) throw new Exception("Create a backup job first");
      job = new BackupJob();
      job.store = Settings.store(context);
      job.namespace = Settings.namespace(context);
    } else job = jobs.get(0);
    return runJob(
        context, job, destination, source, snapshot, "[\".\"]", "fail", progress, cancellation);
  }

  static String runJob(
      Context context,
      BackupJob job,
      String destination,
      String source,
      long snapshot,
      String selections,
      String mode,
      Progress progress,
      Cancellation cancellation)
      throws Exception {
    return runGroupJob(
        context,
        job,
        destination,
        source,
        "host",
        snapshot,
        selections,
        mode,
        progress,
        cancellation);
  }

  static String runGroupJob(
      Context context,
      BackupJob requestedJob,
      String destination,
      String source,
      String sourceType,
      long snapshot,
      String selections,
      String mode,
      Progress progress,
      Cancellation cancellation)
      throws Exception {
    if (!running.compareAndSet(false, true)) throw new BusyException();
    JSONObject pending = null;
    BackupJob job = requestedJob;
    ConnectivityManager manager = context.getSystemService(ConnectivityManager.class);
    ConnectivityManager.NetworkCallback callback = null;
    active = cancellation;
    activeName = destination == null ? requestedJob.name : "Restore files";
    activeJobId = requestedJob.id;
    TransferState.begin(destination != null);
    String runId = null;
    try {
      // Reserve the transfer before reading settings/pending state. Complete an
      // interrupted settings import before deciding whether this is a new run.
      Vault.data(context);
      pending = destination == null ? PendingBackups.get(context, requestedJob.id) : null;
      if (destination == null
          && cancellation.scheduled
          && (Settings.prefs(context).getBoolean("imported_schedule_hold", false)
              || (pending != null && !pending.optBoolean("automatic"))))
        return "Backup held until you resume or cancel its saved run";
      job =
          pending == null
              ? (destination == null ? JobStore.get(context, requestedJob.id) : requestedJob)
              : BackupJob.from(pending.getJSONObject("job"));
      job.validate(destination == null);
      if (destination == null) KeyProfiles.validateJob(context, job);
      activeName = destination == null ? job.name : "Restore files";
      runId =
          pending == null
              ? JobStore.start(context, job, destination == null ? "backup" : "restore")
              : pending.getString("history");
      if (pending != null) JobStore.state(context, runId, "running", "Resuming saved backup");
      activeRunId = runId;
      final String trackedRun = runId;
      java.util.concurrent.atomic.AtomicLong lastPublish =
          new java.util.concurrent.atomic.AtomicLong();
      Runnable publish =
          () -> {
            if (!isActive(cancellation)) return;
            long now = android.os.SystemClock.elapsedRealtime(), before = lastPublish.get();
            if (now - before < 500 || !lastPublish.compareAndSet(before, now)) return;
            TransferState.Snapshot state = TransferState.current();
            String summary = state.summary(context);
            Settings.status(context, summary);
            try {
              JobStore.progress(context, trackedRun, summary);
            } catch (Exception ignored) {
            }
            if (progress != null) progress.update(summary, state.files, state.bytes);
          };
      publish.run();
      cancellation.check();
      checkNetwork(context, job.wifi);
      String config =
          KeyProfiles.config(context, job.connectionID, job.encryptionKeyID, destination == null);
      if (config.isEmpty()) throw new Exception("Enroll or configure an API key first");
      Client client = client(context, config, cancellation, job.wifi);
      cancellation.client = client;
      client.setProgressDetails(
          value -> {
            if (cancellation.cancelled()) client.cancel();
            else if (destination == null && cancellation.paused()) client.pause();
            if (isActive(cancellation) && TransferState.accept(value)) publish.run();
          });
      cancellation.check();
      if (job.wifi) {
        final Network selected = manager.getActiveNetwork();
        callback =
            new ConnectivityManager.NetworkCallback() {
              @Override
              public void onLost(Network network) {
                if (network.equals(selected)) {
                  if (destination == null)
                    cancellation.pause("Paused: Wi-Fi connection lost", true);
                  else cancellation.cancel();
                }
              }
            };
        manager.registerNetworkCallback(
            new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .build(),
            callback);
      }
      if (destination != null) {
        TransferState.phase("Preparing restore destination");
        publish.run();
        String folder = "PBS-X-restore-" + System.currentTimeMillis();
        Uri target = Uri.parse(destination);
        RestoreOutput output;
        if ("pbsx-shizuku".equals(target.getScheme())) {
          output =
              new PrivilegedDocuments(
                  context, target.getSchemeSpecificPart(), mode.equals("fail") ? folder : "");
        } else {
          if (!"content".equals(target.getScheme()))
            throw new Exception("Invalid restore destination");
          DocumentTree parent = new DocumentTree(context.getContentResolver(), target);
          output =
              new AndroidDocuments(
                  mode.equals("fail") ? parent.freshRestoreDirectory(folder) : parent);
        }
        String result;
        try (RestoreOutput docs = output) {
          org.json.JSONObject stats =
              new org.json.JSONObject(
                  client.restoreGroupSelected(
                      job.store,
                      job.namespace,
                      sourceType,
                      source,
                      snapshot,
                      selections,
                      mode,
                      docs,
                      null));
          result =
              "Restored "
                  + stats.getLong("files")
                  + " files · "
                  + stats.getLong("skipped")
                  + " skipped/unchanged · "
                  + stats.getLong("renamed")
                  + " renamed"
                  + (mode.equals("fail") ? " to " + folder : "");
        }
        record(context, runId, true, result);
        return result;
      }
      String device = deviceIdentity(context);
      if (pending != null && !device.equals(pending.getString("device")))
        throw new Exception(
            "This pending backup came from another installation; its source access must be reviewed"
                + " before resuming");
      if (pending == null) {
        // Freeze the effective connection and backup ID, including jobs which use defaults.
        BackupJob frozen = BackupJob.from(job.json());
        frozen.connectionID = KeyProfiles.connection(context, job.connectionID).getString("id");
        frozen.backupID = job.backupID(context);
        String sourceIdentity = PendingBackups.sourceIdentity(device, frozen);
        String descriptor =
            client.newBackupRun(
                frozen.store,
                frozen.namespace,
                frozen.backupID,
                frozen.id,
                frozen.name,
                sourceIdentity);
        pending =
            new JSONObject()
                .put("job", frozen.json())
                .put("history", runId)
                .put("source", sourceIdentity)
                .put("device", device)
                .put("descriptor", descriptor)
                .put("state", "running")
                .put("automatic", cancellation.scheduled)
                .put("occurrence", cancellation.occurrence);
        PendingBackups.put(context, pending);
      }
      if ("cancelling".equals(pending.optString("state")))
        return cancelPending(context, pending, job.wifi);
      pending.put("state", "running").put("automatic", cancellation.scheduled);
      PendingBackups.put(context, pending);
      final JSONObject savedRun = pending;
      String completed =
          client.completedBackupRun(pending.getString("descriptor"), pending.getString("source"));
      if (!completed.isEmpty())
        return finishBackup(context, job, runId, pending, new JSONObject(completed));
      cancellation.check();
      TransferState.phase("Preparing selected sources");
      publish.run();
      try (AndroidDocuments docs =
          new AndroidDocuments(
              context,
              BackupJob.from(pending.getJSONObject("job")),
              cancellation,
              phase -> {
                TransferState.phase(phase);
                publish.run();
              },
              PrivilegedFiles.class,
              pending)) {
        cancellation.check();
        JSONObject backup =
            new JSONObject(
                client.backupGroupResumable(
                    pending.getString("descriptor"),
                    pending.getString("source"),
                    docs,
                    descriptor -> {
                      savedRun.put("descriptor", descriptor);
                      PendingBackups.put(context, savedRun);
                      if (cancellation.cancelled()) client.cancel();
                      else if (cancellation.paused()) client.pause();
                    },
                    null));
        if (backup.optBoolean("paused")) {
          if (cancellation.cancelled()) throw new Exception("Operation cancelled");
          String message = cancellation.paused() ? cancellation.reason : "Backup paused";
          if (!backup.optString("warning").isEmpty())
            message += " · " + backup.getString("warning");
          hold(context, pending, cancellation.automatic, "paused", message);
          return message;
        }
        return finishBackup(context, job, runId, pending, backup);
      }
    } catch (Exception e) {
      if (destination == null && pending != null) {
        if (cancellation.cancelled()) {
          try {
            return cancelPending(context, pending, job.wifi);
          } catch (Exception cleanup) {
            String message =
                "Cancellation pending: "
                    + cleanup.getMessage()
                    + ". Recovery information was kept; retry Cancel when connected.";
            hold(context, pending, false, "cancelling", message);
            throw new Exception(message, cleanup);
          }
        }
        if (cancellation.paused()) {
          hold(context, pending, cancellation.automatic, "paused", cancellation.reason);
          return cancellation.reason;
        }
        hold(
            context,
            pending,
            false,
            "failed",
            e.getMessage() == null ? "Backup failed; saved checkpoint retained" : e.getMessage());
      } else if (runId != null)
        record(context, runId, false, e.getMessage() == null ? "Operation failed" : e.getMessage());
      throw e;
    } finally {
      if (callback != null)
        try {
          manager.unregisterNetworkCallback(callback);
        } catch (RuntimeException ignored) {
        }
      cancellation.client = null;
      active = null;
      activeName = "";
      activeJobId = "";
      activeRunId = "";
      running.set(false);
    }
  }

  static String deviceIdentity(Context c) throws Exception {
    String id =
        android.provider.Settings.Secure.getString(
            c.getContentResolver(), android.provider.Settings.Secure.ANDROID_ID);
    if (id == null || id.isEmpty()) throw new Exception("Android device identity is unavailable");
    return hash("PBS-X source installation v1\n" + id);
  }

  private static String hash(String value) throws Exception {
    byte[] bytes =
        java.security.MessageDigest.getInstance("SHA-256")
            .digest(value.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    StringBuilder out = new StringBuilder();
    for (byte b : bytes) out.append(String.format(java.util.Locale.ROOT, "%02x", b & 255));
    return out.toString();
  }

  private static void hold(
      Context c, JSONObject run, boolean automatic, String state, String message) throws Exception {
    run.put("state", state).put("automatic", automatic).put("message", message);
    PendingBackups.put(c, run);
    JobStore.state(c, run.getString("history"), state, message);
  }

  private static String finishBackup(
      Context c, BackupJob job, String history, JSONObject run, JSONObject backup)
      throws Exception {
    String result = "Backup complete: " + job.name + " · " + backup.getString("snapshot");
    if (!backup.optString("warning").isEmpty()) result += " · " + backup.getString("warning");
    // Record completion first: a crash before removing the descriptor can verify it again.
    JobStore.finish(c, history, true, result);
    PendingBackups.remove(c, job.id);
    ExportCache.discard(c, run.getString("history"));
    if (run.optLong("occurrence", 0) > 0)
      ScheduleService.completed(c, job.id, run.getLong("occurrence"));
    return result;
  }

  private static String cancelPending(Context c, JSONObject run, boolean wifi) throws Exception {
    BackupJob job = BackupJob.from(run.getJSONObject("job"));
    Client cleanup =
        client(c, KeyProfiles.config(c, job.connectionID, job.encryptionKeyID, true), null, wifi);
    String completed =
        cleanup.completedBackupRun(run.getString("descriptor"), run.getString("source"));
    if (!completed.isEmpty())
      return finishBackup(c, job, run.getString("history"), run, new JSONObject(completed));
    cleanup.discardBackupRun(run.getString("descriptor"), run.getString("source"));
    JobStore.state(
        c,
        run.getString("history"),
        "cancelled",
        "Backup cancelled; incomplete server snapshot removed");
    PendingBackups.remove(c, job.id);
    ExportCache.discard(c, run.getString("history"));
    if (run.optLong("occurrence", 0) > 0)
      ScheduleService.completed(c, job.id, run.getLong("occurrence"));
    return "Backup cancelled";
  }

  static String discardPending(Context c, String jobID) throws Exception {
    if (!running.compareAndSet(false, true)) throw new BusyException();
    try {
      Vault.data(c);
      JSONObject run = PendingBackups.get(c, jobID);
      if (run == null) return "No pending backup";
      BackupJob job = BackupJob.from(run.getJSONObject("job"));
      hold(c, run, false, "cancelling", "Cancellation pending; server cleanup must be confirmed");
      try {
        return cancelPending(c, run, job.wifi);
      } catch (Exception e) {
        hold(c, run, false, "cancelling", "Cancellation pending: " + e.getMessage());
        throw e;
      }
    } finally {
      running.set(false);
    }
  }

  private static void record(Context c, String id, boolean success, String message) {
    try {
      JobStore.finish(c, id, success, message);
    } catch (Exception ignored) {
    }
  }
}
