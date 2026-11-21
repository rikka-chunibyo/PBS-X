package com.pbsx.android;

import android.content.Context;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.HashSet;
import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONObject;

/** Portable secrets are wrapped by the device's Keystore-backed vault. Jobs store IDs only. */
final class KeyProfiles {
  static String selected(Context c) throws Exception {
    return Vault.data(c).optString("default_connection");
  }

  static JSONObject find(JSONObject vault, String kind, String id) throws Exception {
    JSONArray values = vault.getJSONArray(kind);
    for (int i = 0; i < values.length(); i++)
      if (id.equals(values.getJSONObject(i).getString("id"))) return values.getJSONObject(i);
    throw new Exception(
        kind.equals("keys")
            ? "The selected encryption key is unavailable"
            : "The selected connection is unavailable");
  }

  static JSONObject connection(Context c, String id) throws Exception {
    JSONObject vault = Vault.data(c);
    return find(vault, "connections", id.isEmpty() ? vault.optString("default_connection") : id);
  }

  static String config(Context c, String connection, String encryption, boolean backup)
      throws Exception {
    JSONObject vault = Vault.data(c);
    String id = connection.isEmpty() ? vault.optString("default_connection") : connection;
    if (id.isEmpty() && connection.isEmpty()) return "";
    JSONObject cfg =
        new JSONObject(find(vault, "connections", id).getJSONObject("config").toString());
    JSONArray keys = vault.getJSONArray("keys"), raw = new JSONArray();
    for (int i = 0; i < keys.length(); i++) raw.put(keys.getJSONObject(i).getString("key"));
    cfg.put("decryption_keys", raw).put("encryption_enabled", backup && !encryption.isEmpty());
    if (backup && !encryption.isEmpty())
      cfg.put("encryption_key", find(vault, "keys", encryption).getString("key"));
    return cfg.toString();
  }

  static void label(String name) throws Exception {
    if (name.trim().isEmpty() || name.length() > 100 || name.indexOf('\n') >= 0)
      throw new Exception("Enter a name of 1–100 characters");
  }

