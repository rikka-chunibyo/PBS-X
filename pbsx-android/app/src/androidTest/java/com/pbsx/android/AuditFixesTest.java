package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.UUID;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class AuditFixesTest {
  private Context isolated() {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String prefix = "audit-fix-" + UUID.randomUUID() + "-";
    return new ContextWrapper(base) {
      @Override
      public SharedPreferences getSharedPreferences(String name, int mode) {
        return base.getSharedPreferences(prefix + name, mode);
      }
    };
  }

  private JSONObject config(String server) throws Exception {
    return new JSONObject()
        .put("server", server)
        .put("host_id", "audit-phone")
        .put("fingerprint", new String(new char[64]).replace('\0', '0'))
        .put("auth_id", "audit@pbs!phone")
        .put("secret", "dummy");
  }

  private interface Checked {
    void run() throws Exception;
  }

  private void rejected(Checked operation) throws Exception {
    try {
      operation.run();
      fail("Invalid operation was accepted");
    } catch (Exception expected) {
      assertNotNull(expected.getMessage());
    }
  }

  @Test
  public void equivalentOriginsCannotMixHistoryEncryption() throws Exception {
    for (String alias :
        new String[] {
          "https://fixture.invalid/", "https://FIXTURE.invalid:443/", "https://fixture.invalid:0443"
        }) {
      Context c = isolated();
      KeyProfiles.addConnection(c, "A", config("https://fixture.invalid").toString());
      String b = KeyProfiles.addConnection(c, "B", config(alias).toString());
      String key = KeyProfiles.addKey(c, "Key", Mobilecore.generateEncryptionKey());
      BackupJob encrypted = BackupJob.create(c);
      encrypted.encryptionKeyID = key;
      JobStore.save(c, encrypted);
      BackupJob plain = BackupJob.create(c);
      plain.connectionID = b;
      plain.backupID = encrypted.backupID;
      rejected(() -> KeyProfiles.validateJob(c, plain));
      // An old saved conflict must also be rejected by export/import validation.
      JobStore.save(c, plain);
      rejected(() -> SettingsTransfer.exportJSON(c));
      JSONObject exported =
          new JSONObject()
              .put("format", "pbsx-android-settings")
              .put("version", 1)
              .put("vault", Vault.data(c))
              .put(
                  "preferences",
                  new JSONObject()
                      .put("settings", SettingsTransfer.preferences(Settings.prefs(c)))
                      .put(
                          "jobs-v1",
                          SettingsTransfer.preferences(c.getSharedPreferences("jobs-v1", 0))));
      rejected(() -> SettingsTransfer.importJSON(c, exported.toString()));
      plain.encryptionKeyID = key;
      KeyProfiles.validateJob(c, plain);
      JobStore.save(c, plain);
      SettingsTransfer.validate(SettingsTransfer.exportJSON(c));
    }
  }

  @Test
  public void profileUpdatesCannotIntroduceConflictingHistories() throws Exception {
    Context c = isolated();
    KeyProfiles.addConnection(c, "A", config("https://a.invalid").toString());
    String b = KeyProfiles.addConnection(c, "B", config("https://b.invalid").toString());
    BackupJob first = BackupJob.create(c);
    first.encryptionKeyID = KeyProfiles.addKey(c, "Key", Mobilecore.generateEncryptionKey());
    JobStore.save(c, first);
    BackupJob second = BackupJob.create(c);
    second.connectionID = b;
    second.backupID = first.backupID;
    KeyProfiles.validateJob(c, second);
    JobStore.save(c, second);
    String before = Vault.data(c).toString();
    rejected(() -> KeyProfiles.updateConnection(c, b, config("https://A.invalid:443/")));
    assertEquals(before, Vault.data(c).toString());
  }

  @Test
  public void defaultChangesCannotIntroduceConflictingHistories() throws Exception {
    Context c = isolated();
    KeyProfiles.addConnection(c, "A", config("https://a.invalid").toString());
    String b = KeyProfiles.addConnection(c, "B", config("https://b.invalid").toString());
    BackupJob first = BackupJob.create(c);
    first.connectionID = "";
    first.encryptionKeyID = KeyProfiles.addKey(c, "Key", Mobilecore.generateEncryptionKey());
    JobStore.save(c, first);
    BackupJob second = BackupJob.create(c);
    second.connectionID = b;
    second.backupID = first.backupID;
    JobStore.save(c, second);
    String before = Vault.data(c).toString();
    rejected(() -> KeyProfiles.setDefault(c, b));
    assertEquals(before, Vault.data(c).toString());
  }

  @Test
  public void wrongKnownPreferenceTypesLeaveAllSavedStateUntouched() throws Exception {
    Context c = isolated();
    KeyProfiles.addConnection(c, "A", config("https://fixture.invalid").toString());
    Settings.prefs(c).edit().putString("status", "Keep this status").commit();
    String original = SettingsTransfer.exportJSON(c);
    String vault = Vault.data(c).toString();
    java.util.Map<String, ?> settings = Settings.prefs(c).getAll();
    java.util.Map<String, ?> jobs = c.getSharedPreferences("jobs-v1", 0).getAll();
    for (String name :
        new String[] {"store", "namespace", "sources", "status", "hours", "status_time"}) {
      JSONObject changed = new JSONObject(original);
      changed
          .getJSONObject("preferences")
          .getJSONObject("settings")
          .put(name, new JSONObject().put("type", "boolean").put("value", true));
      rejected(() -> SettingsTransfer.validate(changed.toString()));
      rejected(() -> SettingsTransfer.importJSON(c, changed.toString()));
      assertEquals(vault, Vault.data(c).toString());
      assertEquals(settings, Settings.prefs(c).getAll());
      assertEquals(jobs, c.getSharedPreferences("jobs-v1", 0).getAll());
    }
    assertEquals("backup", BackupJob.create(c).store);
  }

  @Test
  public void malformedLegacyFolderDefaultsAreRejectedBeforeMigration() throws Exception {
    Context c = isolated();
    KeyProfiles.addConnection(c, "A", config("https://fixture.invalid").toString());
    String original = SettingsTransfer.exportJSON(c);
    String before = Vault.data(c).toString();
    for (String value :
        new String[] {
          "{}", "[1]", "[{\"label\":\"../escape\",\"uri\":\"content://fixture/tree/photos\"}]"
        }) {
      JSONObject exported = new JSONObject(original);
      exported.getJSONObject("preferences").getJSONObject("jobs-v1").remove("jobs");
      exported
          .getJSONObject("preferences")
          .getJSONObject("settings")
          .put("sources", new JSONObject().put("type", "string").put("value", value));
      rejected(() -> SettingsTransfer.importJSON(c, exported.toString()));
      assertEquals(before, Vault.data(c).toString());
      assertTrue(JobStore.all(c).isEmpty());
    }
  }

  @Test
  public void validKnownPreferencesAndUnknownTypedValuesRoundTrip() throws Exception {
    Context c = isolated(), target = isolated();
    KeyProfiles.addConnection(c, "A", config("https://fixture.invalid").toString());
    Settings.prefs(c)
        .edit()
        .putString("store", "family")
        .putString("namespace", "phones")
        .putString("sources", "[]")
        .putString("status", "Ready")
        .putLong("status_time", 123L)
        .putInt("hours", 24)
        .putFloat("future-custom-value", 1.25f)
        .commit();
    SettingsTransfer.importJSON(target, SettingsTransfer.exportJSON(c));
    assertEquals("family", Settings.store(target));
    assertEquals("phones", Settings.namespace(target));
    assertEquals(123L, Settings.prefs(target).getLong("status_time", 0));
    assertEquals(1.25f, Settings.prefs(target).getFloat("future-custom-value", 0), 0);
  }
}
