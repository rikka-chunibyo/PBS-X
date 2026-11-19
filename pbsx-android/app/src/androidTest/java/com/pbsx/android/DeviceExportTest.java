package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.zip.ZipInputStream;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;
import rikka.shizuku.Shizuku;

@RunWith(AndroidJUnit4.class)
public final class DeviceExportTest {
  @Test
  public void inlineMmsTextDoesNotOpenStaleProviderFiles() throws Exception {
    DeviceExport.Source source =
        new DeviceExport.Source() {
          public Cursor query(Uri uri, String[] projection, String sort) {
            MatrixCursor c = new MatrixCursor(new String[] {"_id", "mid", "ct", "_data", "text"});
            if (uri.toString().equals("content://mms/part")) {
              c.addRow(new Object[] {1L, 5L, "application/smil", "/missing/smil", "<smil/>"});
              c.addRow(new Object[] {2L, 5L, "text/plain", "/missing/text", "MMS text 猫"});
            }
            return c;
          }

          public InputStream open(Uri uri) {
            throw new AssertionError("Inline MMS part was opened as a file: " + uri);
          }

          public void close() {}
        };
    ByteArrayOutputStream bytes = new ByteArrayOutputStream();
    DeviceExport.write("mms", source, bytes);
    Map<String, byte[]> contents = unzip(bytes.toByteArray());
    String parts = new String(contents.get("parts.jsonl"), StandardCharsets.UTF_8);
    String[] records = parts.trim().split("\n");
    assertEquals(2, records.length);
    JSONObject smil = new JSONObject(records[0]), text = new JSONObject(records[1]);
    assertEquals("<smil/>", smil.getString("text"));
    assertEquals("MMS text 猫", text.getString("text"));
    for (JSONObject row : new JSONObject[] {smil, text}) {
      assertEquals("inline-text", row.getString("pbsx_payload_state"));
      assertFalse(row.has("pbsx_attachment"));
      assertFalse(row.has("_data"));
    }
  }

  private Context context() {
    Assume.assumeTrue(android.os.Build.VERSION.SDK_INT >= 31);
    Assume.assumeTrue(Shizuku.pingBinder());
    Assume.assumeTrue(Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED);
    return InstrumentationRegistry.getInstrumentation().getTargetContext();
  }

  private BackupJob job(String... categories) {
    BackupJob j = new BackupJob();
    j.shizuku = true;
    for (String category : categories) j.exports.put(category);
    return j;
  }

  private byte[] read(AndroidDocuments docs, String path) throws Exception {
    try (InputStream input =
        new ParcelFileDescriptor.AutoCloseInputStream(
            ParcelFileDescriptor.adoptFd((int) docs.openRead(path)))) {
      ByteArrayOutputStream bytes = new ByteArrayOutputStream();
      byte[] buffer = new byte[4096];
      int count;
      while ((count = input.read(buffer)) != -1) {
        bytes.write(buffer, 0, count);
        assertTrue(bytes.size() < 1024 * 1024);
      }
      assertEquals(bytes.size(), new JSONObject(docs.statJSON(path)).getLong("size"));
      return bytes.toByteArray();
    }
  }

  private Map<String, byte[]> unzip(byte[] bytes) throws Exception {
    Map<String, byte[]> entries = new LinkedHashMap<>();
    try (ZipInputStream zip = new ZipInputStream(new ByteArrayInputStream(bytes))) {
      java.util.zip.ZipEntry entry;
      while ((entry = zip.getNextEntry()) != null) {
        assertFalse(entry.getName().contains(".."));
        ByteArrayOutputStream data = new ByteArrayOutputStream();
        byte[] buffer = new byte[4096];
        int count;
        while ((count = zip.read(buffer)) != -1) data.write(buffer, 0, count);
        assertNull(entries.put(entry.getName(), data.toByteArray()));
      }
    }
    return entries;
  }

