package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.job.JobInfo;
import android.app.job.JobScheduler;
import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import android.net.NetworkCapabilities;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.util.List;
import java.util.UUID;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class JobStoreTest {
  private Context isolated() {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String prefix = "fixture-" + UUID.randomUUID() + "-";
    return new ContextWrapper(base) {
      @Override
      public SharedPreferences getSharedPreferences(String name, int mode) {
        return base.getSharedPreferences(prefix + name, mode);
      }
    };
  }

  @Test
  public void backupIdentityIsIndependentOfJobLabelAndDeviceIdentity() throws Exception {
    Context c = isolated();
    Vault.save(c, new JSONObject().put("host_id", "test-phone").toString());
    BackupJob photos = BackupJob.create(c), manual = BackupJob.create(c);
    assertEquals("devices/test-phone", photos.namespace);
    assertNotEquals(photos.backupID, manual.backupID);
    String identity = photos.backupID;
    photos.name = "Photos";
    photos.hours = 24;
    JobStore.save(c, photos);
    photos.name = "Camera roll";
    JobStore.save(c, photos);
    assertEquals(identity, JobStore.get(c, photos.id).backupID);
    manual.backupID = identity;
    manual.namespace = photos.namespace;
    JobStore.save(c, manual);
    assertEquals(JobStore.get(c, photos.id).target(), JobStore.get(c, manual.id).target());
    assertNotEquals(JobStore.get(c, photos.id).schedulerId, JobStore.get(c, manual.id).schedulerId);
    JSONObject legacy = photos.json();
    legacy.remove("backup_id");
    assertEquals("test-phone", BackupJob.from(legacy).backupID(c));
    assertEquals("", BackupJob.from(legacy).backupID);
    for (String invalid : new String[] {"../other", "a/b", "has space", ".", "name\nother"}) {
      BackupJob bad = BackupJob.create(c);
      bad.backupID = invalid;
      try {
        bad.validate(false);
        fail("Invalid ID accepted: " + invalid);
      } catch (Exception expected) {
      }
    }
    String longID = "abcdefghijklmnopqrstuvwxyz0123456789-long-device";
    Vault.save(c, new JSONObject().put("host_id", longID).toString());
    assertEquals(
        "devices/" + longID.substring(0, 32) + "/" + longID.substring(32),
        BackupJob.create(c).namespace);
  }

  @Test
  public void migratesExistingFoldersAndScheduleExactlyOnce() throws Exception {
    Context c = isolated();
    JSONArray sources =
        new JSONArray()
            .put(
                new JSONObject()
                    .put("label", "DCIM")
                    .put("uri", "content://com.example.documents/tree/DCIM"));
    Settings.prefs(c)
        .edit()
        .putString("sources", sources.toString())
        .putString("store", "family")
        .putString("namespace", "phones")
        .putInt("hours", 24)
        .putBoolean("charging", false)
        .putBoolean("wifi", true)
        .putBoolean("shizuku", true)
        .putBoolean("apks", true)
        .commit();
    BackupJob job = JobStore.all(c).get(0);
    String id = job.id;
    assertEquals("family", job.store);
    assertEquals("phones", job.namespace);
    assertEquals(24, job.hours);
    assertFalse(job.charging);
    assertTrue(job.wifi);
    assertTrue(job.apks);
    assertEquals("DCIM", job.sources.getJSONObject(0).getString("label"));
    assertEquals(id, JobStore.all(c).get(0).id);
    assertEquals(1, JobStore.all(c).size());
    assertEquals(sources.toString(), Settings.prefs(c).getString("sources", ""));
  }

  @Test
  public void jobsKeepIndependentSourcesTargetsAndSchedules() throws Exception {
    Context c = isolated();
    assertEquals(0, JobStore.all(c).size());
    BackupJob photos = new BackupJob();
    photos.name = "Photos";
    photos.hours = 12;
    photos.store = "backup";
    photos.sources.put(
        new JSONObject().put("label", "DCIM").put("uri", "content://documents/tree/DCIM"));
    JobStore.save(c, photos);
    BackupJob docs = new BackupJob();
    docs.name = "Documents";
    docs.hours = 48;
    docs.charging = false;
    docs.wifi = false;
    docs.namespace = "work";
    docs.sources.put(
        new JSONObject().put("label", "Docs").put("uri", "content://documents/tree/Docs"));
    JobStore.save(c, docs);
    assertNotEquals(photos.id, docs.id);
    assertNotEquals(photos.schedulerId, docs.schedulerId);
    BackupJob loaded = JobStore.get(c, photos.id);
    loaded.sources.remove(0);
    assertEquals(1, JobStore.get(c, photos.id).sources.length());
    loaded.name = "Camera";
    JobStore.save(c, loaded);
    assertEquals(12, JobStore.get(c, photos.id).hours);
    assertEquals("work", JobStore.get(c, docs.id).namespace);
    assertEquals(48, JobStore.get(c, docs.id).hours);
    assertFalse(JobStore.get(c, docs.id).charging);
    assertFalse(JobStore.get(c, docs.id).wifi);
    JobStore.delete(c, photos.id);
    List<BackupJob> remaining = JobStore.all(c);
    assertEquals(1, remaining.size());
    assertEquals(docs.id, remaining.get(0).id);
  }

  @Test
  public void recordsCompletionAndRecoversInterruptedRuns() throws Exception {
    Context c = isolated();
    BackupJob job = new BackupJob();
    JobStore.save(c, job);
    String id = JobStore.start(c, job, "backup");
    JobStore.finish(c, id, true, "Snapshot saved");
    JSONObject first = JobStore.history(c).getJSONObject(0);
    assertEquals("success", first.getString("state"));
    assertEquals("Snapshot saved", first.getString("message"));
    JobStore.start(c, job, "restore");
    JobStore.recover(c);
    assertEquals("interrupted", JobStore.history(c).getJSONObject(0).getString("state"));
    assertEquals("success", JobStore.history(c).getJSONObject(1).getString("state"));
  }

  @Test
  public void schedulesUpdateAndCancelIndependently() throws Exception {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext(), c = isolated();
    JobScheduler scheduler = c.getSystemService(JobScheduler.class);
    BackupJob photos = new BackupJob(), docs = new BackupJob(), manual = new BackupJob();
    photos.hours = 12;
    photos.charging = true;
    docs.hours = 48;
    docs.charging = false;
    docs.wifi = false;
    JobStore.save(c, photos);
    JobStore.save(c, docs);
    JobStore.save(c, manual);
    try {
      ScheduleService.configureAll(c);
      JobInfo first = scheduler.getPendingJob(photos.schedulerId);
      assertNotNull(first);
      assertTrue(first.isPersisted());
      assertTrue(first.isRequireCharging());
      assertEquals(12 * 3600000L, first.getIntervalMillis());
      assertEquals(photos.id, first.getExtras().getString("job_id"));
      assertTrue(first.getRequiredNetwork().hasTransport(NetworkCapabilities.TRANSPORT_WIFI));
      assertFalse(scheduler.getPendingJob(docs.schedulerId).isRequireCharging());
      assertEquals(48 * 3600000L, scheduler.getPendingJob(docs.schedulerId).getIntervalMillis());
      assertNull(scheduler.getPendingJob(manual.schedulerId));
      String hash = first.getExtras().getString("config_hash");
      ScheduleService.configureAll(c);
      assertEquals(
          hash, scheduler.getPendingJob(photos.schedulerId).getExtras().getString("config_hash"));
      photos.hours = 24;
      JobStore.save(c, photos);
      ScheduleService.configureAll(c);
      assertEquals(24 * 3600000L, scheduler.getPendingJob(photos.schedulerId).getIntervalMillis());
      assertNotEquals(
          hash, scheduler.getPendingJob(photos.schedulerId).getExtras().getString("config_hash"));
      photos.hours = 0;
      JobStore.save(c, photos);
      JobStore.delete(c, docs.id);
      ScheduleService.configureAll(c);
      assertNull(scheduler.getPendingJob(photos.schedulerId));
      assertNull(scheduler.getPendingJob(docs.schedulerId));
    } finally {
      ScheduleService.configureAll(base);
    }
  }
}
