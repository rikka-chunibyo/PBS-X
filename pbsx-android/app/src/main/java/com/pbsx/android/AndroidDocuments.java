package com.pbsx.android;

import android.content.Context;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import java.io.File;
import java.util.LinkedHashMap;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

final class AndroidDocuments implements RestoreOutput {
  private final Map<String, DocumentTree> trees = new LinkedHashMap<>();
  private final Map<String, String> privileged = new LinkedHashMap<>();
  private final ShizukuBridge bridge;
  private final DocumentTree output;
  private final ExportCache exports;
  private final Map<String, java.util.Set<String>> selected = new LinkedHashMap<>();
  private final java.util.Set<String> sharedFolders = new java.util.HashSet<>();
  private final java.util.Set<String> allApps = new java.util.HashSet<>();

  AndroidDocuments(
      Context context,
      JSONArray sources,
      boolean useShizuku,
      boolean apks,
      boolean data,
      boolean obb)
      throws Exception {
    this(
        context,
        legacy(sources, useShizuku, apks, data, obb),
        new Operations.Cancellation(),
        text -> {});
  }

  private static BackupJob legacy(
      JSONArray sources, boolean shizuku, boolean apks, boolean data, boolean obb) {
    BackupJob job = new BackupJob();
    job.allApps = true; // The explicit legacy constructor requests all applications.
    job.sources = sources;
    job.shizuku = shizuku;
    job.apks = apks;
    job.data = data;
    job.obb = obb;
    return job;
  }

  AndroidDocuments(
      Context context,
      BackupJob job,
      Operations.Cancellation cancellation,
      ExportCache.Progress progress)
      throws Exception {
    this(context, job, cancellation, progress, PrivilegedFiles.class, null);
  }

  AndroidDocuments(
      Context context,
      BackupJob job,
      Operations.Cancellation cancellation,
      ExportCache.Progress progress,
      Class<?> service)
      throws Exception {
    this(context, job, cancellation, progress, service, null);
  }

  AndroidDocuments(
      Context context,
      BackupJob job,
      Operations.Cancellation cancellation,
      ExportCache.Progress progress,
      Class<?> service,
      JSONObject pending)
      throws Exception {
    output = null;
    job.validate(true);
    String[] kinds = {"apks", "data", "obb"},
        labels = {"Installed-APKs", "Android-data", "Android-obb"};
    for (int i = 0; i < kinds.length; i++) {
      if (job.appAll(kinds[i])) allApps.add(labels[i]);
      java.util.Set<String> apps = new java.util.HashSet<>();
      JSONArray packages = job.appPackages(kinds[i]);
      for (int j = 0; j < packages.length(); j++) apps.add(packages.getString(j));
      selected.put(labels[i], apps);
    }
    ShizukuBridge opened = null;
    ExportCache cache = null;
    try {
      for (int i = 0; i < job.sources.length(); i++) {
        JSONObject s = job.sources.getJSONObject(i);
        String label = s.getString("label");
        if (Paths.parts(label).length != 1 || trees.containsKey(label))
          throw new Exception("Invalid or duplicate source label");
        trees.put(
            label, new DocumentTree(context.getContentResolver(), Uri.parse(s.getString("uri"))));
      }
      if (job.shizuku
          && (job.apks
              || job.data
              || job.obb
              || job.exports.length() > 0
              || job.shizukuFolders.length() > 0)) {
        opened = new ShizukuBridge(context, service);
        for (int i = 0; i < job.shizukuFolders.length(); i++) {
          JSONObject folder = job.shizukuFolders.getJSONObject(i);
          String label = folder.getString("label");
          String path = "/@shared/" + folder.getString("path");
          if (!new JSONObject(opened.files().stat(path)).getBoolean("directory"))
            throw new Exception("Shizuku source is no longer a folder: " + label);
          privileged.put(label, path);
          sharedFolders.add(label);
        }
        if (job.apks) privileged.put("Installed-APKs", "/@apps");
        if (job.data) privileged.put("Android-data", "/@data");
        if (job.obb) privileged.put("Android-obb", "/@obb");
        if (job.exports.length() > 0) {
          cache =
              pending == null
                  ? new ExportCache(context)
                  : new ExportCache(
                      context,
                      pending.getString("history"),
                      new JSONObject(pending.getString("descriptor")).getLong("confirmed") > 0);
          cache.prepare(opened.files(), job.exports, cancellation, progress);
        }
      }
      for (String label : privileged.keySet())
        if (trees.containsKey(label))
          throw new Exception("Source label conflicts with Shizuku source");
      if (trees.isEmpty() && privileged.isEmpty() && cache == null)
        throw new Exception("Select at least one backup folder or Shizuku source");
    } catch (Exception e) {
      if (opened != null) opened.close();
      if (cache != null) cache.close();
      throw e;
    }
    bridge = opened;
    exports = cache;
  }

  AndroidDocuments(DocumentTree output) {
    this.output = output;
    bridge = null;
    exports = null;
  }

