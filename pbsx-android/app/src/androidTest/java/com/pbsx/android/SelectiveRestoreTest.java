package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.net.Uri;
import android.provider.DocumentsContract;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Client;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class SelectiveRestoreTest {
  private Client client(Context c) throws Exception {
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue("Requires the loopback archive fixture", config != null && !config.isEmpty());
    return Operations.client(c, config, new Operations.Cancellation());
  }

  private void write(File folder, String name, String content) throws Exception {
    File f = new File(folder, name);
    f.getParentFile().mkdirs();
    Files.write(f.toPath(), content.getBytes(StandardCharsets.UTF_8));
  }

  private String read(File folder, String name) throws Exception {
    return new String(Files.readAllBytes(new File(folder, name).toPath()), StandardCharsets.UTF_8);
  }

  @Test
  public void browsesArchiveAndRestoresOnlySelectedFilesThroughSaf() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    Client client = client(c);
    java.util.concurrent.atomic.AtomicReference<String> details =
        new java.util.concurrent.atomic.AtomicReference<>();
    client.setProgressDetails(details::set);
    JSONObject root =
        new JSONObject(client.listArchive("backup", "", "android-test", 1700000000, ".", 0, 200));
    assertEquals(3, root.getInt("total"));
    JSONObject folder =
        new JSONObject(
            client.listArchive("backup", "", "android-test", 1700000000, "Photos", 0, 2));
    assertTrue(folder.getInt("total") > 2);
    assertEquals(2, folder.getJSONArray("entries").length());
    String unique = "selected-" + System.nanoTime();
    File base = new File(c.getCacheDir(), "document-fixture/" + unique);
    assertTrue(base.mkdirs());
    Uri uri = DocumentsContract.buildTreeDocumentUri(FixtureProvider.AUTHORITY, "root:" + unique);
    try (AndroidDocuments docs =
        new AndroidDocuments(new DocumentTree(c.getContentResolver(), uri))) {
      JSONObject stats =
          new JSONObject(
              client.restoreSelected(
                  "backup",
                  "",
                  "android-test",
                  1700000000,
                  "[\"Photos/nested/猫.jpg\"]",
                  "fail",
                  docs,
                  null));
      assertEquals(1, stats.getLong("files"));
    }
    assertEquals("image fixture", read(base, "Photos/nested/猫.jpg"));
    assertNotNull(details.get());
    JSONObject progress = new JSONObject(details.get());
    assertEquals("Restoring", progress.getString("stage"));
    assertEquals("Photos/nested/猫.jpg", progress.getString("path"));
    assertEquals(1, progress.getLong("files"));
    assertEquals(progress.getLong("file_total"), progress.getLong("file_bytes"));
    assertFalse(new File(base, "Photos/a.txt").exists());
    assertFalse(new File(base, "Documents").exists());
  }

  @Test
  public void nativeRestoreModesPreserveUnrelatedAndIdenticalFiles() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    Client client = client(c);
    for (String mode : new String[] {"skip", "replace", "rename"}) {
      String unique = "mode-" + mode + "-" + System.nanoTime();
      File base = new File(c.getCacheDir(), "document-fixture/" + unique);
      assertTrue(base.mkdirs());
      write(base, "Photos/a.txt", "old-data");
      write(base, "Photos/same.txt", "same");
      write(base, "Photos/local.txt", "local");
      write(base, "Photos/a (restored 1).txt", "older");
      File same = new File(base, "Photos/same.txt");
      assertTrue(same.setLastModified(1234567890000L));
      long original = same.lastModified();
      Uri uri = DocumentsContract.buildTreeDocumentUri(FixtureProvider.AUTHORITY, "root:" + unique);
      try (AndroidDocuments docs =
          new AndroidDocuments(new DocumentTree(c.getContentResolver(), uri))) {
        client.restoreSelected(
            "backup",
            "",
            "android-test",
            1700000000,
            "[\"Photos/a.txt\",\"Photos/same.txt\",\"Photos/missing.txt\"]",
            mode,
            docs,
            null);
      }
      assertEquals("new", read(base, "Photos/missing.txt"));
      assertEquals("local", read(base, "Photos/local.txt"));
      assertEquals("older", read(base, "Photos/a (restored 1).txt"));
      assertEquals(original, same.lastModified());
      assertFalse(new File(base, "Documents").exists());
      if (mode.equals("replace")) assertEquals("backup-A", read(base, "Photos/a.txt"));
      else assertEquals("old-data", read(base, "Photos/a.txt"));
      if (mode.equals("rename")) {
        assertEquals("backup-A", read(base, "Photos/a (restored 2).txt"));
        assertEquals("same", read(base, "Photos/same (restored 1).txt"));
      }
    }
  }
}
