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
