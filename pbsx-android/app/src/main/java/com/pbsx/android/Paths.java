package com.pbsx.android;

final class Paths {
  static String[] parts(String path) {
    if (".".equals(path)) return new String[0];
    if (path.isEmpty()
        || path.length() > 4096
        || path.startsWith("/")
        || path.indexOf('\\') >= 0
        || path.indexOf('\0') >= 0) throw new IllegalArgumentException("Unsafe document path");
    String[] parts = path.split("/", -1);
    if (parts.length > 129) throw new IllegalArgumentException("Directory tree too deep");
    for (String part : parts)
      if (part.isEmpty() || part.equals(".") || part.equals(".."))
        throw new IllegalArgumentException("Unsafe document name");
    return parts;
  }

  private Paths() {}
}
