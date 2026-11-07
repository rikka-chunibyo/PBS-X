package com.pbsx.android;

import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONObject;

/** Local execution identity, display name and storage history are independent. */
final class BackupJob {
  String id = UUID.randomUUID().toString(), name = "Phone backup", store = "backup", namespace = "";
  String backupID = "backup-" + id.replace("-", "").substring(0, 12);
  JSONArray sources = new JSONArray();
  int hours = 0, schedulerId = 0;
  boolean wifi = true, charging = true, shizuku = false, apks = false, data = false, obb = false;

  JSONObject json() throws Exception {
    return new JSONObject()
        .put("id", id)
        .put("name", name)
        .put("store", store)
        .put("namespace", namespace)
        .put("backup_id", backupID)
        .put("sources", sources)
        .put("hours", hours)
        .put("scheduler_id", schedulerId)
        .put("wifi", wifi)
        .put("charging", charging)
        .put("shizuku", shizuku)
        .put("apks", apks)
        .put("data", data)
        .put("obb", obb);
  }

  static BackupJob from(JSONObject value) throws Exception {
    BackupJob j = new BackupJob();
    j.id = value.getString("id");
    j.name = value.getString("name");
    j.store = value.getString("store");
    j.namespace = value.optString("namespace", "");
    // Older jobs used the enrolled host ID. Keep that history until explicitly edited.
    j.backupID = value.optString("backup_id", "");
    j.sources =
        new JSONArray(
            value.optJSONArray("sources") == null
                ? "[]"
                : value.getJSONArray("sources").toString());
    j.hours = value.optInt("hours", 0);
    j.schedulerId = value.optInt("scheduler_id", 0);
    j.wifi = value.optBoolean("wifi", true);
    j.charging = value.optBoolean("charging", true);
    j.shizuku = value.optBoolean("shizuku", false);
    j.apks = value.optBoolean("apks", false);
    j.data = value.optBoolean("data", false);
    j.obb = value.optBoolean("obb", false);
    j.validate(false);
    return j;
  }

  void validate(boolean runnable) throws Exception {
    if (!id.matches("[A-Za-z0-9_-]{1,64}")) throw new Exception("Invalid job identity");
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
    java.util.HashSet<String> labels = new java.util.HashSet<>(), uris = new java.util.HashSet<>();
    for (int i = 0; i < sources.length(); i++) {
      JSONObject s = sources.getJSONObject(i);
      String label = s.getString("label");
      if (Paths.parts(label).length != 1
          || label.equals("Installed-APKs")
          || label.equals("Android-data")
          || label.equals("Android-obb")
          || !labels.add(label)) throw new Exception("Folder labels must be unique simple names");
      String uri = s.getString("uri");
      if (!uri.startsWith("content://") || !uris.add(uri))
        throw new Exception("Choose each folder only once in this job");
    }
    if (runnable && sources.length() == 0 && !(shizuku && (apks || data || obb)))
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
    return backupID.isEmpty() ? new JSONObject(Vault.load(c)).getString("host_id") : backupID;
  }

  static BackupJob create(android.content.Context c) throws Exception {
    BackupJob job = new BackupJob();
    job.store = Settings.store(c);
    String config = Vault.load(c);
    if (!config.isEmpty()) {
      String device = new JSONObject(config).getString("host_id");
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
    return sources.length() + (shizuku ? (apks ? 1 : 0) + (data ? 1 : 0) + (obb ? 1 : 0) : 0);
  }
}
