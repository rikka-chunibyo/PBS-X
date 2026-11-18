package com.pbsx.android;

import java.io.File;
import java.nio.file.Files;
import java.util.ArrayList;
import java.util.Comparator;

/** Profile-relative shared folders, never app-private storage or arbitrary filesystem paths. */
final class SharedStorage {
  static void validate(String path, boolean root) {
    if (root && path.equals(".")) return;
    if (path.isEmpty() || path.length() > 4096 || path.indexOf('\\') >= 0)
      throw new IllegalArgumentException("Choose a shared-storage folder");
    for (String part : path.split("/", -1)) {
      if (part.isEmpty() || part.equals(".") || part.equals(".."))
        throw new IllegalArgumentException("Invalid shared-storage folder path");
      for (int i = 0; i < part.length(); i++)
        if (Character.isISOControl(part.charAt(i)))
          throw new IllegalArgumentException("Invalid shared-storage folder name");
    }
    if (path.equals("Android") || path.startsWith("Android/"))
      throw new IllegalArgumentException(
          "Use the Android/data and Android/obb options for app folders");
  }

  static File resolve(File root, String path, boolean browseRoot) throws Exception {
    validate(path, browseRoot);
    File base = root.getCanonicalFile();
    File file = base;
    if (!path.equals("."))
      for (String part : path.split("/")) {
        file = new File(file, part);
        if (Files.isSymbolicLink(file.toPath()))
          throw new SecurityException("Symlinks are not supported in Shizuku folders");
      }
    File canonical = file.getCanonicalFile();
    if (!canonical.equals(base) && !canonical.getPath().startsWith(base.getPath() + "/"))
      throw new SecurityException("Source escaped shared storage");
    if (!canonical.exists() || !canonical.canRead())
      throw new IllegalArgumentException("Shizuku cannot read this shared-storage folder or file");
    return canonical;
  }

  static File[] directories(File root, String path) throws Exception {
    File[] children = resolve(root, path, true).listFiles();
    if (children == null) throw new IllegalArgumentException("Shizuku cannot list this folder");
    ArrayList<File> result = new ArrayList<>();
    for (File child : children) {
      if (path.equals(".") && child.getName().equals("Android")) continue;
      if (child.isDirectory() && !Files.isSymbolicLink(child.toPath())) result.add(child);
    }
    result.sort(Comparator.comparing(File::getName, String.CASE_INSENSITIVE_ORDER));
    return result.toArray(new File[0]);
  }
}
