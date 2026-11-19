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

/** Exports are ephemeral for legacy callers; pending runs retain verified private copies. */
final class ExportCache implements AutoCloseable {
  interface Progress {
    void phase(String text);
  }

  private final File directory;
  private boolean persistent;
  private boolean requireExisting;
  final Map<String, File> files = new LinkedHashMap<>();

  ExportCache(Context c) throws Exception {
    File root = new File(c.getCacheDir(), "device-exports");
    if (!root.exists() && !root.mkdir())
      throw new Exception("Could not create private export cache");
    directory = new File(root, UUID.randomUUID().toString());
    if (!directory.mkdir()) throw new Exception("Could not create private export cache");
  }

  ExportCache(Context c, String runID, boolean requireExisting) throws Exception {
    if (!runID.matches("[a-f0-9-]{36}")) throw new Exception("Invalid export run identity");
    File root = new File(c.getFilesDir(), "pending-exports");
    directory = new File(root, runID);
    if (!directory.isDirectory() && !directory.mkdirs())
      throw new Exception("Cannot create private export storage");
    persistent = true;
    this.requireExisting = requireExisting;
  }

  private static String checksum(File file) throws Exception {
    java.security.MessageDigest hash = java.security.MessageDigest.getInstance("SHA-256");
    try (java.io.FileInputStream input = new java.io.FileInputStream(file)) {
      byte[] bytes = new byte[65536];
      int count;
      while ((count = input.read(bytes)) != -1) hash.update(bytes, 0, count);
    }
    StringBuilder out = new StringBuilder();
    for (byte b : hash.digest()) out.append(String.format(java.util.Locale.ROOT, "%02x", b & 255));
    return out.toString();
  }

  private boolean reuse(File file) throws Exception {
    File marker = new File(directory, file.getName() + ".complete");
    if (!file.isFile() || !marker.isFile()) {
      if (requireExisting)
        throw new Exception(
            "A prepared device export is missing; cancel this backup before creating new exports");
      return false;
    }
    if (marker.length() > 128) throw new Exception("Invalid prepared export marker");
    String expected =
        new String(Files.readAllBytes(marker.toPath()), java.nio.charset.StandardCharsets.UTF_8);
    if (marker.length() > 128 || !expected.equals(file.length() + ":" + checksum(file)))
      throw new Exception(
          "A prepared device export changed; cancel this backup before creating new exports");
    files.put(file.getName(), file);
    return true;
  }

  private void complete(File file) throws Exception {
    if (!persistent) return;
    android.util.AtomicFile marker =
        new android.util.AtomicFile(new File(directory, file.getName() + ".complete"));
    FileOutputStream output = marker.startWrite();
    try {
      output.write(
          (file.length() + ":" + checksum(file)).getBytes(java.nio.charset.StandardCharsets.UTF_8));
      output.getFD().sync();
      marker.finishWrite(output);
      syncDirectory(directory);
    } catch (Exception e) {
      marker.failWrite(output);
      throw e;
    }
  }

  private static void syncDirectory(File directory) throws Exception {
    java.io.FileDescriptor fd = Os.open(directory.getAbsolutePath(), OsConstants.O_RDONLY, 0);
    try {
      Os.fsync(fd);
    } finally {
      Os.close(fd);
    }
  }

  static void discard(Context c, String runID) throws Exception {
    if (!runID.matches("[a-f0-9-]{36}")) throw new Exception("Invalid export run identity");
    remove(new File(new File(c.getFilesDir(), "pending-exports"), runID));
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
      if (persistent && reuse(file)) {
        total += file.length();
        if (total > DeviceExport.MAX_BYTES)
          throw new Exception("Combined device exports exceed 512 MiB");
        continue;
      }
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
        output.getFD().sync();
      }
      if (file.length() == 0) throw new Exception("Device export produced no archive");
      try (java.util.zip.ZipFile archive = new java.util.zip.ZipFile(file)) {
        if (archive.getEntry("manifest.json") == null)
          throw new Exception("Device export is incomplete");
      }
      complete(file);
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
    if (!persistent) remove(directory);
    files.clear();
  }
}
