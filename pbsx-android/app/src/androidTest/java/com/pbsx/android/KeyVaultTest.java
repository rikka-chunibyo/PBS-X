package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.HashSet;
import java.util.Set;
import java.util.UUID;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class KeyVaultTest {
  private Context isolated() {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String prefix = "vault-fixture-" + UUID.randomUUID() + "-";
    return new ContextWrapper(base) {
      @Override
      public SharedPreferences getSharedPreferences(String name, int mode) {
        return base.getSharedPreferences(prefix + name, mode);
      }
    };
  }

  private JSONObject connection(String secret) throws Exception {
    return new JSONObject()
        .put("server", "https://fixture.invalid:8007")
        .put("host_id", "test-phone")
        .put("fingerprint", new String(new char[64]).replace('\0', '0'))
        .put("auth_id", "fixture@pbs!phone")
        .put("secret", secret);
  }

  @Test
  public void completeSettingsRoundTripIncludesJobsKeysPreferencesAndHistory() throws Exception {
    Context from = isolated(), to = isolated();
    String profile =
        KeyProfiles.addConnection(
            from, "Family server", connection("private-api-fixture").toString());
    String key = KeyProfiles.addKey(from, "Photos encryption", Mobilecore.generateEncryptionKey());
    BackupJob job = BackupJob.create(from);
    job.connectionID = profile;
    job.encryptionKeyID = key;
    job.name = "Camera roll";
    job.hours = 24;
    job.charging = false;
    job.wifi = false;
    job.shizuku = true;
    job.apks = true;
    job.allApps = false;
    job.packages.put("com.pbsx.android");
    job.exports.put("contacts");
    job.sources.put(
        new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
    JobStore.save(from, job);
    String run = JobStore.start(from, job, "backup");
    JobStore.finish(from, run, true, "Completed fixture");
    Set<String> strings = new HashSet<>();
    strings.add("one");
    strings.add("two");
    Settings.prefs(from)
        .edit()
        .putBoolean("wifi", false)
        .putLong("fixture-long", 1234567890123L)
        .putInt("fixture-int", 7)
        .putFloat("fixture-float", 1.5f)
        .putStringSet("fixture-set", strings)
        .putString("status", "Fixture complete")
        .commit();
    String plain = SettingsTransfer.exportJSON(from);
    assertTrue(plain.contains("private-api-fixture"));
    assertTrue(plain.contains("Camera roll"));
    assertTrue(plain.contains("Completed fixture"));
    String encrypted = Mobilecore.encryptSettings(plain, "export-only-password");
    assertFalse(encrypted.contains("private-api-fixture"));
    assertFalse(encrypted.contains("Camera roll"));
    String decoded = Mobilecore.decryptSettings(encrypted, "export-only-password");
    assertEquals(plain, decoded);
    SettingsTransfer.importJSON(to, decoded);
    assertEquals(1, JobStore.all(to).size());
    BackupJob restored = JobStore.get(to, job.id);
    assertEquals(job.json().toString(), restored.json().toString());
    assertEquals("Completed fixture", JobStore.history(to).getJSONObject(0).getString("message"));
    assertEquals(run, JobStore.history(to).getJSONObject(0).getString("id"));
    assertEquals(
        "private-api-fixture",
        KeyProfiles.connection(to, profile).getJSONObject("config").getString("secret"));
    assertEquals(
        Vault.data(from).getJSONArray("keys").toString(),
        Vault.data(to).getJSONArray("keys").toString());
    assertEquals(1234567890123L, Settings.prefs(to).getLong("fixture-long", 0));
    assertEquals(7, Settings.prefs(to).getInt("fixture-int", 0));
    assertEquals(1.5f, Settings.prefs(to).getFloat("fixture-float", 0), 0);
    assertEquals(strings, Settings.prefs(to).getStringSet("fixture-set", null));
    assertFalse(Settings.prefs(to).getBoolean("wifi", true));
    assertTrue(Settings.prefs(to).getBoolean("imported_schedule_hold", false));
    String saved = to.getSharedPreferences("vault", 0).getString("connection", "");
    assertFalse(saved.contains("private-api-fixture"));
    assertFalse(saved.contains("Photos encryption"));
    assertFalse(Vault.data(to).toString().contains("export-only-password"));
  }

  @Test
  public void jobsChooseIndependentCredentialsAndReferencedEntriesCannotBeDeleted()
      throws Exception {
    Context c = isolated();
    String a = KeyProfiles.addConnection(c, "Phone", connection("phone-secret").toString()),
        b = KeyProfiles.addConnection(c, "Family", connection("family-secret").toString()),
        key = KeyProfiles.addKey(c, "Encryption", Mobilecore.generateEncryptionKey());
    BackupJob job = BackupJob.create(c);
    job.connectionID = b;
    job.encryptionKeyID = key;
    JobStore.save(c, job);
    JSONObject config =
        new JSONObject(KeyProfiles.config(c, job.connectionID, job.encryptionKeyID, true));
    assertEquals("family-secret", config.getString("secret"));
    assertTrue(config.getBoolean("encryption_enabled"));
    assertEquals(64, config.getString("encryption_key").length());
    JSONObject restore = new JSONObject(KeyProfiles.config(c, a, "", false));
    assertEquals("phone-secret", restore.getString("secret"));
    assertFalse(restore.getBoolean("encryption_enabled"));
    assertEquals(1, restore.getJSONArray("decryption_keys").length());
    for (String[] entry : new String[][] {{"connections", b}, {"keys", key}}) {
      try {
        KeyProfiles.remove(c, entry[0], entry[1]);
        fail("referenced entry deleted");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("Reassign"));
      }
    }
    BackupJob shared = BackupJob.create(c);
    shared.backupID = job.backupID;
    shared.namespace = job.namespace;
    try {
      KeyProfiles.validateJob(c, shared);
      fail("mixed encryption policy accepted");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("same encryption"));
    }
    try {
      KeyProfiles.config(c, b, "missing-key", true);
      fail("missing key silently disabled encryption");
    } catch (Exception expected) {
    }
  }

  @Test
  public void invalidImportLeavesAllExistingStateUntouched() throws Exception {
    Context c = isolated();
    KeyProfiles.addConnection(c, "Original", connection("keep-original").toString());
    KeyProfiles.addKey(c, "Original key", Mobilecore.generateEncryptionKey());
    JSONObject original = new JSONObject(SettingsTransfer.exportJSON(c));
    String before = Vault.data(c).toString();
    MapSnapshot snapshot = new MapSnapshot(c);
    JSONObject invalid = new JSONObject(original.toString());
    invalid
        .getJSONObject("vault")
        .getJSONArray("keys")
        .getJSONObject(0)
        .put("fingerprint", "wrong");
    try {
      SettingsTransfer.importJSON(c, invalid.toString());
      fail("invalid fingerprint accepted");
    } catch (Exception expected) {
    }
    assertEquals(before, Vault.data(c).toString());
    snapshot.assertUnchanged(c);
    invalid = new JSONObject(original.toString());
    invalid.put("version", 999);
    try {
      SettingsTransfer.importJSON(c, invalid.toString());
      fail("future version accepted");
    } catch (Exception expected) {
    }
    assertEquals(before, Vault.data(c).toString());
    snapshot.assertUnchanged(c);
  }

  @Test
  public void encryptedImportJournalResumesInterruptedPreferenceWrites() throws Exception {
    Context from = isolated(), to = isolated();
    KeyProfiles.addConnection(from, "Recovered", connection("journal-secret").toString());
    Settings.prefs(from).edit().putString("status", "Recovered settings").commit();
    JSONObject exported = new JSONObject(SettingsTransfer.exportJSON(from));
    JSONObject data = exported.getJSONObject("vault");
    data.put("pending_import", exported.getJSONObject("preferences"));
    Vault.store(to, data);
    assertFalse(Vault.data(to).has("pending_import"));
    assertEquals("Recovered settings", Settings.prefs(to).getString("status", ""));
    assertEquals("journal-secret", new JSONObject(Vault.load(to)).getString("secret"));
    assertFalse(
        to.getSharedPreferences("vault", 0).getString("connection", "").contains("journal-secret"));
  }

  @Test
  public void memorizedPassphraseRecreatesKeyAfterReset() throws Exception {
    String passphrase = "fixture custom key 0123456789A";
    Context original = isolated(), reset = isolated();
    String first =
        KeyProfiles.addKey(original, "Original", Mobilecore.deriveEncryptionKey(passphrase));
    String next =
        KeyProfiles.addKey(
            reset, "Re-entered after reset", Mobilecore.deriveEncryptionKey(passphrase));
    JSONObject a = KeyProfiles.find(Vault.data(original), "keys", first),
        b = KeyProfiles.find(Vault.data(reset), "keys", next);
    assertEquals(a.getString("fingerprint"), b.getString("fingerprint"));
    assertEquals(a.getString("key"), b.getString("key"));
    assertFalse(Vault.data(original).toString().contains(passphrase));
    assertFalse(Vault.data(reset).toString().contains(passphrase));
  }

  private static final class MapSnapshot {
    final java.util.Map<String, ?> settings, jobs;

    MapSnapshot(Context c) {
      settings = Settings.prefs(c).getAll();
      jobs = c.getSharedPreferences("jobs-v1", 0).getAll();
    }

    void assertUnchanged(Context c) {
      assertEquals(settings, Settings.prefs(c).getAll());
      assertEquals(jobs, c.getSharedPreferences("jobs-v1", 0).getAll());
    }
  }
}
