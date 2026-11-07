package com.pbsx.android;

import android.content.Context;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;
import android.system.ErrnoException;
import android.system.Os;
import android.system.OsConstants;
import android.system.StructPollfd;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.file.Files;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;

/** Temporary exports stay in private cache and are removed on failure, close or process restart. */
final class ExportCache implements AutoCloseable {
  interface Progress {
    void phase(String text);
  }

  private final File directory;
  final Map<String, File> files = new LinkedHashMap<>();

  ExportCache(Context c) throws Exception {
    File root = new File(c.getCacheDir(), "device-exports");
    if (!root.exists() && !root.mkdir())
      throw new Exception("Could not create private export cache");
    directory = new File(root, UUID.randomUUID().toString());
    if (!directory.mkdir()) throw new Exception("Could not create private export cache");
  }

  void prepare(
      IPrivilegedFiles service,
      org.json.JSONArray categories,
      Operations.Cancellation cancellation,
      Progress progress)
      throws Exception {
    if (android.os.Build.VERSION.SDK_INT < 31)
      throw new Exception("Device exports require Android 12 or later");
    long total = 0;
    for (int i = 0; i < categories.length(); i++) {
      cancellation.check();
      String category = categories.getString(i);
      if (!DeviceExport.CATEGORIES.contains(category)) throw new Exception("Unknown device export");
      String phase = "Exporting " + category;
      progress.phase(phase);
      File file = new File(directory, category + ".zip");
      long deadline = SystemClock.elapsedRealtime() + 10 * 60 * 1000;
      long update = 0;
      try (ParcelFileDescriptor descriptor = service.openExport(category);
          FileOutputStream output = new FileOutputStream(file)) {
        if (descriptor == null || !descriptor.canDetectErrors())
          throw new Exception("Export did not return a reliable stream");
        Os.fcntlInt(descriptor.getFileDescriptor(), OsConstants.F_SETFL, OsConstants.O_NONBLOCK);
        StructPollfd poll = new StructPollfd();
        poll.fd = descriptor.getFileDescriptor();
        poll.events = (short) OsConstants.POLLIN;
        byte[] bytes = new byte[65536];
        while (true) {
          cancellation.check();
          if (SystemClock.elapsedRealtime() > deadline)
            throw new Exception("Device export timed out; try fewer categories");
          poll.revents = 0;
          if (Os.poll(new StructPollfd[] {poll}, 250) == 0) continue;
          int count;
          try {
            count = Os.read(descriptor.getFileDescriptor(), bytes, 0, bytes.length);
          } catch (ErrnoException e) {
            if (e.errno == OsConstants.EAGAIN || e.errno == OsConstants.EINTR) continue;
            throw e;
          }
          if (count == 0) {
            descriptor.checkError();
            break;
          }
          total += count;
          if (total > DeviceExport.MAX_BYTES)
            throw new Exception("Combined device exports exceed 512 MiB; choose fewer categories");
          if (directory.getUsableSpace() < 32L * 1024 * 1024)
            throw new Exception("Not enough space for temporary device exports");
          output.write(bytes, 0, count);
          if (SystemClock.elapsedRealtime() - update > 500) {
            update = SystemClock.elapsedRealtime();
            progress.phase(phase + " · " + (total / 1024) + " KiB prepared");
          }
        }
      }
      if (file.length() == 0) throw new Exception("Device export produced no archive");
      try (java.util.zip.ZipFile archive = new java.util.zip.ZipFile(file)) {
        if (archive.getEntry("manifest.json") == null)
          throw new Exception("Device export is incomplete");
      }
      files.put(file.getName(), file);
    }
  }

  static void remove(File file) {
    if (!Files.isSymbolicLink(file.toPath()) && file.isDirectory()) {
      File[] children = file.listFiles();
      if (children != null) for (File child : children) remove(child);
    }
    file.delete();
  }

  static void clearStale(Context c) {
    remove(new File(c.getCacheDir(), "device-exports"));
  }

  public void close() {
    remove(directory);
    files.clear();
  }
}
