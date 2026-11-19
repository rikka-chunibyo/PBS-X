package com.pbsx.android;

import android.content.Context;
import android.os.ParcelFileDescriptor;

/** App-side facade; privileged writes stay inside one selected, descriptor-anchored root. */
final class PrivilegedDocuments implements RestoreOutput {
  private final ShizukuBridge bridge;

  PrivilegedDocuments(Context c, String root, String fresh) throws Exception {
    bridge = new ShizukuBridge(c);
    try {
      bridge.files().beginRestore(root, fresh);
    } catch (Exception e) {
      bridge.close();
      throw e;
    }
  }

  private IPrivilegedFiles files() throws Exception {
    return bridge.files();
  }

  public String statJSON(String path) throws Exception {
    return files().stat(path);
  }

  public String listJSON(String path) throws Exception {
    return files().list(path);
  }

  public String inspectJSON(String path) throws Exception {
    return files().inspect(path);
  }

  public void makeDirectory(String path) throws Exception {
    files().makeDirectory(path);
  }

  public void commitFile(String temporary, String path, boolean replace) throws Exception {
    files().commitFile(temporary, path, replace);
  }

  public void deleteFile(String path) throws Exception {
    files().deleteStaged(path);
  }

  public long openRead(String path) throws Exception {
    return descriptor(files().openRead(path));
  }

  public long openWrite(String path) throws Exception {
    return descriptor(files().openWrite(path));
  }

  private static long descriptor(ParcelFileDescriptor fd) throws Exception {
    if (fd == null) throw new Exception("Shizuku returned no file descriptor");
    return fd.detachFd();
  }

  public void close() {
    bridge.close();
  }
}