  static synchronized String addConnection(Context c, String name, String config) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      return storeConnection(c, name, config);
    }
  }

  static String enrollConnection(Context c, String name, String code, String hostname)
      throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      label(name);
      synchronized (KeyProfiles.class) {
        if (Vault.data(c).getJSONArray("connections").length() >= 100)
          throw new Exception("At most 100 connections are supported");
      }
      // Reserve before consuming a one-time code, but never hold the profile
      // monitor across the network call (other UI actions must fail promptly).
      String config = Mobilecore.enroll(code, hostname);
      synchronized (KeyProfiles.class) {
        return storeConnection(c, name, config);
      }
    }
  }

  private static String storeConnection(Context c, String name, String config) throws Exception {
    label(name);
    Mobilecore.newClient(config);
    JSONObject data = Vault.data(c);
    JSONArray values = data.getJSONArray("connections");
    if (values.length() >= 100) throw new Exception("At most 100 connections are supported");
    String id = UUID.randomUUID().toString();
    values.put(
        new JSONObject()
            .put("id", id)
            .put("name", name.trim())
            .put("config", new JSONObject(config)));
    if (data.optString("default_connection").isEmpty()) data.put("default_connection", id);
    Vault.store(c, data);
    return id;
  }

  static synchronized String addKey(Context c, String name, String raw) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      label(name);
      raw = raw.trim().toLowerCase(java.util.Locale.ROOT);
      String fp = Mobilecore.encryptionFingerprint(raw);
      JSONObject data = Vault.data(c);
      JSONArray values = data.getJSONArray("keys");
      if (values.length() >= 100) throw new Exception("At most 100 encryption keys are supported");
      for (int i = 0; i < values.length(); i++)
        if (fp.equals(values.getJSONObject(i).getString("fingerprint")))
          throw new Exception("This key is already saved");
      String id = UUID.randomUUID().toString();
      values.put(
          new JSONObject()
              .put("id", id)
              .put("name", name.trim())
              .put("key", raw)
              .put("fingerprint", fp));
      Vault.store(c, data);
      return id;
    }
  }

  static synchronized void remove(Context c, String kind, String id) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      for (BackupJob job : JobStore.all(c))
        if (id.equals(
            kind.equals("keys")
                ? job.encryptionKeyID
                : job.connectionID.isEmpty() ? selected(c) : job.connectionID))
          throw new Exception("Reassign jobs using this entry first");
      for (BackupJob job : pendingJobs(c))
        if (id.equals(kind.equals("keys") ? job.encryptionKeyID : job.connectionID))
          throw new Exception("Resume or cancel pending backups using this entry first");
      JSONObject data = Vault.data(c);
      JSONArray values = data.getJSONArray(kind), out = new JSONArray();
      for (int i = 0; i < values.length(); i++)
        if (!id.equals(values.getJSONObject(i).getString("id"))) out.put(values.get(i));
      data.put(kind, out);
      if (kind.equals("connections") && id.equals(data.optString("default_connection")))
        data.put(
            "default_connection", out.length() == 0 ? "" : out.getJSONObject(0).getString("id"));
      Vault.store(c, data);
    }
  }

  static synchronized void setDefault(Context c, String id) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      JSONObject data = Vault.data(c);
      find(data, "connections", id);
      data.put("default_connection", id);
      java.util.List<BackupJob> histories = JobStore.all(c);
      histories.addAll(pendingJobs(c));
      validateHistories(data, histories);
      Vault.store(c, data);
    }
  }

  static synchronized void rename(Context c, String kind, String id, String name) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      label(name);
      JSONObject data = Vault.data(c);
      find(data, kind, id).put("name", name.trim());
      Vault.store(c, data);
    }
  }

  static synchronized void updateConnection(Context c, String id, JSONObject config)
      throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      Mobilecore.newClient(config.toString());
      for (BackupJob job : pendingJobs(c))
        if (id.equals(job.connectionID))
          throw new Exception("Resume or cancel pending backups using this connection first");
      JSONObject data = Vault.data(c);
      find(data, "connections", id).put("config", config);
      java.util.List<BackupJob> histories = JobStore.all(c);
      histories.addAll(pendingJobs(c));
      validateHistories(data, histories);
      Vault.store(c, data);
    }
  }

  static synchronized void saveJob(Context c, BackupJob job) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      synchronized (JobStore.class) {
        job.validate(false);
        validateJob(c, job);
        JobStore.save(c, job);
      }
    }
  }

  static synchronized void deleteJob(Context c, String id) throws Exception {
    try (Operations.SettingsGuard ignored = Operations.reserveSettings()) {
      JobStore.delete(c, id);
    }
  }

  private static java.util.List<BackupJob> pendingJobs(Context c) throws Exception {
    java.util.List<BackupJob> out = new java.util.ArrayList<>();
    JSONArray runs = PendingBackups.all(c);
    for (int i = 0; i < runs.length(); i++)
      out.add(BackupJob.from(runs.getJSONObject(i).getJSONObject("job")));
    return out;
  }

  static void validate(JSONObject vault) throws Exception {
    if (vault.getInt("version") != 2
        || (vault.has("pending_import") || vault.has("pending_import_runs")))
      throw new Exception("Unsupported vault format");
    HashSet<String> ids = new HashSet<>(), fps = new HashSet<>();
    for (String kind : new String[] {"connections", "keys"}) {
      JSONArray values = vault.getJSONArray(kind);
      if (values.length() > 100) throw new Exception("Too many saved keys or connections");
      for (int i = 0; i < values.length(); i++) {
        JSONObject value = values.getJSONObject(i);
        String id = value.getString("id");
        if (!id.matches("[A-Za-z0-9_-]{1,64}") || !ids.add(id))
          throw new Exception("Invalid or duplicate saved identity");
        label(value.getString("name"));
        if (kind.equals("connections")) {
          JSONObject cfg = value.getJSONObject("config");
          if (cfg.has("encryption_key")
              || cfg.has("decryption_keys")
              || cfg.has("encryption_enabled"))
            throw new Exception("Connection contains unexpected encryption settings");
          Mobilecore.newClient(cfg.toString());
        } else {
          String fp = Mobilecore.encryptionFingerprint(value.getString("key"));
          if (!fp.equals(value.getString("fingerprint")) || !fps.add(fp))
            throw new Exception("Invalid or duplicate encryption fingerprint");
        }
      }
    }
    String selected = vault.getString("default_connection");
    if (!selected.isEmpty()) find(vault, "connections", selected);
    if (selected.isEmpty() && vault.getJSONArray("connections").length() > 0)
      throw new Exception("Select a default connection");
  }

  static void validateJob(Context c, BackupJob job) throws Exception {
    JSONObject vault = Vault.data(c);
    java.util.List<BackupJob> jobs = JobStore.all(c);
    jobs.removeIf(other -> other.id.equals(job.id));
    jobs.add(job);
    jobs.addAll(pendingJobs(c));
    validateHistories(vault, jobs);
  }

  // Use the same origin normalization as the network client, including default
  // HTTPS ports, trailing slash and host casing. Import and editing share this check.
  static void validateHistories(JSONObject vault, java.util.List<BackupJob> jobs) throws Exception {
    java.util.Map<String, String> policies = new java.util.HashMap<>();
    java.util.Map<String, String> servers = new java.util.HashMap<>();
    for (BackupJob job : jobs) {
      validateJob(vault, job);
      String id =
          job.connectionID.isEmpty() ? vault.getString("default_connection") : job.connectionID;
      JSONObject config = find(vault, "connections", id).getJSONObject("config");
      String server = servers.get(id);
      if (server == null) {
        server = Mobilecore.canonicalServerURL(config.getString("server"));
        servers.put(id, server);
      }
      String backup = job.backupID.isEmpty() ? config.getString("host_id") : job.backupID;
      String history = server + "\n" + job.store + "\n" + job.namespace + "\n" + backup;
      String previous = policies.put(history, job.encryptionKeyID);
      if (previous != null && !previous.equals(job.encryptionKeyID))
        throw new Exception(
            "Jobs sharing this backup history must use the same encryption key setting");
    }
  }

  static void validateJob(JSONObject vault, BackupJob job) throws Exception {
    String id =
        job.connectionID.isEmpty() ? vault.optString("default_connection") : job.connectionID;
    find(vault, "connections", id);
    if (!job.encryptionKeyID.isEmpty()) find(vault, "keys", job.encryptionKeyID);
  }
}
