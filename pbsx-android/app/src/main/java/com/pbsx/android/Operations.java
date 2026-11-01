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

  static boolean running() {
    return running.get();
  }

  static void cancel() {
    Cancellation operation = active;
    if (operation != null) operation.cancel();
  }

  static Client client(Context context, String config, Cancellation cancellation) throws Exception {
    checkNetwork(context);
    Client client = Mobilecore.newClient(config);
    ConnectivityManager manager = context.getSystemService(ConnectivityManager.class);
    final boolean wifiOnly = Settings.prefs(context).getBoolean("wifi", true);
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
    ConnectivityManager manager = c.getSystemService(ConnectivityManager.class);
    NetworkCapabilities caps = manager.getNetworkCapabilities(manager.getActiveNetwork());
    if (caps == null || !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET))
      throw new Exception("No network connection");
    if (Settings.prefs(c).getBoolean("wifi", true)
        && !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))
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
    if (!running.compareAndSet(false, true))
      throw new Exception("Another backup or restore is already running");
    ConnectivityManager manager = context.getSystemService(ConnectivityManager.class);
    ConnectivityManager.NetworkCallback callback = null;
    active = cancellation;
    try {
      cancellation.check();
      checkNetwork(context);
      String config = Vault.load(context);
      if (config.isEmpty()) throw new Exception("Enroll or configure an API key first");
      Client client = client(context, config, cancellation);
      cancellation.client = client;
      cancellation.check();
      if (Settings.prefs(context).getBoolean("wifi", true)) {
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
        String folder = "PBS-X-restore-" + System.currentTimeMillis();
        DocumentTree parent =
            new DocumentTree(context.getContentResolver(), Uri.parse(destination));
        try (AndroidDocuments docs = new AndroidDocuments(parent.freshRestoreDirectory(folder))) {
          client.restore(
              Settings.store(context),
              Settings.namespace(context),
              source,
              snapshot,
              docs,
              progress);
        }
        return "Restored to " + folder;
      }
      android.content.SharedPreferences p = Settings.prefs(context);
      try (AndroidDocuments docs =
          new AndroidDocuments(
              context,
              Settings.sources(context),
              p.getBoolean("shizuku", false),
              p.getBoolean("apks", false),
              p.getBoolean("data", false),
              p.getBoolean("obb", false))) {
        return "Backup complete: "
            + client.backup(Settings.store(context), Settings.namespace(context), docs, progress);
      }
    } finally {
      if (callback != null)
        try {
          manager.unregisterNetworkCallback(callback);
        } catch (RuntimeException ignored) {
        }
      cancellation.client = null;
      active = null;
      running.set(false);
    }
  }
}
