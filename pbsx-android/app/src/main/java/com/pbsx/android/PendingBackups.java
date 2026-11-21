package com.pbsx.android;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import com.pbsx.core.mobilecore.Mobilecore;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;
import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Durable private recovery records. Never calls the vault/jobs/scheduler while holding its lock.
 */
final class PendingBackups {
  private static final String ALIAS = "pbsx.pending.v1";
  private static final byte[] AAD = "PBS-X pending backups v1".getBytes(StandardCharsets.UTF_8);
  private static final int LIMIT = 8 * 1024 * 1024;
  private static String cachedPath;
  private static JSONArray cached;

  private static SecretKey key() throws Exception {
    KeyStore store = KeyStore.getInstance("AndroidKeyStore");
    store.load(null);
    if (store.containsAlias(ALIAS)) return (SecretKey) store.getKey(ALIAS, null);
    KeyGenerator generator = KeyGenerator.getInstance("AES", "AndroidKeyStore");
    generator.init(
        new KeyGenParameterSpec.Builder(
                ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
            .setKeySize(256)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .build());
    return generator.generateKey();
  }

  private static File path(Context c) {
    return new File(c.getFilesDir(), "pending-backups-v1");
  }

  static String sourceIdentity(String device, BackupJob job) throws Exception {
    byte[] bytes =
        java.security.MessageDigest.getInstance("SHA-256")
            .digest((device + "\n" + job.json().toString()).getBytes(StandardCharsets.UTF_8));
    StringBuilder out = new StringBuilder();
    for (byte b : bytes) out.append(String.format(java.util.Locale.ROOT, "%02x", b & 255));
    return out.toString();
  }

  static JSONArray validate(JSONArray runs) throws Exception {
    if (runs.length() > 100 || runs.toString().getBytes(StandardCharsets.UTF_8).length > LIMIT)
      throw new Exception("Too many pending backups");
    java.util.Set<String> ids = new java.util.HashSet<>(), histories = new java.util.HashSet<>();
    for (int i = 0; i < runs.length(); i++) {
      JSONObject run = runs.getJSONObject(i);
      BackupJob job = BackupJob.from(run.getJSONObject("job"));
      job.validate(true);
      if (!ids.add(job.id)
          || !histories.add(run.getString("history"))
          || !run.getString("history").matches("[a-f0-9-]{36}")
          || !run.getString("source").matches("[a-f0-9]{64}")
          || !run.getString("device").matches("[a-f0-9]{64}")
          || !java.util.Arrays.asList("running", "paused", "failed", "cancelling")
              .contains(run.getString("state"))) throw new Exception("Invalid pending backup");
      if (!sourceIdentity(run.getString("device"), job).equals(run.getString("source")))
        throw new Exception("Pending backup source settings have changed");
      run.getBoolean("automatic");
      if (run.has("occurrence")
          && (!(run.opt("occurrence") instanceof Number) || run.getLong("occurrence") < 0))
        throw new Exception("Invalid pending schedule occurrence");
      String descriptor = run.getString("descriptor");
      Mobilecore.validateBackupRun(descriptor);
      JSONObject nativeRun = new JSONObject(descriptor);
      if (!nativeRun.getString("job_id").equals(job.id)
          || !nativeRun.getString("source").equals(run.getString("source"))
          || !nativeRun.getString("store").equals(job.store)
          || !nativeRun.getString("namespace").equals(job.namespace)
          || (!job.backupID.isEmpty() && !nativeRun.getString("backup_id").equals(job.backupID)))
        throw new Exception("Pending backup target differs from its frozen job");
    }
    return new JSONArray(runs.toString());
  }

  static synchronized JSONArray all(Context c) throws Exception {
    File file = path(c);
    if (cached != null && file.getAbsolutePath().equals(cachedPath))
      return new JSONArray(cached.toString());
    DurableFiles.recoverLegacyBackup(file);
    byte[] sealed;
    try (java.io.FileInputStream input = new java.io.FileInputStream(file)) {
      if (input.getChannel().size() > LIMIT + 64)
        throw new Exception("Pending backup storage exceeds limit");
      java.io.ByteArrayOutputStream bytes = new java.io.ByteArrayOutputStream();
      byte[] buffer = new byte[8192];
      int count;
      while ((count = input.read(buffer)) != -1) {
        bytes.write(buffer, 0, count);
        if (bytes.size() > LIMIT + 64) throw new Exception("Pending backup storage exceeds limit");
      }
      sealed = bytes.toByteArray();
    } catch (java.io.FileNotFoundException e) {
      if (file.exists() || new File(file + ".bak").exists() || new File(file + ".new").exists())
        throw e;
      cachedPath = file.getAbsolutePath();
      cached = new JSONArray();
      return new JSONArray();
    }
    if (sealed.length < 29 || sealed[0] != 1)
      throw new Exception("Pending backup storage is damaged");
    Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
    cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, sealed, 1, 12));
    cipher.updateAAD(AAD);
    JSONArray runs =
        validate(
            new JSONArray(
                new String(
                    cipher.doFinal(sealed, 13, sealed.length - 13), StandardCharsets.UTF_8)));
    cachedPath = file.getAbsolutePath();
    cached = runs;
    return new JSONArray(runs.toString());
  }

  static synchronized void install(Context c, JSONArray runs) throws Exception {
    JSONArray checked = validate(runs);
    Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
    cipher.init(Cipher.ENCRYPT_MODE, key());
    cipher.updateAAD(AAD);
    byte[] sealed = cipher.doFinal(checked.toString().getBytes(StandardCharsets.UTF_8));
    try {
      DurableFiles.ensureDirectory(c.getFilesDir());
      byte[] stored =
          java.nio.ByteBuffer.allocate(13 + sealed.length)
              .put((byte) 1)
              .put(cipher.getIV())
              .put(sealed)
              .array();
      DurableFiles.write(path(c), stored);
    } catch (Exception e) {
      cached = null;
      cachedPath = null;
      throw e;
    }
    cachedPath = path(c).getAbsolutePath();
    cached = checked;
  }

  static synchronized JSONObject get(Context c, String jobID) throws Exception {
    JSONArray all = all(c);
    for (int i = 0; i < all.length(); i++) {
      JSONObject run = all.getJSONObject(i);
      if (jobID.equals(run.getJSONObject("job").getString("id"))) return run;
    }
    return null;
  }

  static synchronized void put(Context c, JSONObject run) throws Exception {
    String id = run.getJSONObject("job").getString("id");
    JSONArray all = all(c), out = new JSONArray();
    for (int i = 0; i < all.length(); i++) {
      JSONObject previous = all.getJSONObject(i);
      if (id.equals(previous.getJSONObject("job").getString("id"))) {
        if (!run.getString("history").equals(previous.getString("history")))
          throw new Exception("This job already has an unfinished task");
      } else out.put(all.get(i));
    }
    out.put(run);
    install(c, out);
  }

  /** Terminal tasks retain cleanup secrets, not a resumable job state. */
  static boolean cleanupOnly(JSONObject run) {
    return run != null
        && ("failed".equals(run.optString("state")) || "cancelling".equals(run.optString("state")));
  }

  static boolean canResume(JSONObject run) {
    return run != null && !cleanupOnly(run);
  }

  static synchronized void remove(Context c, String jobID) throws Exception {
    JSONArray all = all(c), out = new JSONArray();
    for (int i = 0; i < all.length(); i++)
      if (!jobID.equals(all.getJSONObject(i).getJSONObject("job").getString("id")))
        out.put(all.get(i));
    install(c, out);
  }

  static synchronized void forgetCache() {
    cached = null;
    cachedPath = null;
  }
}