  @Test
  public void exportsAllCategoriesThroughRealShellBinderAndPreservesAttachments() throws Exception {
    Context c = context();
    BackupJob j = job(DeviceExport.CATEGORIES.toArray(new String[0]));
    File stage;
    try (AndroidDocuments docs =
        new AndroidDocuments(
            c, j, new Operations.Cancellation(), text -> {}, ExportFixtureService.class)) {
      JSONArray roots = new JSONArray(docs.listJSON("."));
      assertEquals(1, roots.length());
      assertEquals("Device-data", roots.getJSONObject(0).getString("name"));
      assertEquals(6, new JSONArray(docs.listJSON("Device-data")).length());
      Map<String, byte[]> contacts = unzip(read(docs, "Device-data/contacts.zip"));
      JSONObject row =
          new JSONObject(new String(contacts.get("contacts.jsonl"), StandardCharsets.UTF_8).trim());
      assertEquals(2000, row.getInt("reader_uid"));
      assertEquals("Unicode 猫\ncommas, and = signs", row.getString("text"));
      assertEquals(1790000000000L, row.getLong("integer"));
      assertTrue(row.isNull("nothing"));
      assertEquals("AAH/", row.getJSONObject("blob").getString("value"));
      assertArrayEquals(new byte[] {0, 1, 127, (byte) 255, 2}, contacts.get("photos/1.bin"));
      assertTrue(
          new String(contacts.get("contacts.vcf"), StandardCharsets.UTF_8).contains("BEGIN:VCARD"));
      Map<String, byte[]> mms = unzip(read(docs, "Device-data/mms.zip"));
      assertArrayEquals(new byte[] {0, 1, 127, (byte) 255, 2}, mms.get("attachments/7.bin"));
      String[] parts =
          new String(mms.get("parts.jsonl"), StandardCharsets.UTF_8).trim().split("\n");
      assertEquals(2, parts.length);
      JSONObject part = new JSONObject(parts[0]);
      assertEquals("attachments/7.bin", part.getString("pbsx_attachment"));
      assertFalse(part.has("_data"));
      assertEquals("../../unsafe.png", part.getString("fn"));
      assertEquals("MMS text\n猫", new JSONObject(parts[1]).getString("text"));
      Map<String, byte[]> calendar = unzip(read(docs, "Device-data/calendar.zip"));
      for (String table :
          new String[] {"calendars", "events", "attendees", "reminders", "extendedproperties"})
        assertTrue(calendar.containsKey(table + ".jsonl"));
      Map<String, byte[]> preferences = unzip(read(docs, "Device-data/preferences.zip"));
      String settings = new String(preferences.get("preferences.jsonl"), StandardCharsets.UTF_8);
      assertEquals(9, settings.trim().split("\n").length);
      assertFalse(settings.contains("android_id"));
      read(docs, "Device-data/sms.zip");
      read(docs, "Device-data/calls.zip");
      File root = new File(c.getCacheDir(), "device-exports");
      assertEquals(1, root.listFiles().length);
      stage = root.listFiles()[0];
    }
    assertFalse("Completed exports left private staged files", stage.exists());
  }

  @Test
  public void repeatedUnchangedExportsAreByteIdentical() throws Exception {
    Context c = context();
    BackupJob j = job("sms");
    byte[] first;
    try (AndroidDocuments docs =
        new AndroidDocuments(
            c, j, new Operations.Cancellation(), text -> {}, ExportFixtureService.class)) {
      first = read(docs, "Device-data/sms.zip");
    }
    try (AndroidDocuments docs =
        new AndroidDocuments(
            c, j, new Operations.Cancellation(), text -> {}, ExportFixtureService.class)) {
      assertArrayEquals(first, read(docs, "Device-data/sms.zip"));
    }
  }

  @Test
  public void providerFailureDoesNotExposePartialExportsAndCleansCache() throws Exception {
    Context c = context();
    try (AndroidDocuments ignored =
        new AndroidDocuments(
            c,
            job("contacts"),
            new Operations.Cancellation(),
            text -> {},
            BrokenExportFixtureService.class)) {
      fail("Denied provider was accepted");
    } catch (Exception expected) {
      assertTrue(expected.getMessage(), expected.getMessage().contains("contacts"));
    }
    assertEquals(0, new File(c.getCacheDir(), "device-exports").listFiles().length);
  }

  @Test
  public void cancellationStopsBlockedExportAndRemovesTemporaryFiles() throws Exception {
    Context c = context();
    Operations.Cancellation cancellation = new Operations.Cancellation();
    CountDownLatch preparing = new CountDownLatch(1);
    AtomicReference<Throwable> failure = new AtomicReference<>();
    Thread worker =
        new Thread(
            () -> {
              try (AndroidDocuments ignored =
                  new AndroidDocuments(
                      c,
                      job("contacts"),
                      cancellation,
                      text -> preparing.countDown(),
                      SlowExportFixtureService.class)) {
                failure.set(new AssertionError("Cancelled export succeeded"));
              } catch (Throwable e) {
                failure.set(e);
              }
            });
    worker.start();
    assertTrue(preparing.await(15, TimeUnit.SECONDS));
    Thread.sleep(500);
    cancellation.cancel();
    worker.join(5000);
    assertFalse("Export cancellation remained blocked", worker.isAlive());
    assertNotNull(failure.get());
    assertTrue(failure.get().getMessage().contains("cancelled"));
    assertEquals(0, new File(c.getCacheDir(), "device-exports").listFiles().length);
  }

  @Test
  public void invalidCategoriesAreRejectedBeforeProviderAccess() throws Exception {
    Context c = context();
    try (ShizukuBridge bridge = new ShizukuBridge(c)) {
      for (String category : new String[] {"../../sms", "accounts", "", "contacts/secret"}) {
        try {
          bridge.files().openExport(category);
          fail("Invalid category accepted");
        } catch (IllegalArgumentException expected) {
          assertTrue(expected.getMessage().contains("Unknown"));
        }
      }
    }
  }

  @Test
  public void oversizedProviderFieldsFailInsteadOfTruncating() throws Exception {
    DeviceExport.Source source =
        new DeviceExport.Source() {
          public Cursor query(Uri uri, String[] projection, String sort) {
            MatrixCursor c = new MatrixCursor(new String[] {"_id", "body"});
            c.addRow(new Object[] {1L, new String(new char[2 * 1024 * 1024 + 1])});
            return c;
          }

          public InputStream open(Uri uri) {
            throw new AssertionError("Unexpected attachment");
          }

          public void close() {}
        };
    try {
      DeviceExport.write("sms", source, new ByteArrayOutputStream());
      fail("Oversized field accepted");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("exceeds"));
    }
  }
}
