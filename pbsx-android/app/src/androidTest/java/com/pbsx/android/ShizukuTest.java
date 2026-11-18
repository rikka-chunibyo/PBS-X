package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.pm.PackageManager;
import android.os.ParcelFileDescriptor;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.InputStream;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;
import rikka.shizuku.Shizuku;

@RunWith(AndroidJUnit4.class)
public class ShizukuTest {
  @Test
  public void selectedDownloadFolderReadsFilesWithoutAnAppSelection() throws Exception {
    Assume.assumeTrue(android.os.Build.VERSION.SDK_INT >= 29);
    Assume.assumeTrue(Shizuku.pingBinder());
    Assume.assumeTrue(Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED);
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String relative = "Download/pbsx-fixture-" + java.util.UUID.randomUUID();
    android.content.ContentValues values = new android.content.ContentValues();
    values.put(android.provider.MediaStore.MediaColumns.DISPLAY_NAME, "download-test.txt");
    values.put(android.provider.MediaStore.MediaColumns.MIME_TYPE, "text/plain");
    values.put(android.provider.MediaStore.MediaColumns.RELATIVE_PATH, relative);
    android.net.Uri uri =
        c.getContentResolver()
            .insert(android.provider.MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
    assertNotNull(uri);
    byte[] content =
        "Download folder backup fixture".getBytes(java.nio.charset.StandardCharsets.UTF_8);
    try {
      try (java.io.OutputStream out = c.getContentResolver().openOutputStream(uri)) {
        assertNotNull(out);
        out.write(content);
      }
      BackupJob job = new BackupJob();
      job.shizuku = true;
      job.shizukuFolders.put(new JSONObject().put("label", "My-downloads").put("path", relative));
      assertFalse(job.allApps);
      try (AndroidDocuments docs =
          new AndroidDocuments(c, job, new Operations.Cancellation(), text -> {})) {
        JSONArray roots = new JSONArray(docs.listJSON("."));
        assertEquals(1, roots.length());
        assertEquals("My-downloads", roots.getJSONObject(0).getString("name"));
        JSONArray files = new JSONArray(docs.listJSON("My-downloads"));
        assertEquals(1, files.length());
        assertEquals("download-test.txt", files.getJSONObject(0).getString("name"));
        assertEquals(
            content.length,
            new JSONObject(docs.statJSON("My-downloads/download-test.txt")).getLong("size"));
        try (InputStream in =
            new ParcelFileDescriptor.AutoCloseInputStream(
                ParcelFileDescriptor.adoptFd(
                    (int) docs.openRead("My-downloads/download-test.txt")))) {
          java.io.ByteArrayOutputStream bytes = new java.io.ByteArrayOutputStream();
          byte[] buffer = new byte[256];
          int count;
          while ((count = in.read(buffer)) != -1) bytes.write(buffer, 0, count);
          assertArrayEquals(content, bytes.toByteArray());
        }
      }
      try (ShizukuBridge bridge = new ShizukuBridge(c)) {
        JSONArray directories = new JSONArray(bridge.files().folders("."));
        boolean downloads = false;
        for (int i = 0; i < directories.length(); i++) {
          String name = directories.getJSONObject(i).getString("name");
          assertNotEquals("Android", name);
          if (name.equals("Download")) downloads = true;
        }
        assertTrue(downloads);
        assertEquals(0, new JSONArray(bridge.files().folders(relative)).length());
        for (String path :
            new String[] {"/@shared/../data", "/@shared/Android/data", "/@shared/."}) {
          try {
            bridge.files().stat(path);
            fail("Unsafe shared-storage path accepted");
          } catch (IllegalArgumentException | SecurityException expected) {
          }
        }
      }
      job.shizukuFolders =
          new JSONArray()
              .put(new JSONObject().put("label", "Missing").put("path", relative + "/gone"));
      try (AndroidDocuments ignored =
          new AndroidDocuments(c, job, new Operations.Cancellation(), text -> {})) {
        fail("Missing selected folder was silently omitted");
      } catch (IllegalArgumentException expected) {
      }
    } finally {
      c.getContentResolver().delete(uri, null, null);
    }
  }

  @Test
  public void selectedAppsRestrictBothListingAndDirectReads() throws Exception {
    Assume.assumeTrue(Shizuku.pingBinder());
    Assume.assumeTrue(Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED);
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    BackupJob j = new BackupJob();
    j.shizuku = true;
    j.apks = true;
    j.allApps = false;
    j.packages.put("com.pbsx.android");
    try (AndroidDocuments docs =
        new AndroidDocuments(c, j, new Operations.Cancellation(), text -> {})) {
      JSONArray apps = new JSONArray(docs.listJSON("Installed-APKs"));
      assertEquals(1, apps.length());
      assertEquals("com.pbsx.android", apps.getJSONObject(0).getString("name"));
      try {
        docs.statJSON("Installed-APKs/moe.shizuku.privileged.api");
        fail("Unselected app was readable");
      } catch (SecurityException expected) {
        assertTrue(expected.getMessage().contains("not selected"));
      }
      assertTrue(new JSONArray(docs.listJSON("Installed-APKs/com.pbsx.android")).length() > 0);
    }
  }

  @Test
  public void readsInstalledApksAndRejectsPrivatePaths() throws Exception {
    Assume.assumeTrue(
        "Shizuku is an optional, separately started test dependency", Shizuku.pingBinder());
    Assume.assumeTrue(Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED);
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    try (AndroidDocuments docs =
        new AndroidDocuments(c, new JSONArray(), true, true, false, false)) {
      assertEquals(
          "Installed-APKs", new JSONArray(docs.listJSON(".")).getJSONObject(0).getString("name"));
      assertTrue(new JSONArray(docs.listJSON("Installed-APKs")).length() > 0);
      JSONArray apks = new JSONArray(docs.listJSON("Installed-APKs/com.pbsx.android"));
      assertTrue(apks.length() > 0);
      String path = "Installed-APKs/com.pbsx.android/" + apks.getJSONObject(0).getString("name");
      assertTrue(new JSONObject(docs.statJSON(path)).getLong("size") > 0);
      try (InputStream in =
          new ParcelFileDescriptor.AutoCloseInputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openRead(path)))) {
        assertEquals('P', in.read());
        assertEquals('K', in.read());
      }
    }
    try (ShizukuBridge bridge = new ShizukuBridge(c)) {
      try {
        bridge.files().stat("/data/user/0/com.pbsx.android");
        fail("Private path escaped the read-only allowlist");
      } catch (IllegalArgumentException | SecurityException expected) {
        assertTrue(expected.getMessage().contains("outside"));
      }
    }
  }
}
