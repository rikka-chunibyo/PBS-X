package com.pbsx.android;

import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONObject;

/** Local execution identity, display name and storage history are independent. */
final class BackupJob {
  String id = UUID.randomUUID().toString(), name = "Phone backup", store = "backup", namespace = "";
  String backupID = "backup-" + id.replace("-", "").substring(0, 12);
  String connectionID = "", encryptionKeyID = "";
  JSONArray sources = new JSONArray();
  JSONArray shizukuFolders = new JSONArray();
  int hours = 0, schedulerId = 0;
  boolean wifi = true, charging = true, shizuku = false, apks = false, data = false, obb = false;
  boolean allApps = false;
  JSONArray packages = new JSONArray(), exports = new JSONArray();

  JSONObject json() throws Exception {
    return new JSONObject()
        .put("id", id)
        .put("name", name)
        .put("store", store)
        .put("namespace", namespace)
        .put("backup_id", backupID)
        .put("connection_id", connectionID)
        .put("encryption_key_id", encryptionKeyID)
        .put("sources", sources)
        .put("shizuku_folders", shizukuFolders)
        .put("hours", hours)
        .put("scheduler_id", schedulerId)
        .put("wifi", wifi)
        .put("charging", charging)
        .put("shizuku", shizuku)
        .put("apks", apks)
        .put("data", data)
        .put("obb", obb)
        .put("all_apps", allApps)
        .put("packages", packages)
        .put("exports", exports);
  }

  static BackupJob from(JSONObject value) throws Exception {
    if (value.has("all_apps") && !(value.opt("all_apps") instanceof Boolean))
      throw new Exception("Invalid saved app selection mode");
    for (String key : new String[] {"packages", "exports", "shizuku_folders"})
      if (value.has(key) && !(value.opt(key) instanceof JSONArray))
        throw new Exception("Invalid saved " + key + " selection");
    BackupJob j = new BackupJob();
    j.id = value.getString("id");
    j.name = value.getString("name");
    j.store = value.getString("store");
    j.namespace = value.optString("namespace", "");
    // Older jobs used the enrolled host ID. Keep that history until explicitly edited.
    j.backupID = value.optString("backup_id", "");
    j.connectionID = value.optString("connection_id", "");
    j.encryptionKeyID = value.optString("encryption_key_id", "");
    j.sources =
        new JSONArray(
            value.optJSONArray("sources") == null
                ? "[]"
                : value.getJSONArray("sources").toString());
    j.shizukuFolders =
        new JSONArray(
            value.optJSONArray("shizuku_folders") == null
                ? "[]"
                : value.getJSONArray("shizuku_folders").toString());
    j.hours = value.optInt("hours", 0);
    j.schedulerId = value.optInt("scheduler_id", 0);
    j.wifi = value.optBoolean("wifi", true);
    j.charging = value.optBoolean("charging", true);
    j.shizuku = value.optBoolean("shizuku", false);
    j.apks = value.optBoolean("apks", false);
    j.data = value.optBoolean("data", false);
    j.obb = value.optBoolean("obb", false);
    j.allApps = value.optBoolean("all_apps", true);
    j.packages =
        new JSONArray(
            value.optJSONArray("packages") == null
                ? "[]"
                : value.getJSONArray("packages").toString());
    j.exports =
        new JSONArray(
            value.optJSONArray("exports") == null
                ? "[]"
                : value.getJSONArray("exports").toString());
    j.validate(false);
    return j;
  }

