package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class SettingsSafetyTest {
  private Context isolated() {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String prefix = "settings-safety-" + UUID.randomUUID() + "-";
    return new ContextWrapper(base) {
      @Override
      public SharedPreferences getSharedPreferences(String name, int mode) {
        return base.getSharedPreferences(prefix + name, mode);
      }
    };
  }

  private void connect(Context c) throws Exception {
    KeyProfiles.addConnection(
        c,
        "Fixture",
        new JSONObject()
            .put("server", "https://fixture.invalid:8007")
            .put("host_id", "audit-phone")
            .put("fingerprint", new String(new char[64]).replace('\0', '0'))
            .put("auth_id", "fixture@pbs!phone")
            .put("secret", "dummy")
            .toString());
  }

  private BackupJob job(Context c) throws Exception {
    BackupJob job = BackupJob.create(c);
    job.sources.put(
        new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
    return job;
  }

  private JSONObject jobs(JSONObject root) throws Exception {
    return root.getJSONObject("preferences").getJSONObject("jobs-v1");
  }

  private JSONArray list(JSONObject root) throws Exception {
    return new JSONArray(jobs(root).getJSONObject("jobs").getString("value"));
  }

  private void replaceJobs(JSONObject root, JSONArray values) throws Exception {
    jobs(root).getJSONObject("jobs").put("value", values.toString());
  }

  private interface Mutation {
    void apply(JSONObject root) throws Exception;
  }

  @Test
  public void brokenJobReferencesAndSchedulerIdentitiesCannotReplaceSavedState() throws Exception {
    Context c = isolated();
    connect(c);
    BackupJob saved = job(c);
    saved.hours = 24;
    saved.encryptionKeyID =
        KeyProfiles.addKey(c, "Fixture key", Mobilecore.generateEncryptionKey());
    JobStore.save(c, saved);
    Settings.prefs(c).edit().putString("status", "Keep the original settings").commit();
    String original = SettingsTransfer.exportJSON(c);
    String vault = Vault.data(c).toString();
    Map<String, ?> prefs = Settings.prefs(c).getAll();
    Map<String, ?> jobPrefs = c.getSharedPreferences("jobs-v1", 0).getAll();
    Mutation[] cases = {
      root -> {
        JSONArray values = list(root);
        values.getJSONObject(0).put("encryption_key_id", "missing-key");
        replaceJobs(root, values);
      },
      root -> {
        JSONArray values = list(root);
        values.getJSONObject(0).put("connection_id", "missing-profile");
        replaceJobs(root, values);
      },
      root -> {
        JSONArray values = list(root);
        values.put(
            new JSONObject(values.getJSONObject(0).toString())
                .put("scheduler_id", saved.schedulerId + 1));
        replaceJobs(root, values);
        jobs(root).getJSONObject("next_scheduler_id").put("value", saved.schedulerId + 2);
      },
      root -> {
        JSONArray values = list(root);
        values.put(
            new JSONObject(values.getJSONObject(0).toString())
                .put("id", UUID.randomUUID().toString()));
        replaceJobs(root, values);
      },
      root -> jobs(root).getJSONObject("next_scheduler_id").put("value", saved.schedulerId),
      root -> {
        JSONArray values = list(root);
        values.getJSONObject(0).put("scheduler_id", 72);
        replaceJobs(root, values);
      },
      root ->
          jobs(root)
              .put("next_scheduler_id", new JSONObject().put("type", "boolean").put("value", true))
    };
    String[] expected = {
      "encryption key",
      "connection",
      "duplicate job/schedule identity",
      "duplicate job/schedule identity",
      "overlaps",
      "job/schedule identity",
      "identity type"
    };
    for (int i = 0; i < cases.length; i++) {
      JSONObject changed = new JSONObject(original);
      cases[i].apply(changed);
      try {
        SettingsTransfer.importJSON(c, changed.toString());
        fail("Invalid import accepted: " + i);
      } catch (Exception e) {
        assertTrue(
            "case " + i + ": " + e.getMessage(),
            e.getMessage() != null && e.getMessage().contains(expected[i]));
      }
      assertEquals(vault, Vault.data(c).toString());
      assertEquals(prefs, Settings.prefs(c).getAll());
      assertEquals(jobPrefs, c.getSharedPreferences("jobs-v1", 0).getAll());
    }
    assertEquals(saved.json().toString(), JobStore.get(c, saved.id).json().toString());
  }

  @Test
  public void settingsImportDuringActiveOperationLeavesStateUntouched() throws Exception {
    Context c = isolated();
    connect(c);
    JobStore.save(c, job(c));
    String original = SettingsTransfer.exportJSON(c);
    String vault = Vault.data(c).toString();
    Map<String, ?> prefs = Settings.prefs(c).getAll();
    Map<String, ?> jobPrefs = c.getSharedPreferences("jobs-v1", 0).getAll();
    CountDownLatch started = new CountDownLatch(1), release = new CountDownLatch(1);
    AtomicReference<Throwable> failure = new AtomicReference<>();
    assertFalse(Operations.running());
    Thread active =
        new Thread(
            () -> {
              try {
                Operations.changeSettings(
                    () -> {
                      started.countDown();
                      if (!release.await(10, TimeUnit.SECONDS))
                        throw new Exception("Fixture timed out");
                    });
              } catch (Throwable error) {
                failure.set(error);
              }
            },
            "settings-safety-busy-fixture");
    active.start();
    try {
      assertTrue(started.await(5, TimeUnit.SECONDS));
      try {
        SettingsTransfer.importJSON(c, original);
        fail("Import replaced settings during an active operation");
      } catch (Operations.BusyException expected) {
      }
      assertEquals(vault, Vault.data(c).toString());
      assertEquals(prefs, Settings.prefs(c).getAll());
      assertEquals(jobPrefs, c.getSharedPreferences("jobs-v1", 0).getAll());
    } finally {
      release.countDown();
      active.join(5000);
      assertFalse("Fixture thread did not stop", active.isAlive());
    }
    if (failure.get() != null) throw new AssertionError(failure.get());
    assertFalse(Operations.running());
    SettingsTransfer.importJSON(c, original); // Busy rejection must not poison the next import.
    assertEquals(1, JobStore.all(c).size());
  }

  @Test
  public void distinctHistoriesMayUseDifferentEncryptionKeys() throws Exception {
    for (String field : new String[] {"store", "namespace", "backup_id"}) {
      Context c = isolated();
      connect(c);
      BackupJob first = job(c);
      first.encryptionKeyID =
          KeyProfiles.addKey(c, "First key", Mobilecore.generateEncryptionKey());
      JobStore.save(c, first);
      BackupJob second = job(c);
      second.backupID = first.backupID;
      second.encryptionKeyID =
          KeyProfiles.addKey(c, "Second key", Mobilecore.generateEncryptionKey());
      if (field.equals("store")) second.store = "archive";
      if (field.equals("namespace")) second.namespace = "phones/other";
      if (field.equals("backup_id")) second.backupID = "other-files";
      KeyProfiles.validateJob(c, second);
      JobStore.save(c, second);
      Context imported = isolated();
      SettingsTransfer.importJSON(imported, SettingsTransfer.exportJSON(c));
      assertEquals(first.encryptionKeyID, JobStore.get(imported, first.id).encryptionKeyID);
      assertEquals(second.encryptionKeyID, JobStore.get(imported, second.id).encryptionKeyID);
    }
  }

  @Test
  public void unicodeRecoveryPassphraseMatchesIndependentUTF8Vector() throws Exception {
    String passphrase = "Café recovery 🐱 phrase 2026";
    String expected = "07871996c1e8b7a9cd520dd9f649e1a11bf7a8ccc798e7ea7cb02e5e6c248fba";
    assertEquals(expected, Mobilecore.deriveEncryptionKey(passphrase));
    Context c = isolated();
    connect(c);
    String id =
        KeyProfiles.addKey(
            c, "Unicode recovery fixture", Mobilecore.deriveEncryptionKey(passphrase));
    Context imported = isolated();
    SettingsTransfer.importJSON(imported, SettingsTransfer.exportJSON(c));
    assertEquals(expected, KeyProfiles.find(Vault.data(imported), "keys", id).getString("key"));
    assertFalse(Vault.data(imported).toString().contains(passphrase));
  }
}
