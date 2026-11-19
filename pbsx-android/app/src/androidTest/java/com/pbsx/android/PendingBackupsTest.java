package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import android.os.ParcelFileDescriptor;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class PendingBackupsTest {
  private Context isolated() {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String prefix = "resume-fixture-" + UUID.randomUUID();
    File root = new File(base.getFilesDir(), prefix);
    assertTrue(root.mkdirs());
    return new ContextWrapper(base) {
      @Override
      public File getFilesDir() {
        return root;
      }

      @Override
      public SharedPreferences getSharedPreferences(String name, int mode) {
        return base.getSharedPreferences(prefix + name, mode);
      }
    };
  }

  private JSONObject run(Context c) throws Exception {
    BackupJob job = new BackupJob();
    job.sources.put(
        new JSONObject().put("label", "Download").put("uri", "content://fixture/tree/download"));
    JobStore.save(c, job);
    String history = JobStore.start(c, job, "backup"), hex = repeat("ab", 32);
    JSONObject descriptor =
        new JSONObject()
            .put("version", 1)
            .put("token", hex)
            .put("state_key", repeat("cd", 32))
            .put("binding", hex)
            .put("source", hex)
            .put("store", job.store)
            .put("namespace", job.namespace)
            .put("backup_id", job.backupID)
            .put("job_id", job.id)
            .put("job_name", job.name)
            .put("stamp", 12345)
            .put("prepared", 2)
            .put("confirmed", 2)
            .put("files", 1)
            .put("bytes", 65536)
            .put("uploaded", 20000);
    return new JSONObject()
        .put("job", job.json())
        .put("history", history)
        .put("device", hex)
        .put("source", hex)
        .put("descriptor", descriptor.toString())
        .put("state", "paused")
        .put("automatic", false);
  }

  private String repeat(String value, int count) {
    return new String(new char[count]).replace("\0", value);
  }

  @Test
  public void encryptedRecordSurvivesReloadAndDetectsDamage() throws Exception {
    Context c = isolated();
    JSONObject run = run(c);
    String job = run.getJSONObject("job").getString("id");
    PendingBackups.put(c, run);
    File file = new File(c.getFilesDir(), "pending-backups-v1");
    byte[] sealed = Files.readAllBytes(file.toPath());
    assertFalse(new String(sealed, StandardCharsets.ISO_8859_1).contains("state_key"));
    assertFalse(new String(sealed, StandardCharsets.ISO_8859_1).contains(repeat("cd", 32)));
    PendingBackups.forgetCache();
    assertEquals(run.toString(), PendingBackups.get(c, job).toString());
    sealed[sealed.length - 1] ^= 1;
    Files.write(file.toPath(), sealed);
    PendingBackups.forgetCache();
    try {
      PendingBackups.all(c);
      fail("Corruption was treated as no pending backup");
    } catch (Exception expected) {
    }
    ExportCache.remove(c.getFilesDir());
    PendingBackups.forgetCache();
  }

  @Test
  public void historyAndJobDeletionPreservePendingRun() throws Exception {
    Context c = isolated();
    JSONObject run = run(c);
    PendingBackups.put(c, run);
    String jobID = run.getJSONObject("job").getString("id");
    JobStore.recover(c);
    assertEquals("paused", JobStore.history(c).getJSONObject(0).getString("state"));
    for (int i = 0; i < 105; i++) {
      BackupJob other = new BackupJob();
      String id = JobStore.start(c, other, "restore");
      JobStore.finish(c, id, true, "Fixture complete");
    }
    JSONArray history = JobStore.history(c);
    assertEquals(100, history.length());
    boolean found = false;
    for (int i = 0; i < history.length(); i++)
      if (run.getString("history").equals(history.getJSONObject(i).getString("id"))) found = true;
    assertTrue(found);
    try {
      JobStore.delete(c, jobID);
      fail("Deleted a job with a pending backup");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("pending"));
    }
    PendingBackups.remove(c, jobID);
    JobStore.delete(c, jobID);
    assertTrue(JobStore.all(c).isEmpty());
    ExportCache.remove(c.getFilesDir());
    PendingBackups.forgetCache();
  }

  @Test
  public void manualPauseRemovesScheduleAndAutomaticPauseKeepsOriginalDue() throws Exception {
    Context c = isolated();
    JSONObject run = run(c);
    BackupJob job = BackupJob.from(run.getJSONObject("job"));
    job.hours = 12;
    JobStore.save(c, job);
    android.app.job.JobScheduler scheduler = c.getSystemService(android.app.job.JobScheduler.class);
    try {
      ScheduleService.configureAll(c);
      long due = ScheduleState.due(c, job, false);
      assertNotNull(scheduler.getPendingJob(job.schedulerId));
      PendingBackups.put(c, run);
      ScheduleService.configureAll(c);
      assertNull(scheduler.getPendingJob(job.schedulerId));
      assertEquals(due, ScheduleState.due(c, job, false));
      run.put("automatic", true);
      PendingBackups.put(c, run);
      ScheduleService.configureAll(c);
      assertNotNull(scheduler.getPendingJob(job.schedulerId));
      assertEquals(due, scheduler.getPendingJob(job.schedulerId).getExtras().getLong("due"));
    } finally {
      scheduler.cancel(job.schedulerId);
      ExportCache.remove(c.getFilesDir());
      PendingBackups.forgetCache();
    }
  }

  @Test
  public void settingsRoundTripIncludesRecoverySecretsAndHoldsImportedRuns() throws Exception {
    Context from = isolated(), to = isolated();
    String connection =
        KeyProfiles.addConnection(
            from,
            "Fixture",
            new JSONObject()
                .put("server", "https://fixture.invalid:8007")
                .put("host_id", "test-phone")
                .put("fingerprint", repeat("ab", 32))
                .put("auth_id", "fixture@pbs!resume")
                .put("secret", "synthetic-secret")
                .toString());
    JSONObject run = run(from);
    BackupJob job = BackupJob.from(run.getJSONObject("job"));
    job.connectionID = connection;
    JobStore.save(from, job);
    run.put("job", job.json()).put("automatic", true);
    PendingBackups.put(from, run);
    String exported = SettingsTransfer.exportJSON(from);
    assertEquals(
        run.getString("descriptor"),
        new JSONObject(exported)
            .getJSONArray("pending_backups")
            .getJSONObject(0)
            .getString("descriptor"));
    SettingsTransfer.importJSON(to, exported);
    JSONObject imported = PendingBackups.get(to, job.id);
    assertNotNull(imported);
    assertEquals(run.getString("descriptor"), imported.getString("descriptor"));
    assertFalse(imported.getBoolean("automatic"));
    assertEquals("paused", imported.getString("state"));
    assertTrue(Settings.prefs(to).getBoolean("imported_schedule_hold", false));
    try {
      SettingsTransfer.importJSON(to, exported);
      fail("Replaced an existing pending backup");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("pending"));
    }
    JSONObject config = KeyProfiles.connection(to, connection).getJSONObject("config");
    try {
      KeyProfiles.updateConnection(to, connection, config);
      fail("Updated a pending run's credentials");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("pending"));
    }
    PendingBackups.remove(from, job.id);
    PendingBackups.remove(to, job.id);
    ExportCache.remove(from.getFilesDir());
    ExportCache.remove(to.getFilesDir());
    PendingBackups.forgetCache();
  }

  private static class Exports extends PrivilegedFiles {
    int opened;

    Exports(Context c) {
      super(c);
    }

    @Override
    public ParcelFileDescriptor openExport(String category) {
      opened++;
      try {
        ParcelFileDescriptor[] pipe = ParcelFileDescriptor.createReliablePipe();
        new Thread(
                () -> {
                  try (java.io.OutputStream output =
                          new ParcelFileDescriptor.AutoCloseOutputStream(pipe[1]);
                      java.util.zip.ZipOutputStream zip =
                          new java.util.zip.ZipOutputStream(output)) {
                    zip.putNextEntry(new java.util.zip.ZipEntry("manifest.json"));
                    zip.write("{}".getBytes(StandardCharsets.UTF_8));
                    zip.closeEntry();
                  } catch (Exception e) {
                    throw new RuntimeException(e);
                  }
                })
            .start();
        return pipe[0];
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }
  }

  @Test
  public void persistentExportsReuseExactBytesAndRejectMissingOrChangedCache() throws Exception {
    Context c = isolated();
    Exports service = new Exports(c);
    String id = UUID.randomUUID().toString();
    JSONArray categories = new JSONArray().put("contacts");
    File exported;
    byte[] original;
    try (ExportCache cache = new ExportCache(c, id, false)) {
      cache.prepare(service, categories, new Operations.Cancellation(), text -> {});
      exported = cache.files.get("contacts.zip");
      original = Files.readAllBytes(exported.toPath());
    }
    assertTrue(exported.exists());
    assertEquals(1, service.opened);
    try (ExportCache cache = new ExportCache(c, id, true)) {
      cache.prepare(service, categories, new Operations.Cancellation(), text -> {});
      assertArrayEquals(original, Files.readAllBytes(cache.files.get("contacts.zip").toPath()));
    }
    assertEquals(1, service.opened);
    Files.write(exported.toPath(), new byte[] {1, 2, 3});
    try (ExportCache cache = new ExportCache(c, id, true)) {
      try {
        cache.prepare(service, categories, new Operations.Cancellation(), text -> {});
        fail("Regenerated a changed export");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("changed"));
      }
    }
    assertTrue(exported.delete());
    try (ExportCache cache = new ExportCache(c, id, true)) {
      try {
        cache.prepare(service, categories, new Operations.Cancellation(), text -> {});
        fail("Regenerated a missing export");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("missing"));
      }
    }
    assertEquals(1, service.opened);
    ExportCache.discard(c, id);
    ExportCache.remove(c.getFilesDir());
  }
}
