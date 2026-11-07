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
