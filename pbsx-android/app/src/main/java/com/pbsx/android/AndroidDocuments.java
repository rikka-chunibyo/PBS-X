package com.pbsx.android;

import android.content.Context;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import com.pbsx.core.mobilecore.Documents;
import java.util.LinkedHashMap;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

final class AndroidDocuments implements Documents, AutoCloseable {
  private final Map<String, DocumentTree> trees = new LinkedHashMap<>();
  private final Map<String, String> privileged = new LinkedHashMap<>();
  private final ShizukuBridge bridge;
  private final DocumentTree output;

  AndroidDocuments(
      Context context,
      JSONArray sources,
      boolean useShizuku,
      boolean apks,
      boolean data,
      boolean obb)
      throws Exception {
    output = null;
    ShizukuBridge opened = null;
    try {
      for (int i = 0; i < sources.length(); i++) {
        JSONObject s = sources.getJSONObject(i);
        String label = s.getString("label");
        if (Paths.parts(label).length != 1 || trees.containsKey(label))
          throw new Exception("Invalid or duplicate source label");
        trees.put(
            label, new DocumentTree(context.getContentResolver(), Uri.parse(s.getString("uri"))));
      }
      if (useShizuku && (apks || data || obb)) {
        opened = new ShizukuBridge(context);
        if (apks) privileged.put("Installed-APKs", "/@apps");
        if (data) privileged.put("Android-data", "/storage/emulated/0/Android/data");
        if (obb) privileged.put("Android-obb", "/storage/emulated/0/Android/obb");
      }
      for (String label : privileged.keySet())
        if (trees.containsKey(label))
          throw new Exception("Source label conflicts with Shizuku source");
      if (trees.isEmpty() && privileged.isEmpty())
        throw new Exception("Select at least one backup folder or Shizuku source");
    } catch (Exception e) {
      if (opened != null) opened.close();
      throw e;
    }
    bridge = opened;
  }

  AndroidDocuments(DocumentTree output) {
    this.output = output;
    bridge = null;
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
    return child(path).equals(".") ? base : base + "/" + child(path);
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
      return out.toString();
    }
    if (privileged.containsKey(label(path))) return bridge.files().list(privilegedPath(path));
    return tree(path).list(child(path)).toString();
  }

  public long openRead(String path) throws Exception {
    Paths.parts(path);
    if (output != null) throw new Exception("Restore destination is write-only");
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

  public void close() {
    if (bridge != null) bridge.close();
  }
}
