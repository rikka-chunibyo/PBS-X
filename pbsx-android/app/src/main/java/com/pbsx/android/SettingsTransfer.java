package com.pbsx.android;

import android.content.Context;
import android.content.SharedPreferences;
import android.content.UriPermission;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import org.json.JSONArray;
import org.json.JSONObject;

/** Complete portable configuration. Device permission grants are descriptive, never replayed. */
final class SettingsTransfer {
  static final int LIMIT = 16 * 1024 * 1024;

  static JSONObject preferences(SharedPreferences prefs) throws Exception {
    JSONObject out = new JSONObject();
    for (Map.Entry<String, ?> entry : prefs.getAll().entrySet()) {
      Object value = entry.getValue();
      String type;
      if (value instanceof String) type = "string";
      else if (value instanceof Boolean) type = "boolean";
      else if (value instanceof Integer) type = "int";
      else if (value instanceof Long) type = "long";
      else if (value instanceof Float) type = "float";
      else if (value instanceof Set) {
        type = "strings";
        value = new JSONArray((Set<?>) value);
      } else throw new Exception("Unsupported saved preference type");
      out.put(entry.getKey(), new JSONObject().put("type", type).put("value", value));
    }
    return out;
  }

  static String exportJSON(Context c) throws Exception {
    synchronized (KeyProfiles.class) {
      synchronized (JobStore.class) {
        return exportLocked(c);
      }
    }
  }

  private static String exportLocked(Context c) throws Exception {
    if (Operations.running())
      throw new Exception("Wait for the active transfer before exporting settings");
    Vault.data(c); // Finish an interrupted import before capturing any settings section.
    JobStore.all(c);
    JSONObject preferences =
        new JSONObject()
            .put("settings", preferences(Settings.prefs(c)))
            .put("jobs-v1", preferences(c.getSharedPreferences("jobs-v1", 0)));
    JSONArray permissions = new JSONArray();
    for (UriPermission p : c.getContentResolver().getPersistedUriPermissions())
      permissions.put(
          new JSONObject()
              .put("uri", p.getUri().toString())
              .put("read", p.isReadPermission())
              .put("write", p.isWritePermission()));
    String out =
        new JSONObject()
            .put("format", "pbsx-android-settings")
            .put("version", 1)
            .put("app_version", BuildConfig.VERSION_NAME)
            .put("exported_at", System.currentTimeMillis())
            .put("vault", Vault.data(c))
            .put("pending_backups", PendingBackups.all(c))
            .put("preferences", preferences)
            .put("folder_permissions", permissions)
            .toString(2);
    if (out.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > LIMIT)
      throw new Exception("Settings export exceeds 16 MiB");
    validate(out);
    return out;
  }

