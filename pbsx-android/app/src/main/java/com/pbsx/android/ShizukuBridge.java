package com.pbsx.android;

import android.content.ComponentName;
import android.content.Context;
import android.content.ServiceConnection;
import android.content.pm.PackageManager;
import android.os.IBinder;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import rikka.shizuku.Shizuku;

final class ShizukuBridge implements AutoCloseable {
  private volatile IPrivilegedFiles files;
  private volatile boolean closed;
  private final CountDownLatch ready = new CountDownLatch(1);
  private final Shizuku.UserServiceArgs args;
  private final ServiceConnection connection =
      new ServiceConnection() {
        public void onServiceConnected(ComponentName name, IBinder binder) {
          if (!closed) files = IPrivilegedFiles.Stub.asInterface(binder);
          ready.countDown();
        }

        public void onServiceDisconnected(ComponentName name) {
          files = null;
          ready.countDown();
        }
      };

  ShizukuBridge(Context context) throws Exception {
    this(context, PrivilegedFiles.class);
  }

  ShizukuBridge(Context context, Class<?> service) throws Exception {
    if (!Shizuku.pingBinder() || Shizuku.checkSelfPermission() != PackageManager.PERMISSION_GRANTED)
      throw new Exception("Start Shizuku and grant PBS-X permission in Settings");
    args =
        new Shizuku.UserServiceArgs(new ComponentName(context, service))
            // Removal is asynchronous. Each operation needs its own connection
            // identity so the previous reader's death cannot cancel a new one.
            .tag("pbsx-reader-" + UUID.randomUUID())
            .daemon(false)
            .processNameSuffix("backup-reader")
            .debuggable(BuildConfig.DEBUG)
            .version(BuildConfig.VERSION_CODE);
    Shizuku.bindUserService(args, connection);
    try {
      if (!ready.await(15, TimeUnit.SECONDS) || files == null)
        throw new Exception("Shizuku reader did not start");
    } catch (Exception e) {
      close();
      throw e;
    }
  }

  IPrivilegedFiles files() throws Exception {
    if (closed || files == null) throw new Exception("Shizuku disconnected; restart it and retry");
    return files;
  }

  @Override
  public void close() {
    if (closed) return;
    closed = true;
    try {
      Shizuku.unbindUserService(args, connection, true);
    } catch (RuntimeException ignored) {
    }
    files = null;
  }
}
