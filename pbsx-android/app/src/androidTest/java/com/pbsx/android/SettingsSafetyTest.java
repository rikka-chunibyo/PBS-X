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
    java.io.File privateFiles = new java.io.File(base.getFilesDir(), prefix);
    if (!privateFiles.mkdirs())
      throw new IllegalStateException("Cannot create fixture files directory");
    return new ContextWrapper(base) {
      @Override
      public java.io.File getFilesDir() {
        return privateFiles;
      }

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

  private interface Edit {
    void apply(Context context) throws Exception;
  }

  @Test
  public void credentialJobAndExportOperationsReserveTheTransferGate() throws Exception {
    Context c = isolated();
    connect(c);
    String first = KeyProfiles.selected(c);
    String config = KeyProfiles.connection(c, first).getJSONObject("config").toString();
    String other = KeyProfiles.addConnection(c, "Other", config);
    String key = KeyProfiles.addKey(c, "Unused", Mobilecore.generateEncryptionKey());
    String freshKey = Mobilecore.generateEncryptionKey();
    BackupJob saved = job(c);
    JobStore.save(c, saved);
    Edit[] edits = {
      context -> KeyProfiles.addConnection(context, "New", config),
      context -> KeyProfiles.addKey(context, "New key", freshKey),
      context -> KeyProfiles.setDefault(context, other),
      context -> KeyProfiles.rename(context, "connections", first, "Renamed"),
      context -> KeyProfiles.updateConnection(context, first, new JSONObject(config)),
      context -> KeyProfiles.remove(context, "keys", key),
      context -> KeyProfiles.saveJob(context, saved),
      context -> KeyProfiles.deleteJob(context, saved.id),
      context -> SettingsTransfer.exportJSON(context)
    };
    for (Edit edit : edits) {
      CountDownLatch entered = new CountDownLatch(1), release = new CountDownLatch(1);
      java.util.concurrent.atomic.AtomicBoolean intercept =
          new java.util.concurrent.atomic.AtomicBoolean(true);
      AtomicReference<Throwable> failure = new AtomicReference<>();
      Context blocked =
          new ContextWrapper(c) {
            @Override
            public SharedPreferences getSharedPreferences(String name, int mode) {
              if ((name.equals("vault") || name.equals("jobs-v1"))
                  && intercept.compareAndSet(true, false)) {
                entered.countDown();
                try {
                  if (!release.await(10, TimeUnit.SECONDS))
                    throw new AssertionError("Vault fixture timed out");
                } catch (InterruptedException e) {
                  throw new AssertionError(e);
                }
              }
              return super.getSharedPreferences(name, mode);
            }
          };
      Thread worker =
          new Thread(
              () -> {
                try {
                  edit.apply(blocked);
                } catch (Throwable error) {
                  failure.set(error);
                }
              },
              "credential-write-race-fixture");
      worker.start();
      try {
        assertTrue("Edit never reached the vault", entered.await(5, TimeUnit.SECONDS));
        assertTrue("Transfer gate was released during credential save", Operations.running());
        try {
          Operations.changeSettings(
              () -> {
                throw new AssertionError("Second operation entered");
              });
          fail("A transfer could start during a credential write");
        } catch (Operations.BusyException expected) {
        }
      } finally {
        release.countDown();
        worker.join(5000);
        assertFalse("Credential writer did not finish", worker.isAlive());
      }
      if (failure.get() != null) throw new AssertionError(failure.get());
      assertFalse("Edit left the transfer gate locked", Operations.running());
    }
    try {
      KeyProfiles.addConnection(c, "", config);
      fail("Invalid edit accepted");
    } catch (Exception expected) {
    }
    assertFalse("Failed edit left the transfer gate locked", Operations.running());
  }

  @Test
  public void busyEnrollmentIsRejectedBeforeConsumingACodeAndReleasesOnFailure() throws Exception {
    Context c = isolated();
    Operations.changeSettings(
        () -> {
          try {
            KeyProfiles.enrollConnection(
                c, "Fixture", "invalid-code-must-not-be-parsed", "Fixture");
            fail("Enrollment entered an active transfer");
          } catch (Operations.BusyException expected) {
          }
        });
    try {
      KeyProfiles.enrollConnection(c, "Fixture", "invalid-code", "Fixture");
      fail("Invalid code was accepted");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("enrollment"));
    }
    assertFalse(Operations.running());
    assertEquals(0, Vault.data(c).getJSONArray("connections").length());
  }

  @Test
  public void jobSaveChecksNormalizedHistoryPolicyAndKeepsOldSettingsOnFailure() throws Exception {
    Context c = isolated();
    connect(c);
    BackupJob first = job(c);
    first.namespace = "photos";
    first.encryptionKeyID = KeyProfiles.addKey(c, "Photos", Mobilecore.generateEncryptionKey());
    KeyProfiles.saveJob(c, first);
    BackupJob other = job(c);
    other.namespace = " photos ";
    other.backupID = first.backupID;
    try {
      KeyProfiles.saveJob(c, other);
      fail("Saved a different encryption policy under the same normalized history");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("same encryption"));
    }
    assertFalse(Operations.running());
    assertEquals(1, JobStore.all(c).size());
    Operations.changeSettings(
        () -> {
          try {
            KeyProfiles.saveJob(c, first);
            fail("Edited a job during a transfer");
          } catch (Operations.BusyException expected) {
          }
          try {
            KeyProfiles.deleteJob(c, first.id);
            fail("Deleted a job during a transfer");
          } catch (Operations.BusyException expected) {
          }
        });
    assertEquals(first.json().toString(), JobStore.get(c, first.id).json().toString());
  }

  @Test
  public void interruptedImportIsRecoveredBeforeCredentialAndJobReferenceChecks() throws Exception {
    Context from = isolated();
    connect(from);
    String connection = KeyProfiles.selected(from);
    String key = KeyProfiles.addKey(from, "Recovery fixture", Mobilecore.generateEncryptionKey());
    JSONObject task = new PendingBackupsTest().run(from);
    BackupJob frozen = BackupJob.from(task.getJSONObject("job"));
    frozen.connectionID = connection;
    frozen.encryptionKeyID = key;
    JobStore.save(from, frozen);
    String source = PendingBackups.sourceIdentity(task.getString("device"), frozen);
    JSONObject descriptor = new JSONObject(task.getString("descriptor")).put("source", source);
    task.put("job", frozen.json()).put("source", source).put("descriptor", descriptor.toString());
    PendingBackups.put(from, task);
    JSONObject exported = new JSONObject(SettingsTransfer.exportJSON(from));
    JSONObject originalConfig = KeyProfiles.connection(from, connection).getJSONObject("config");
    for (int action = 0; action < 3; action++) {
      Context to = isolated();
      try {
        Vault.store(
            to,
            new JSONObject(exported.getJSONObject("vault").toString())
                .put("pending_import", exported.getJSONObject("preferences"))
                .put("pending_import_runs", exported.getJSONArray("pending_backups")));
        try {
          if (action == 0) KeyProfiles.remove(to, "keys", key);
          else if (action == 1)
            KeyProfiles.updateConnection(
                to,
                connection,
                new JSONObject(originalConfig.toString()).put("secret", "different"));
          else KeyProfiles.deleteJob(to, frozen.id);
          fail("Mutation ignored references in the interrupted import: " + action);
        } catch (Exception expected) {
          assertTrue(
              expected.getMessage(),
              expected.getMessage().contains(action == 0 ? "Reassign" : "pending"));
        }
        assertFalse(Operations.running());
        assertFalse(Vault.data(to).has("pending_import"));
        assertEquals(
            originalConfig.toString(),
            KeyProfiles.connection(to, connection).getJSONObject("config").toString());
        assertEquals(key, KeyProfiles.find(Vault.data(to), "keys", key).getString("id"));
        assertEquals(
            task.getString("descriptor"),
            PendingBackups.get(to, frozen.id).getString("descriptor"));
        assertEquals(frozen.json().toString(), JobStore.get(to, frozen.id).json().toString());
      } finally {
        ExportCache.remove(to.getFilesDir());
        PendingBackups.forgetCache();
      }
    }
    ExportCache.remove(from.getFilesDir());
    PendingBackups.forgetCache();
  }

  @Test
  public void staleScheduledCallbacksCannotStartDisabledOrSupersededOccurrences() throws Exception {
    Context c = isolated();
    connect(c);
    BackupJob requested = job(c);
    requested.hours = 12;
    JobStore.save(c, requested);
    long due = ScheduleState.due(c, requested, false);
    BackupJob disabled = BackupJob.from(requested.json());
    disabled.hours = 0;
    JobStore.save(c, disabled);
    for (boolean enabled : new boolean[] {false, true}) {
      if (enabled) JobStore.save(c, requested);
      Operations.Cancellation token = new Operations.Cancellation();
      token.scheduled = true;
      token.occurrence = enabled ? due - 1 : due;
      assertEquals(
          "Scheduled occurrence is no longer current",
          Operations.runJob(c, requested, null, null, 0, "[\".\"]", "fail", null, token));
      assertEquals(0, JobStore.history(c).length());
      assertNull(PendingBackups.get(c, requested.id));
      assertFalse(Operations.running());
    }
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