  static JSONObject validate(String raw) throws Exception {
    if (raw.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > LIMIT)
      throw new Exception("Settings import exceeds 16 MiB");
    JSONObject root = new JSONObject(raw);
    if (!root.getString("format").equals("pbsx-android-settings") || root.getInt("version") != 1)
      throw new Exception("Unsupported settings export format");
    JSONObject vault = root.getJSONObject("vault");
    KeyProfiles.validate(vault);
    JSONObject preferences = root.getJSONObject("preferences");
    if (preferences.length() != 2) throw new Exception("Unexpected settings sections");
    for (String section : new String[] {"settings", "jobs-v1"})
      checkPreferences(preferences.getJSONObject(section));
    JSONObject jobs = preferences.getJSONObject("jobs-v1");
    JSONArray list = new JSONArray(stringPreference(jobs, "jobs", "[]"));
    if (list.length() > 100) throw new Exception("Too many jobs");
    Set<String> identities = new HashSet<>();
    Set<Integer> schedules = new HashSet<>();
    java.util.List<BackupJob> histories = new java.util.ArrayList<>();
    for (int i = 0; i < list.length(); i++) {
      BackupJob job = BackupJob.from(list.getJSONObject(i));
      KeyProfiles.validateJob(vault, job);
      if (!identities.add(job.id)
          || job.schedulerId < 1000
          || job.schedulerId == Integer.MAX_VALUE
          || !schedules.add(job.schedulerId))
        throw new Exception("Invalid or duplicate job/schedule identity");
      histories.add(job);
    }
    JSONArray pending =
        PendingBackups.validate(
            root.optJSONArray("pending_backups") == null
                ? new JSONArray()
                : root.getJSONArray("pending_backups"));
    for (int i = 0; i < pending.length(); i++) {
      BackupJob job = BackupJob.from(pending.getJSONObject(i).getJSONObject("job"));
      if (!identities.contains(job.id)) throw new Exception("Pending backup has no saved job");
      KeyProfiles.validateJob(vault, job);
      histories.add(job);
    }
    KeyProfiles.validateHistories(vault, histories);
    if (jobs.has("schedule_state"))
      ScheduleState.validate(stringPreference(jobs, "schedule_state", "{}"));
    if (jobs.has("next_scheduler_id")) {
      JSONObject next = jobs.getJSONObject("next_scheduler_id");
      if (!next.getString("type").equals("int"))
        throw new Exception("Invalid next schedule identity type");
      int n = next.getInt("value");
      if (n < 1001 || n == Integer.MAX_VALUE) throw new Exception("Invalid next schedule identity");
      for (int id : schedules)
        if (id >= n) throw new Exception("Next schedule identity overlaps saved jobs");
    } else if (list.length() > 0) throw new Exception("Missing next schedule identity");
    JSONArray history = new JSONArray(stringPreference(jobs, "history", "[]"));
    if (history.length() > 100) throw new Exception("Activity history exceeds 100 entries");
    for (int i = 0; i < history.length(); i++) {
      JSONObject run = history.getJSONObject(i);
      run.getString("id");
      run.getString("state");
      run.getString("job_id");
    }
    for (int i = 0; i < pending.length(); i++) {
      String expected = pending.getJSONObject(i).getString("history");
      boolean found = false;
      for (int j = 0; j < history.length(); j++)
        if (expected.equals(history.getJSONObject(j).getString("id"))) found = true;
      if (!found) throw new Exception("Pending backup activity entry is missing");
    }
    JSONObject settings = preferences.getJSONObject("settings");
    for (String flag :
        new String[] {
          "wifi", "imported_schedule_hold", "shizuku", "apks", "data", "obb", "charging"
        })
      if (settings.has(flag) && !settings.getJSONObject(flag).getString("type").equals("boolean"))
        throw new Exception("Invalid setting type: " + flag);
    for (String key : new String[] {"store", "namespace", "sources", "status"})
      requireType(settings, key, "string");
    requireType(settings, "hours", "int");
    requireType(settings, "status_time", "long");
    // These defaults are consumed if an imported jobs store needs legacy migration.
    // Validate their values before installation can trigger that migration.
    BackupJob legacy = new BackupJob();
    legacy.store = stringPreference(settings, "store", "backup");
    legacy.namespace = stringPreference(settings, "namespace", "");
    legacy.sources = new JSONArray(stringPreference(settings, "sources", "[]"));
    if (settings.has("hours")) legacy.hours = settings.getJSONObject("hours").getInt("value");
    legacy.validate(false);
    return root;
  }

  private static void requireType(JSONObject section, String key, String type) throws Exception {
    if (section.has(key) && !section.getJSONObject(key).getString("type").equals(type))
      throw new Exception("Invalid preference type: " + key);
  }

  private static String stringPreference(JSONObject section, String key, String fallback)
      throws Exception {
    if (!section.has(key)) return fallback;
    JSONObject value = section.getJSONObject(key);
    if (!value.getString("type").equals("string"))
      throw new Exception("Invalid preference type: " + key);
    return value.getString("value");
  }

