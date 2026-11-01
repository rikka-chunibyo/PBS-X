package com.pbsx.android;

import android.content.Context;
import android.os.Binder;
import android.os.ParcelFileDescriptor;
import android.os.RemoteException;
import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import org.json.JSONArray;
import org.json.JSONObject;

/** Read-only Shizuku UserService. No arbitrary command or filesystem API. */
public final class PrivilegedFiles extends IPrivilegedFiles.Stub {
  private final int owner;

  public PrivilegedFiles(Context context) {
    owner = context.getApplicationInfo().uid;
  }

  private void authorize() {
    if (Binder.getCallingUid() != owner) throw new SecurityException("Wrong caller");
  }

  private static ArrayList<String> command(String... args) throws Exception {
    Process p = new ProcessBuilder(args).redirectErrorStream(true).start();
    ArrayList<String> lines = new ArrayList<>();
    try (BufferedReader r =
        new BufferedReader(new InputStreamReader(p.getInputStream(), StandardCharsets.UTF_8))) {
      String line;
      int size = 0;
      while ((line = r.readLine()) != null) {
        size += line.length();
        if (size > 400000) {
          p.destroy();
          throw new Exception("Package list too large");
        }
        lines.add(line);
      }
    }
    if (p.waitFor() != 0) throw new Exception("Android package service refused access");
    return lines;
  }

  private static void packageName(String name) {
    if (!name.matches("[A-Za-z0-9_]+(\\.[A-Za-z0-9_]+)+"))
      throw new IllegalArgumentException("Invalid package name");
  }

  private static File apk(String pkg, String name) throws Exception {
    packageName(pkg);
    if (!name.matches("[0-9]+\\.apk")) throw new Exception("Invalid APK name");
    int index = Integer.parseInt(name.substring(0, name.length() - 4));
    ArrayList<String> paths = command("/system/bin/pm", "path", pkg);
    if (index < 0 || index >= paths.size() || !paths.get(index).startsWith("package:"))
      throw new Exception("APK no longer exists");
    File f = new File(paths.get(index).substring(8)).getCanonicalFile();
    if (!f.getPath().startsWith("/data/app/")
        && !f.getPath().startsWith("/system/")
        && !f.getPath().startsWith("/product/")) throw new Exception("Unexpected APK location");
    return f;
  }

  private static File allowed(String path) throws Exception {
    if (!path.startsWith("/storage/emulated/0/Android/data")
        && !path.startsWith("/storage/emulated/0/Android/obb"))
      throw new SecurityException("Source outside selected Shizuku roots");
    File f = new File(path).getCanonicalFile();
    String canonical = f.getPath();
    boolean allowed = false;
    for (String base :
        new String[] {"/storage/emulated/0/Android/data", "/storage/emulated/0/Android/obb"}) {
      String root = new File(base).getCanonicalPath();
      if (canonical.equals(root) || canonical.startsWith(root + "/")) allowed = true;
    }
    if (!allowed) throw new SecurityException("Source escaped selected Shizuku root");
    if (java.nio.file.Files.isSymbolicLink(new File(path).toPath()))
      throw new Exception("Symlinks are not supported");
    if (!f.exists() || !f.canRead())
      throw new Exception("Shizuku cannot read this source on this Android version");
    return f;
  }

  private static JSONObject entry(File f, String name) throws Exception {
    if (!f.isFile() && !f.isDirectory()) throw new Exception("Unsupported special file");
    return new JSONObject()
        .put("name", name)
        .put("directory", f.isDirectory())
        .put("size", f.isDirectory() ? 0 : f.length())
        .put("modified", f.lastModified());
  }

  private static JSONObject directory(String name) throws Exception {
    return new JSONObject()
        .put("name", name)
        .put("directory", true)
        .put("size", 0)
        .put("modified", 0);
  }

  private static String[] virtual(String path) {
    if (!path.equals("/@apps") && !path.startsWith("/@apps/")) return null;
    String[] parts = path.substring(1).split("/", -1);
    if (parts.length > 3) throw new IllegalArgumentException("Invalid APK path");
    return parts;
  }

  private static IllegalArgumentException error(Exception e) {
    // Binder cannot marshal a server-thrown RemoteException. Use its supported
    // exception protocol so a denied read reaches the client as an error.
    return new IllegalArgumentException(
        e.getMessage() == null ? "Shizuku read failed" : e.getMessage());
  }

  @Override
  public String stat(String path) throws RemoteException {
    authorize();
    try {
      String[] v = virtual(path);
      if (v != null) {
        if (v.length < 3) return directory(v[v.length - 1]).toString();
        return entry(apk(v[1], v[2]), v[2]).toString();
      }
      File f = allowed(path);
      return entry(f, f.getName()).toString();
    } catch (Exception e) {
      throw error(e);
    }
  }

  @Override
  public String list(String path) throws RemoteException {
    authorize();
    try {
      JSONArray out = new JSONArray();
      String[] v = virtual(path);
      if (v != null) {
        if (v.length == 1) {
          ArrayList<String> packages = command("/system/bin/pm", "list", "packages", "-3");
          Collections.sort(packages);
          for (String p : packages)
            if (p.startsWith("package:")) {
              String name = p.substring(8);
              packageName(name);
              out.put(directory(name));
            }
        } else if (v.length == 2) {
          packageName(v[1]);
          ArrayList<String> paths = command("/system/bin/pm", "path", v[1]);
          for (int i = 0; i < paths.size(); i++) out.put(entry(apk(v[1], i + ".apk"), i + ".apk"));
        } else throw new Exception("Not a directory");
      } else {
        File dir = allowed(path);
        File[] files = dir.listFiles();
        if (files == null) throw new Exception("Shizuku cannot list this directory");
        for (File f : files) out.put(entry(allowed(f.getPath()), f.getName()));
      }
      String json = out.toString();
      if (json.length() > 350000) throw new Exception("Directory exceeds safe Binder message size");
      return json;
    } catch (Exception e) {
      throw error(e);
    }
  }

  @Override
  public ParcelFileDescriptor openRead(String path) throws RemoteException {
    authorize();
    try {
      String[] v = virtual(path);
      File file = v != null && v.length == 3 ? apk(v[1], v[2]) : allowed(path);
      if (!file.isFile()) throw new Exception("Not a regular file");
      return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY);
    } catch (Exception e) {
      throw error(e);
    }
  }

  @Override
  public void destroy() {
    int caller = Binder.getCallingUid();
    if (caller != owner && caller != android.os.Process.myUid())
      throw new SecurityException("Wrong caller");
    System.exit(0);
  }
}
