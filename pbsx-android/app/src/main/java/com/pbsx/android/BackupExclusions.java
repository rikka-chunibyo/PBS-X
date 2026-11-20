package com.pbsx.android;

import java.util.ArrayList;
import java.util.List;
import org.json.JSONArray;

/** Case-sensitive backup-relative paths; a matched directory excludes its subtree. */
final class BackupExclusions {
  private final List<String[]> rules = new ArrayList<>();

  BackupExclusions(JSONArray values) throws Exception {
    this(strings(values));
  }

  private static List<String> strings(JSONArray values) throws Exception {
    if (values.length() > 200) throw new Exception("Use at most 200 exclusion rules");
    List<String> out = new ArrayList<>();
    for (int i = 0; i < values.length(); i++) {
      if (!(values.opt(i) instanceof String)) throw new Exception("Invalid exclusion rule");
      out.add(values.getString(i));
    }
    return out;
  }

  BackupExclusions(List<String> values) throws Exception {
    if (values.size() > 200) throw new Exception("Use at most 200 exclusion rules");
    int length = 0;
    for (String value : values) {
      if (value == null) throw new Exception("Invalid exclusion rule");
      length += value.length();
      if (value.isEmpty() || value.length() > 512 || length > 16384 || !value.equals(value.trim()))
        throw new Exception("Exclusion rules must be nonempty paths of at most 512 characters");
      if (value.equals(".") || value.indexOf('\n') >= 0 || value.indexOf('\r') >= 0)
        throw new Exception("Enter a backup-relative exclusion, for example DCIM/.thumbnails");
      String[] parts = Paths.parts(value);
      for (String part : parts)
        if (part.contains("**") && !part.equals("**"))
          throw new Exception("Use ** as a complete path component");
      rules.add(parts);
    }
  }

  boolean excludes(String path) {
    if (path.equals(".") || rules.isEmpty()) return false;
    String[] parts = Paths.parts(path);
    for (String[] rule : rules) {
      // Dynamic programming avoids regex backtracking for imported wildcard rules.
      boolean[] previous = new boolean[parts.length + 1];
      previous[0] = true;
      for (String component : rule) {
        boolean[] next = new boolean[parts.length + 1];
        if (component.equals("**")) {
          next[0] = previous[0];
          for (int i = 1; i <= parts.length; i++) next[i] = previous[i] || next[i - 1];
        } else {
          for (int i = 1; i <= parts.length; i++)
            next[i] = previous[i - 1] && segment(component, parts[i - 1]);
        }
        previous = next;
      }
      for (boolean matched : previous) if (matched) return true;
    }
    return false;
  }

  private static boolean segment(String pattern, String name) {
    if (pattern.indexOf('*') < 0 && pattern.indexOf('?') < 0) return pattern.equals(name);
    int p = 0, n = 0, star = -1, retry = 0;
    while (n < name.length()) {
      if (p < pattern.length()
          && pattern.charAt(p) != '*'
          && (pattern.charAt(p) == '?' || pattern.codePointAt(p) == name.codePointAt(n))) {
        p += Character.charCount(pattern.codePointAt(p));
        n += Character.charCount(name.codePointAt(n));
      } else if (p < pattern.length() && pattern.charAt(p) == '*') {
        star = p++;
        retry = n;
      } else if (star >= 0) {
        p = star + 1;
        retry += Character.charCount(name.codePointAt(retry));
        n = retry;
      } else return false;
    }
    while (p < pattern.length() && pattern.charAt(p) == '*') p++;
    return p == pattern.length();
  }

  static JSONArray parse(String text) throws Exception {
    JSONArray out = new JSONArray();
    for (String line : text.split("\\R")) if (!line.trim().isEmpty()) out.put(line.trim());
    new BackupExclusions(out);
    return out;
  }

  static String text(JSONArray values) throws Exception {
    StringBuilder out = new StringBuilder();
    for (int i = 0; i < values.length(); i++) {
      if (i > 0) out.append('\n');
      out.append(values.getString(i));
    }
    return out.toString();
  }
}