  private static void checkPreferences(JSONObject values) throws Exception {
    if (values.length() > 1000) throw new Exception("Too many settings");
    java.util.Iterator<String> keys = values.keys();
    while (keys.hasNext()) {
      String key = keys.next();
      if (key.length() > 200) throw new Exception("Invalid setting name");
      JSONObject entry = values.getJSONObject(key);
      Object value = entry.get("value");
      switch (entry.getString("type")) {
        case "string":
          if (!(value instanceof String)) throw new Exception("Invalid string setting");
          break;
        case "boolean":
          if (!(value instanceof Boolean)) throw new Exception("Invalid boolean setting");
          break;
        case "int":
        case "long":
          if (!(value instanceof Integer) && !(value instanceof Long))
            throw new Exception("Invalid integer setting");
          if (entry.getString("type").equals("int")
              && (((Number) value).longValue() > Integer.MAX_VALUE
                  || ((Number) value).longValue() < Integer.MIN_VALUE))
            throw new Exception("Integer setting out of range");
          break;
        case "float":
          if (!(value instanceof Number) || !Float.isFinite(((Number) value).floatValue()))
            throw new Exception("Invalid float setting");
          break;
        case "strings":
          JSONArray array = entry.getJSONArray("value");
          if (array.length() > 1000) throw new Exception("String set too large");
          for (int i = 0; i < array.length(); i++)
            if (!(array.get(i) instanceof String)) throw new Exception("Invalid string set");
          break;
        default:
          throw new Exception("Unsupported preference type");
      }
    }
  }

  static void importJSON(Context c, String raw) throws Exception {
    JSONObject root = validate(raw), preferences = root.getJSONObject("preferences");
    preferences
        .getJSONObject("settings")
        .put("imported_schedule_hold", new JSONObject().put("type", "boolean").put("value", true));
    synchronized (KeyProfiles.class) {
      synchronized (JobStore.class) {
        Operations.changeSettings(
            () -> {
              Vault.data(c);
              if (PendingBackups.all(c).length() != 0)
                throw new Exception(
                    "Resume or cancel current pending backups before importing settings");
              JSONArray pending = root.optJSONArray("pending_backups");
              if (pending == null) pending = new JSONArray();
              for (int i = 0; i < pending.length(); i++) {
                JSONObject task = pending.getJSONObject(i);
                task.put("automatic", false);
                if (!PendingBackups.cleanupOnly(task)) task.put("state", "paused");
              }
              Vault.install(c, root.getJSONObject("vault"), preferences, pending);
              ScheduleService.configureAll(c);
            });
        JobStore.recover(c);
      }
    }
  }

  static void applyPreferences(Context c, JSONObject preferences) throws Exception {
    for (String section : new String[] {"settings", "jobs-v1"}) {
      JSONObject values = preferences.getJSONObject(section);
      checkPreferences(values);
      SharedPreferences.Editor editor = c.getSharedPreferences(section, 0).edit().clear();
      java.util.Iterator<String> keys = values.keys();
      while (keys.hasNext()) {
        String key = keys.next();
        JSONObject entry = values.getJSONObject(key);
        switch (entry.getString("type")) {
          case "string":
            editor.putString(key, entry.getString("value"));
            break;
          case "boolean":
            editor.putBoolean(key, entry.getBoolean("value"));
            break;
          case "int":
            editor.putInt(key, entry.getInt("value"));
            break;
          case "long":
            editor.putLong(key, entry.getLong("value"));
            break;
          case "float":
            editor.putFloat(key, (float) entry.getDouble("value"));
            break;
          case "strings":
            Set<String> set = new HashSet<>();
            JSONArray array = entry.getJSONArray("value");
            for (int i = 0; i < array.length(); i++) set.add(array.getString(i));
            editor.putStringSet(key, set);
            break;
          default:
            throw new Exception("Unsupported preference type");
        }
      }
      if (!editor.commit())
        throw new Exception(
            "Settings import was saved but could not finish; reopen the app to retry");
    }
  }
}
