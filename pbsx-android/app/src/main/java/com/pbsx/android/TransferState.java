package com.pbsx.android;

import android.content.Context;
import android.os.SystemClock;
import android.text.format.Formatter;
import org.json.JSONObject;

/** Process-local transfer state survives screen recreation, without writing every read to disk. */
final class TransferState {
  static final class Snapshot {
    final String stage, path;
    final long files, bytes, fileBytes, fileTotal, uploaded, started;
    final boolean restore;

    Snapshot(
        String stage,
        String path,
        long files,
        long bytes,
        long fileBytes,
        long fileTotal,
        long uploaded,
        long started,
        boolean restore) {
      this.stage = stage;
      this.path = path;
      this.files = files;
      this.bytes = bytes;
      this.fileBytes = fileBytes;
      this.fileTotal = fileTotal;
      this.uploaded = uploaded;
      this.started = started;
      this.restore = restore;
    }

    boolean hasFileProgress() {
      return fileTotal > 0 && (stage.equals("Backing up") || stage.equals("Restoring"));
    }

    int percent() {
      return hasFileProgress() ? (int) Math.min(100, 100.0 * fileBytes / fileTotal) : 0;
    }

    String counts(Context c) {
      String text =
          files
              + " files · "
              + Formatter.formatFileSize(c, bytes)
              + (restore ? " restored" : " read");
      if (!restore) text += " · " + Formatter.formatFileSize(c, uploaded) + " uploaded";
      return text;
    }

    String summary(Context c) {
      return stage + (path.isEmpty() ? "" : "\n" + path) + "\n" + counts(c);
    }

    String barLabel() {
      return hasFileProgress()
          ? "Current file · " + percent() + "%"
          : "Working · " + Math.max(0, (SystemClock.elapsedRealtime() - started) / 1000) + "s";
    }
  }

  private static volatile Snapshot current =
      new Snapshot("Preparing transfer", "", 0, 0, 0, -1, 0, 0, false);

  static Snapshot current() {
    return current;
  }

  static synchronized void begin(boolean restore) {
    current =
        new Snapshot(
            "Checking connection", "", 0, 0, 0, -1, 0, SystemClock.elapsedRealtime(), restore);
  }

  static synchronized void phase(String stage) {
    Snapshot s = current;
    current = new Snapshot(stage, "", s.files, s.bytes, 0, -1, s.uploaded, s.started, s.restore);
  }

  static synchronized boolean accept(String value) {
    try {
      JSONObject j = new JSONObject(value);
      Snapshot old = current;
      current =
          new Snapshot(
              j.getString("stage"),
              j.optString("path"),
              Math.max(0, j.getLong("files")),
              Math.max(0, j.getLong("bytes")),
              Math.max(0, j.getLong("file_bytes")),
              j.getLong("file_total"),
              Math.max(0, j.getLong("uploaded_bytes")),
              old.started,
              old.restore);
      return true;
    } catch (Exception ignored) {
      return false;
    }
  }
}
