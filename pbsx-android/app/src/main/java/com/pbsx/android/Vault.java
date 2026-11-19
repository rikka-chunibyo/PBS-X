package com.pbsx.android;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;
import org.json.JSONArray;
import org.json.JSONObject;

/** Device-bound encrypted configuration. Android cloud/device backup is disabled. */
final class Vault {
  private static final String ALIAS = "pbsx.connection.v1";

  private static SecretKey key() throws Exception {
    KeyStore store = KeyStore.getInstance("AndroidKeyStore");
    store.load(null);
    if (store.containsAlias(ALIAS)) return (SecretKey) store.getKey(ALIAS, null);
    KeyGenerator generator =
        KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
    generator.init(
        new KeyGenParameterSpec.Builder(
                ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .build());
    return generator.generateKey();
  }

  private static synchronized void write(Context c, String json) throws Exception {
    Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
    cipher.init(Cipher.ENCRYPT_MODE, key());
    String value =
        Base64.encodeToString(cipher.getIV(), Base64.NO_WRAP)
            + ":"
            + Base64.encodeToString(
                cipher.doFinal(json.getBytes(StandardCharsets.UTF_8)), Base64.NO_WRAP);
    if (!c.getSharedPreferences("vault", 0).edit().putString("connection", value).commit())
      throw new Exception("Could not save connection");
  }

  private static synchronized String read(Context c) throws Exception {
    String value = c.getSharedPreferences("vault", 0).getString("connection", "");
    if (value.isEmpty()) return "";
    String[] parts = value.split(":", 2);
    if (parts.length != 2) throw new Exception("Connection storage is damaged");
    Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
    cipher.init(
        Cipher.DECRYPT_MODE,
        key(),
        new GCMParameterSpec(128, Base64.decode(parts[0], Base64.NO_WRAP)));
    return new String(
        cipher.doFinal(Base64.decode(parts[1], Base64.NO_WRAP)), StandardCharsets.UTF_8);
  }

  static synchronized JSONObject data(Context c) throws Exception {
    String raw = read(c);
    if (raw.isEmpty())
      return new JSONObject()
          .put("version", 2)
          .put("connections", new JSONArray())
          .put("keys", new JSONArray())
          .put("default_connection", "");
    JSONObject value = new JSONObject(raw);
    if (value.optInt("version") != 2)
      throw new Exception(
          "This test build uses a new vault format. Clear app storage and reconnect.");
    if (value.has("pending_import")) {
      SettingsTransfer.applyPreferences(c, value.getJSONObject("pending_import"));
      if (value.has("pending_import_runs"))
        PendingBackups.install(c, value.getJSONArray("pending_import_runs"));
      value.remove("pending_import_runs");
      value.remove("pending_import");
      write(c, value.toString());
    }
    return value;
  }

  static synchronized void store(Context c, JSONObject value) throws Exception {
    write(c, value.toString());
  }

  static synchronized void install(
      Context c, JSONObject value, JSONObject preferences, JSONArray pending) throws Exception {
    JSONObject next =
        new JSONObject(value.toString())
            .put("pending_import", preferences)
            .put("pending_import_runs", pending);
    write(c, next.toString());
    data(c); // Encrypted import journal completes interrupted preference writes on next startup.
  }

  static synchronized String load(Context c) throws Exception {
    return KeyProfiles.config(c, "", "", false);
  }

  // Test fixtures can update the selected default connection without a network request.
  static synchronized void save(Context c, String config) throws Exception {
    JSONObject data = data(c), cfg = new JSONObject(config);
    cfg.remove("decryption_keys");
    cfg.remove("encryption_key");
    cfg.remove("encryption_enabled");
    String id = data.optString("default_connection");
    JSONArray profiles = data.getJSONArray("connections");
    JSONObject entry = null;
    for (int i = 0; i < profiles.length(); i++)
      if (profiles.getJSONObject(i).getString("id").equals(id)) entry = profiles.getJSONObject(i);
    if (entry == null) {
      id = java.util.UUID.randomUUID().toString();
      entry = new JSONObject().put("id", id).put("name", "Default connection");
      profiles.put(entry);
    }
    entry.put("config", cfg);
    data.put("default_connection", id);
    store(c, data);
  }
}
