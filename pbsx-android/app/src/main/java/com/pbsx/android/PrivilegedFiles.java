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
public class PrivilegedFiles extends IPrivilegedFiles.Stub {
  private final int owner;
  private boolean exporting;

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
    boolean trusted = false;
    for (String root :
        new String[] {
          "/data/app/", "/system/", "/product/", "/system_ext/", "/vendor/", "/odm/", "/apex/"
        }) if (f.getPath().startsWith(root)) trusted = true;
    if (!trusted || !f.getName().endsWith(".apk")) throw new Exception("Unexpected APK location");
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
          if (paths.isEmpty()) throw new Exception("Selected app is no longer installed");
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
  public String apps() {
    authorize();
    try {
      java.util.TreeSet<String> apps = new java.util.TreeSet<>();
      for (String line :
          command(
              "/system/bin/pm", "list", "packages", "-3", "--user", String.valueOf(owner / 100000)))
        if (line.startsWith("package:")) apps.add(line.substring(8));
      for (String root :
          new String[] {"/storage/emulated/0/Android/data", "/storage/emulated/0/Android/obb"}) {
        if (!new File(root).exists() || !new File(root).canRead()) continue;
        File base = allowed(root);
        File[] children = base.listFiles();
        if (children == null) continue;
        for (File child : children)
          if (child.isDirectory() && child.getName().matches("[A-Za-z0-9_]+(\\.[A-Za-z0-9_]+)+"))
            apps.add(child.getName());
      }
      JSONArray out = new JSONArray();
      for (String pkg : apps) {
        packageName(pkg);
        out.put(pkg);
      }
      if (out.toString().length() > 300000) throw new Exception("App selection list is too large");
      return out.toString();
    } catch (Exception e) {
      throw error(e);
    }
  }

  protected DeviceExport.Source exportSource() throws Exception {
    return new ShellProviderSource(owner / 100000);
  }

  @Override
  public synchronized ParcelFileDescriptor openExport(String category) {
    authorize();
    if (!DeviceExport.CATEGORIES.contains(category))
      throw new IllegalArgumentException("Unknown device export");
    if (android.os.Build.VERSION.SDK_INT < 31)
      throw new IllegalArgumentException("Device exports require Android 12 or later");
    if (exporting) throw new IllegalStateException("Another export is running");
    try {
      ParcelFileDescriptor[] pipe = ParcelFileDescriptor.createReliablePipe();
      exporting = true;
      Thread producer =
          new Thread(
              () -> {
                String failure = null;
                ParcelFileDescriptor.AutoCloseOutputStream output =
                    new ParcelFileDescriptor.AutoCloseOutputStream(pipe[1]);
                try (DeviceExport.Source source = exportSource()) {
                  java.io.OutputStream bounded =
                      new java.io.FilterOutputStream(output) {
                        long total;

                        public void write(int value) throws java.io.IOException {
                          byte[] one = {(byte) value};
                          write(one, 0, 1);
                        }

                        public void write(byte[] bytes, int start, int count)
                            throws java.io.IOException {
                          total += count;
                          if (total > DeviceExport.MAX_BYTES)
                            throw new java.io.IOException("Device export exceeds 512 MiB");
                          out.write(bytes, start, count);
                        }
                      };
                  DeviceExport.write(category, source, bounded);
                } catch (Throwable e) {
                  String message = e.getMessage();
                  failure =
                      "Could not export "
                          + category
                          + ": "
                          + (message == null
                              ? e.getClass().getSimpleName()
                              : message.substring(0, Math.min(256, message.length())));
                } finally {
                  synchronized (PrivilegedFiles.this) {
                    exporting = false;
                  }
                  try {
                    if (failure != null) pipe[1].closeWithError(failure);
                  } catch (Exception ignored) {
                  }
                  try {
                    output.close();
                  } catch (Exception ignored) {
                  }
                }
              },
              "pbsx-device-export");
      producer.setDaemon(true);
      producer.start();
      return pipe[0];
    } catch (Exception e) {
      exporting = false;
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
