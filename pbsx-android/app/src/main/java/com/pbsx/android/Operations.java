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

final class Operations {
  private static final AtomicBoolean running = new AtomicBoolean();
  private static volatile Cancellation active;
  private static volatile String activeName = "";
  private static volatile String activeJobId = "";
  private static volatile String activeRunId = "";

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

  static final class Cancellation {
    private volatile boolean cancelled;
    private volatile Client client;

    void cancel() {
      cancelled = true;
      Client c = client;
      if (c != null) c.cancel();
    }

    void check() throws Exception {
      if (cancelled) throw new Exception("Operation cancelled");
    }
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
      BackupJob job,
      String destination,
      String source,
      String sourceType,
      long snapshot,
      String selections,
      String mode,
      Progress progress,
      Cancellation cancellation)
      throws Exception {
    job.validate(destination == null);
    if (!running.compareAndSet(false, true)) throw new BusyException();
    ConnectivityManager manager = context.getSystemService(ConnectivityManager.class);
    ConnectivityManager.NetworkCallback callback = null;
    active = cancellation;
    activeName = destination == null ? job.name : "Restore files";
    activeJobId = job.id;
    TransferState.begin(destination != null);
    String runId = null;
    try {
      runId = JobStore.start(context, job, destination == null ? "backup" : "restore");
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
            if (isActive(cancellation) && TransferState.accept(value)) publish.run();
          });
      cancellation.check();
      if (job.wifi) {
        final Network selected = manager.getActiveNetwork();
        callback =
            new ConnectivityManager.NetworkCallback() {
              @Override
              public void onLost(Network network) {
                if (network.equals(selected)) cancellation.cancel();
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
        DocumentTree parent =
            new DocumentTree(context.getContentResolver(), Uri.parse(destination));
        DocumentTree output = mode.equals("fail") ? parent.freshRestoreDirectory(folder) : parent;
        String result;
        try (AndroidDocuments docs = new AndroidDocuments(output)) {
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
      TransferState.phase("Preparing selected sources");
      publish.run();
      try (AndroidDocuments docs =
          new AndroidDocuments(
              context,
              job,
              cancellation,
              phase -> {
                TransferState.phase(phase);
                publish.run();
              })) {
        org.json.JSONObject backup =
            new org.json.JSONObject(
                client.backupGroupJob(
                    job.store, job.namespace, job.backupID(context), job.id, job.name, docs, null));
        String result = "Backup complete: " + job.name + " · " + backup.getString("snapshot");
        if (!backup.optString("warning").isEmpty()) result += " · " + backup.getString("warning");
        record(context, runId, true, result);
        return result;
      }
    } catch (Exception e) {
      if (runId != null)
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

  private static void record(Context c, String id, boolean success, String message) {
    try {
      JobStore.finish(c, id, success, message);
    } catch (Exception ignored) {
    }
  }
}
