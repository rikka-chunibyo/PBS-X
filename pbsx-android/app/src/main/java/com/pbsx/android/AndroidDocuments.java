package com.pbsx.android;

import android.content.Context;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import com.pbsx.core.mobilecore.Documents;
import java.io.File;
import java.util.LinkedHashMap;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

final class AndroidDocuments implements Documents, AutoCloseable {
  private final Map<String, DocumentTree> trees = new LinkedHashMap<>();
  private final Map<String, String> privileged = new LinkedHashMap<>();
  private final ShizukuBridge bridge;
  private final DocumentTree output;
  private final ExportCache exports;
  private final java.util.Set<String> selected = new java.util.HashSet<>();
  private boolean allApps = true;

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
    this(context, job, cancellation, progress, PrivilegedFiles.class);
  }

  AndroidDocuments(
      Context context,
      BackupJob job,
      Operations.Cancellation cancellation,
      ExportCache.Progress progress,
      Class<?> service)
      throws Exception {
    output = null;
    job.validate(true);
    allApps = job.allApps;
    for (int i = 0; i < job.packages.length(); i++) selected.add(job.packages.getString(i));
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
      if (job.shizuku && (job.apks || job.data || job.obb || job.exports.length() > 0)) {
        opened = new ShizukuBridge(context, service);
        if (job.apks) privileged.put("Installed-APKs", "/@apps");
        if (job.data) privileged.put("Android-data", "/storage/emulated/0/Android/data");
        if (job.obb) privileged.put("Android-obb", "/storage/emulated/0/Android/obb");
        if (job.exports.length() > 0) {
          cache = new ExportCache(context);
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
    if (!allApps && !child(path).equals(".")) {
      String pkg = child(path).split("/", 2)[0];
      if (!selected.contains(pkg)) throw new SecurityException("App is not selected for this job");
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
      if (!allApps && child(path).equals(".") && label(path).equals("Installed-APKs")) {
        JSONArray out = new JSONArray();
        for (String pkg : new java.util.TreeSet<>(selected))
          out.put(new JSONObject(bridge.files().stat("/@apps/" + pkg)).put("name", pkg));
        return out.toString();
      }
      JSONArray all = new JSONArray(bridge.files().list(privilegedPath(path)));
      if (allApps || !child(path).equals(".")) return all.toString();
      JSONArray filtered = new JSONArray();
      for (int i = 0; i < all.length(); i++)
        if (selected.contains(all.getJSONObject(i).getString("name"))) filtered.put(all.get(i));
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

  public long openReplace(String path) throws Exception {
    Paths.parts(path);
    if (output == null) throw new Exception("Backup sources are read-only");
    return output.replace(path);
  }

  public void close() {
    if (bridge != null) bridge.close();
    if (exports != null) exports.close();
  }
}
