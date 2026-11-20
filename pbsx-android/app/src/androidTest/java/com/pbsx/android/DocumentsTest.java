package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class DocumentsTest {
  @Test
  public void backupExclusionsPreventListingOpeningAndTraversingExcludedSafPaths()
      throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String unique = "excluded-" + System.nanoTime();
    File base = new File(c.getCacheDir(), "document-fixture/" + unique);
    assertTrue(new File(base, ".thumbnails/nested").mkdirs());
    assertTrue(new File(base, "Camera").mkdirs());
    Files.write(new File(base, ".thumbnails/nested/private.jpg").toPath(), new byte[] {1});
    Files.write(new File(base, "Camera/keep.jpg").toPath(), new byte[] {2});
    Files.write(new File(base, "Camera/skip.tmp").toPath(), new byte[] {3});
    Uri src = DocumentsContract.buildTreeDocumentUri(FixtureProvider.AUTHORITY, "root:" + unique);
    BackupJob job = new BackupJob();
    job.sources.put(new JSONObject().put("label", "DCIM").put("uri", src.toString()));
    job.excludes = BackupExclusions.parse("DCIM/.thumbnails\n**/*.tmp");
    try (AndroidDocuments docs =
        new AndroidDocuments(c, job, new Operations.Cancellation(), text -> {})) {
      assertEquals(
          "Camera", new JSONArray(docs.listJSON("DCIM")).getJSONObject(0).getString("name"));
      assertEquals(1, new JSONArray(docs.listJSON("DCIM")).length());
      assertEquals(
          "keep.jpg",
          new JSONArray(docs.listJSON("DCIM/Camera")).getJSONObject(0).getString("name"));
      assertEquals(1, new JSONArray(docs.listJSON("DCIM/Camera")).length());
      for (String path :
          new String[] {
            "DCIM/.thumbnails", "DCIM/.thumbnails/nested/private.jpg", "DCIM/Camera/skip.tmp"
          }) {
        try {
          docs.statJSON(path);
          fail("Stat of excluded path succeeded");
        } catch (SecurityException expected) {
        }
        try {
          docs.openRead(path);
          fail("Read of excluded path succeeded");
        } catch (SecurityException expected) {
        }
      }
      try {
        docs.listJSON("DCIM/.thumbnails");
        fail("Traversed excluded directory");
      } catch (SecurityException expected) {
      }
      try (InputStream input =
          new ParcelFileDescriptor.AutoCloseInputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openRead("DCIM/Camera/keep.jpg")))) {
        assertArrayEquals(new byte[] {2}, input.readAllBytes());
      }
    } finally {
      ExportCache.remove(base);
    }
  }

  @Test
  public void stagedSafRestorePreservesOriginalUntilCommitAndRefusesNewConflicts()
      throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String unique = "staged-" + System.nanoTime();
    File base = new File(c.getCacheDir(), "document-fixture/" + unique);
    assertTrue(base.mkdirs());
    File target = new File(base, "target.txt");
    Files.write(target.toPath(), "original".getBytes(StandardCharsets.UTF_8));
    Uri uri = DocumentsContract.buildTreeDocumentUri(FixtureProvider.AUTHORITY, "root:" + unique);
    try (AndroidDocuments docs =
        new AndroidDocuments(new DocumentTree(c.getContentResolver(), uri))) {
      String stage = ".pbsx-stage-test";
      try (OutputStream out =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openWrite(stage)))) {
        out.write("complete restore".getBytes(StandardCharsets.UTF_8));
      }
      assertEquals(
          "original", new String(Files.readAllBytes(target.toPath()), StandardCharsets.UTF_8));
      docs.commitFile(stage, "target.txt", true);
      assertEquals(
          "complete restore",
          new String(Files.readAllBytes(target.toPath()), StandardCharsets.UTF_8));
      assertArrayEquals(new String[] {"target.txt"}, base.list());
      try (OutputStream out =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openWrite(stage)))) {
        out.write("incomplete".getBytes(StandardCharsets.UTF_8));
      }
      try {
        docs.commitFile(stage, "target.txt", false);
        fail("New-file commit overwrote an existing file");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("appeared"));
      }
      docs.deleteFile(stage);
      docs.deleteFile(stage); // Cleanup after a successful rename is idempotent too.
      assertEquals(
          "complete restore",
          new String(Files.readAllBytes(target.toPath()), StandardCharsets.UTF_8));
      assertArrayEquals(new String[] {"target.txt"}, base.list());
    }
  }

  @Test
  public void largeSafRestoreUpdatesCacheAndProtectsFolders() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String unique = "cached-" + System.nanoTime();
    File base = new File(c.getCacheDir(), "document-fixture/" + unique);
    assertTrue(base.mkdirs());
    assertTrue(new File(base, "folder").mkdir());
    Uri uri = DocumentsContract.buildTreeDocumentUri(FixtureProvider.AUTHORITY, "root:" + unique);
    try (AndroidDocuments docs =
        new AndroidDocuments(new DocumentTree(c.getContentResolver(), uri))) {
      int queries = FixtureProvider.childQueries();
      for (int i = 0; i < 100; i++) {
        String pending = ".pbsx-stage-" + i;
        try (OutputStream out =
            new ParcelFileDescriptor.AutoCloseOutputStream(
                ParcelFileDescriptor.adoptFd((int) docs.openWrite(pending)))) {
          out.write(i);
        }
        docs.commitFile(pending, "file-" + i, false);
      }
      assertTrue(
          "Restore repeatedly listed a growing directory",
          FixtureProvider.childQueries() - queries <= 2);
      String stage = ".pbsx-stage-folder-test";
      try (OutputStream out =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openWrite(stage)))) {
        out.write(1);
      }
      try {
        docs.commitFile(stage, "folder", true);
        fail("Restore replaced a directory");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("safe restores"));
      }
      docs.deleteFile(stage);
      assertTrue(new File(base, "folder").isDirectory());
    }
  }

  @Test
  public void streamsSafDocumentsAndCreatesSafeRestoreTree() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String unique = "run-" + System.nanoTime();
    File base = new File(c.getCacheDir(), "document-fixture"),
        source = new File(base, unique + "/source"),
        destination = new File(base, unique + "/destination");
    assertTrue(source.mkdirs());
    assertTrue(destination.mkdirs());
    File nested = new File(source, "nested");
    assertTrue(nested.mkdir());
    byte[] content = "Samsung photo 猫 fixture".getBytes(StandardCharsets.UTF_8);
    Files.write(new File(nested, "猫 photo.jpg").toPath(), content);
    Uri
        src =
            DocumentsContract.buildTreeDocumentUri(
                FixtureProvider.AUTHORITY, "root:" + unique + "/source"),
        dst =
            DocumentsContract.buildTreeDocumentUri(
                FixtureProvider.AUTHORITY, "root:" + unique + "/destination");
    JSONArray sources =
        new JSONArray().put(new JSONObject().put("label", "Photos").put("uri", src.toString()));
    try (AndroidDocuments docs = new AndroidDocuments(c, sources, false, false, false, false)) {
      assertEquals("Photos", new JSONArray(docs.listJSON(".")).getJSONObject(0).getString("name"));
      assertEquals(1, new JSONArray(docs.listJSON("Photos/nested")).length());
      assertEquals(
          content.length,
          new JSONObject(docs.statJSON("Photos/nested/猫 photo.jpg")).getLong("size"));
      try (InputStream input =
          new ParcelFileDescriptor.AutoCloseInputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openRead("Photos/nested/猫 photo.jpg")))) {
        assertArrayEquals(content, input.readAllBytes());
      }
      try {
        docs.openRead("Photos/../escape");
        fail("Traversal was accepted");
      } catch (IllegalArgumentException expected) {
      }
    }
    DocumentTree target =
        new DocumentTree(c.getContentResolver(), dst).freshRestoreDirectory("restored");
    try (AndroidDocuments docs = new AndroidDocuments(target)) {
      docs.makeDirectory("nested");
      try (OutputStream out =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) docs.openWrite("nested/猫 photo.jpg")))) {
        out.write(content);
      }
      try {
        docs.openWrite("nested/猫 photo.jpg");
        fail("Existing file was overwritten");
      } catch (Exception expected) {
        assertTrue(expected.getMessage().contains("overwrite"));
      }
    }
    assertArrayEquals(
        content, Files.readAllBytes(new File(destination, "restored/nested/猫 photo.jpg").toPath()));
  }
}