  void validate(boolean runnable) throws Exception {
    if (!id.matches("[A-Za-z0-9_-]{1,64}")) throw new Exception("Invalid job identity");
    for (String identity : new String[] {connectionID, encryptionKeyID})
      if (!identity.isEmpty() && !identity.matches("[A-Za-z0-9_-]{1,64}"))
        throw new Exception("Invalid saved credential reference");
    name = name.trim();
    store = store.trim();
    namespace = namespace.trim();
    backupID = backupID.trim();
    if (!backupID.isEmpty() && !backupID.matches("[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}"))
      throw new Exception("Backup ID must be 1–64 letters, digits, underscores, dots or hyphens");
    if (name.isEmpty() || name.length() > 100 || name.indexOf('\n') >= 0)
      throw new Exception("Enter a job name of 1–100 characters");
    if (!store.matches("[A-Za-z0-9_][A-Za-z0-9_.-]*"))
      throw new Exception("Enter a valid datastore name");
    if (!namespace.isEmpty()) Paths.parts(namespace);
    if (hours < 0 || hours > 720) throw new Exception("Schedule interval must be 0–720 hours");
    java.util.HashSet<String> selectedApps = new java.util.HashSet<>(),
        selectedExports = new java.util.HashSet<>();
    if (packages.length() > 500) throw new Exception("Select at most 500 apps");
    for (int i = 0; i < packages.length(); i++) {
      String pkg = packages.getString(i);
      if (pkg.length() > 255
          || !pkg.matches("[A-Za-z0-9_]+(\\.[A-Za-z0-9_]+)+")
          || !selectedApps.add(pkg)) throw new Exception("Invalid or duplicate selected app");
    }
    for (int i = 0; i < exports.length(); i++) {
      String category = exports.getString(i);
      if (!DeviceExport.CATEGORIES.contains(category) || !selectedExports.add(category))
        throw new Exception("Invalid or duplicate device export");
    }
    if (runnable && shizuku && (apks || data || obb) && !allApps && packages.length() == 0)
      throw new Exception("Choose apps or enable All apps");
    java.util.HashSet<String> labels = new java.util.HashSet<>(), uris = new java.util.HashSet<>();
    if (shizukuFolders.length() > 100) throw new Exception("Select at most 100 Shizuku folders");
    java.util.HashSet<String> sharedPaths = new java.util.HashSet<>();
    for (int i = 0; i < sources.length() + shizukuFolders.length(); i++) {
      boolean shared = i >= sources.length();
      JSONObject s =
          shared ? shizukuFolders.getJSONObject(i - sources.length()) : sources.getJSONObject(i);
      if (shared && (!(s.opt("label") instanceof String) || !(s.opt("path") instanceof String)))
        throw new Exception("Invalid saved Shizuku folder");
      String label = s.getString("label");
      if (Paths.parts(label).length != 1
          || label.equals("Installed-APKs")
          || label.equals("Android-data")
          || label.equals("Android-obb")
          || (label.equals("Device-data") && shizuku && exports.length() > 0)
          || !labels.add(label)) throw new Exception("Folder labels must be unique simple names");
      if (shared) {
        String path = s.getString("path");
        SharedStorage.validate(path, false);
        if (!sharedPaths.add(path)) throw new Exception("Choose each Shizuku folder only once");
        continue;
      }
      String uri = s.getString("uri");
      if (!uri.startsWith("content://") || !uris.add(uri))
        throw new Exception("Choose each folder only once in this job");
    }
    if (runnable
        && sources.length() == 0
        && !(shizuku
            && (apks || data || obb || exports.length() > 0 || shizukuFolders.length() > 0)))
      throw new Exception("Add a folder or enable a Shizuku source for this job");
  }

  String target() {
    return store
        + " / "
        + (namespace.isEmpty() ? "root" : namespace)
        + " / "
        + (backupID.isEmpty() ? "device ID" : backupID);
  }

  String backupID(android.content.Context c) throws Exception {
    return backupID.isEmpty()
        ? new JSONObject(KeyProfiles.config(c, connectionID, "", false)).getString("host_id")
        : backupID;
  }

  static BackupJob create(android.content.Context c) throws Exception {
    BackupJob job = new BackupJob();
    job.connectionID = KeyProfiles.selected(c);
    job.store = Settings.store(c);
    String config = KeyProfiles.config(c, job.connectionID, "", false);
    if (!config.isEmpty()) {
      JSONObject values = new JSONObject(config);
      job.store = values.optString("default_store", job.store);
      if (values.has("default_namespace")) {
        job.namespace = values.getString("default_namespace");
        return job;
      }
      String device = values.getString("host_id");
      // PBS namespace components are limited to 32 characters; preserve the
      // full device ID without a collision-prone truncated name.
      job.namespace =
          "devices/"
              + (device.length() > 32
                  ? device.substring(0, 32) + "/" + device.substring(32)
                  : device);
    }
    return job;
  }

  String schedule() {
    return hours == 0 ? "Manual" : "Every " + hours + "h" + (charging ? " · charging" : "");
  }

  int sourceCount() {
    return sources.length()
        + (shizuku
            ? (apks ? 1 : 0)
                + (data ? 1 : 0)
                + (obb ? 1 : 0)
                + exports.length()
                + shizukuFolders.length()
            : 0);
  }
}