  private String label(String path) {
    String[] p = Paths.parts(path);
    if (p.length == 0) throw new IllegalArgumentException("Expected source path");
    return p[0];
  }

  private String child(String path) {
    int slash = path.indexOf('/');
    return slash < 0 ? "." : path.substring(slash + 1);
  }

  private String privilegedPath(String path) {
    String base = privileged.get(label(path));
    if (!allApps.contains(label(path))
        && !sharedFolders.contains(label(path))
        && !child(path).equals(".")) {
      String pkg = child(path).split("/", 2)[0];
      if (!selected.get(label(path)).contains(pkg))
        throw new SecurityException("App is not selected for this source");
    }
    return child(path).equals(".") ? base : base + "/" + child(path);
  }

  private boolean exportPath(String path) {
    return exports != null && label(path).equals("Device-data");
  }

  private File exportFile(String path) throws Exception {
    File file = exports.files.get(child(path));
    if (file == null) throw new Exception("Unknown device export file");
    return file;
  }

  private JSONObject exportStat(String name, boolean directory, long size) throws Exception {
    return new JSONObject()
        .put("name", name)
        .put("directory", directory)
        .put("size", size)
        .put("modified", 0);
  }

  private DocumentTree tree(String path) throws Exception {
    DocumentTree t = trees.get(label(path));
    if (t == null) throw new Exception("Unknown source folder");
    return t;
  }

  public String statJSON(String path) throws Exception {
    Paths.parts(path);
    if (output != null) return output.stat(path).toString();
    if (path.equals("."))
      return new JSONObject()
          .put("name", "root")
          .put("size", 0)
          .put("modified", 0)
          .put("directory", true)
          .toString();
    if (exportPath(path))
      return child(path).equals(".")
          ? exportStat("Device-data", true, 0).toString()
          : exportStat(child(path), false, exportFile(path).length()).toString();
    if (privileged.containsKey(label(path))) return bridge.files().stat(privilegedPath(path));
    return tree(path).stat(child(path)).toString();
  }

  public String listJSON(String path) throws Exception {
    Paths.parts(path);
    if (output != null) return output.list(path).toString();
    if (path.equals(".")) {
      JSONArray out = new JSONArray();
      for (String name : trees.keySet()) out.put(new JSONObject(statJSON(name)).put("name", name));
      for (String name : privileged.keySet())
        out.put(new JSONObject(statJSON(name)).put("name", name));
      if (exports != null) out.put(exportStat("Device-data", true, 0));
      return out.toString();
    }
    if (exportPath(path)) {
      if (!child(path).equals(".")) throw new Exception("Not a directory");
      JSONArray out = new JSONArray();
      for (Map.Entry<String, File> entry : exports.files.entrySet())
        out.put(exportStat(entry.getKey(), false, entry.getValue().length()));
      return out.toString();
    }
    if (privileged.containsKey(label(path))) {
      if (!allApps.contains(label(path))
          && child(path).equals(".")
          && label(path).equals("Installed-APKs")) {
        JSONArray out = new JSONArray();
        for (String pkg : new java.util.TreeSet<>(selected.get(label(path))))
          out.put(new JSONObject(bridge.files().stat("/@apps/" + pkg)).put("name", pkg));
        return out.toString();
      }
      JSONArray all = new JSONArray(bridge.files().list(privilegedPath(path)));
      if (allApps.contains(label(path))
          || sharedFolders.contains(label(path))
          || !child(path).equals(".")) return all.toString();
      JSONArray filtered = new JSONArray();
      for (int i = 0; i < all.length(); i++)
        if (selected.get(label(path)).contains(all.getJSONObject(i).getString("name")))
          filtered.put(all.get(i));
      return filtered.toString();
    }
    return tree(path).list(child(path)).toString();
  }

  public long openRead(String path) throws Exception {
    Paths.parts(path);
    if (output != null) return output.read(path);
    if (exportPath(path))
      return ParcelFileDescriptor.open(exportFile(path), ParcelFileDescriptor.MODE_READ_ONLY)
          .detachFd();
    if (privileged.containsKey(label(path))) {
      ParcelFileDescriptor fd = bridge.files().openRead(privilegedPath(path));
      if (fd == null) throw new Exception("Shizuku returned no descriptor");
      return fd.detachFd();
    }
    return tree(path).read(child(path));
  }

  public void makeDirectory(String path) throws Exception {
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    output.mkdir(path);
  }

  public long openWrite(String path) throws Exception {
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    return output.write(path);
  }

  public String inspectJSON(String path) throws Exception {
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    return output.inspect(path).toString();
  }

  public void commitFile(String temporary, String path, boolean replace) throws Exception {
    Paths.parts(temporary);
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    output.commitFile(temporary, path, replace);
  }

  public void deleteFile(String path) throws Exception {
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    output.deleteFile(path);
  }

  public void close() {
    if (bridge != null) bridge.close();
    if (exports != null) exports.close();
  }
}
