package com.pbsx.android;

import static org.junit.Assert.*;

import androidx.test.ext.junit.runners.AndroidJUnit4;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class SharedFolderJobTest {
  @Test
  public void appFiltersAreIndependentAndLegacyChoicesArePreserved() throws Exception {
    BackupJob job = new BackupJob();
    job.shizuku = true;
    job.apks = true;
    job.data = true;
    job.packages.put("com.example.legacy");
    assertEquals("com.example.legacy", job.appPackages("data").getString(0));
    job.appSelection("apks", false, new JSONArray().put("com.example.installer"));
    job.appSelection("data", false, new JSONArray().put("com.example.files"));
    job.validate(true);
    BackupJob saved = BackupJob.from(job.json());
    assertEquals("com.example.installer", saved.appPackages("apks").getString(0));
    assertEquals("com.example.files", saved.appPackages("data").getString(0));
    assertEquals("com.example.legacy", saved.appPackages("obb").getString(0));
    saved.appSelection("apks", true, new JSONArray());
    assertTrue(saved.appAll("apks"));
    assertFalse(saved.appAll("data"));
    assertEquals("com.example.files", saved.appPackages("data").getString(0));
    saved.appSelections.put("unknown", new JSONObject());
    try {
      saved.validate(false);
      fail("unknown app source accepted");
    } catch (Exception expected) {
    }
  }

  @Test
  public void folderOnlyJobsRoundTripWithoutSelectingAppsOrExports() throws Exception {
    BackupJob job = new BackupJob();
    assertEquals(0, job.shizukuFolders.length());
    job.shizuku = true;
    job.shizukuFolders.put(new JSONObject().put("label", "Downloads").put("path", "Download"));
    job.validate(true);
    BackupJob copy = BackupJob.from(job.json());
    assertEquals(job.shizukuFolders.toString(), copy.shizukuFolders.toString());
    assertEquals(1, copy.sourceCount());
    assertFalse(copy.allApps);
    assertEquals(0, copy.packages.length());
    assertEquals(0, copy.exports.length());
    copy.shizukuFolders.remove(0);
    assertEquals(1, job.shizukuFolders.length());
    job.shizuku = false;
    assertEquals(0, job.sourceCount());
    try {
      job.validate(true);
      fail("Disabled Shizuku counted as a backup source");
    } catch (Exception expected) {
    }
    JSONObject old = job.json();
    old.remove("shizuku_folders");
    assertEquals(0, BackupJob.from(old).shizukuFolders.length());
  }

  @Test
  public void rejectsMalformedPathsDuplicateSelectionsAndCrossProviderLabelCollisions()
      throws Exception {
    BackupJob job = new BackupJob();
    job.shizuku = true;
    for (Object value : new Object[] {"Download", JSONObject.NULL, new JSONObject()}) {
      try {
        BackupJob.from(job.json().put("shizuku_folders", value));
        fail("Malformed folder list accepted");
      } catch (Exception expected) {
      }
    }
    for (String path : new String[] {".", "Download/../DCIM", "/data/user/0", "Android/data"}) {
      job.shizukuFolders =
          new JSONArray().put(new JSONObject().put("label", "Downloads").put("path", path));
      try {
        BackupJob.from(job.json());
        fail("Unsafe path accepted: " + path);
      } catch (Exception expected) {
      }
    }
    JSONObject source = new JSONObject().put("label", "Downloads").put("path", "Download");
    job.shizukuFolders = new JSONArray().put(source);
    job.sources.put(
        new JSONObject().put("label", "Downloads").put("uri", "content://fixture/tree/other"));
    try {
      job.validate(true);
      fail("Cross-provider duplicate label accepted");
    } catch (Exception expected) {
    }
    job.sources = new JSONArray();
    job.shizukuFolders.put(new JSONObject().put("label", "Other").put("path", "Download"));
    try {
      job.validate(true);
      fail("Duplicate Shizuku path accepted");
    } catch (Exception expected) {
    }
    job.shizukuFolders =
        new JSONArray()
            .put(new JSONObject().put("label", "Installed-APKs").put("path", "Download"));
    try {
      job.validate(true);
      fail("Reserved label accepted");
    } catch (Exception expected) {
    }
  }
}
