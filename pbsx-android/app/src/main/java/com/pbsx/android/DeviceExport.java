package com.pbsx.android;

import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.util.Base64;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.zip.Deflater;
import java.util.zip.ZipEntry;
import java.util.zip.ZipOutputStream;
import org.json.JSONObject;

/** Versioned, bounded provider exports. This class has no provider write/import methods. */
final class DeviceExport {
  static final List<String> CATEGORIES =
      Collections.unmodifiableList(
          Arrays.asList("contacts", "sms", "mms", "calls", "calendar", "preferences"));
  static final String[] LABELS = {
    "Contacts",
    "SMS messages",
    "MMS messages and attachments",
    "Call history",
    "Calendars and events",
    "Selected device preferences"
  };
  static final long MAX_BYTES = 512L * 1024 * 1024;
  static final int MAX_ROWS = 250000;

  interface Source extends AutoCloseable {
    Cursor query(Uri uri, String[] projection, String sort) throws Exception;

    InputStream open(Uri uri) throws Exception;

    void close() throws Exception;
  }

  private final Source source;
  private final ZipOutputStream zip;
  private final HashSet<String> entries = new HashSet<>();
  private int rows;

  private DeviceExport(Source source, OutputStream output) {
    this.source = source;
    zip =
        new ZipOutputStream(
            new java.io.FilterOutputStream(output) {
              @Override
              public void close() throws java.io.IOException {
                flush();
              }
            },
            StandardCharsets.UTF_8);
    // Deterministic, streamable entries; PBS performs content compression/deduplication.
    zip.setLevel(Deflater.NO_COMPRESSION);
  }

  static void write(String category, Source source, OutputStream output) throws Exception {
    if (!CATEGORIES.contains(category)) throw new IllegalArgumentException("Unknown device export");
    DeviceExport e = new DeviceExport(source, output);
    try {
      e.entry("manifest.json");
      e.line(
          new JSONObject()
              .put("format", "pbsx-android-provider-export")
              .put("version", 1)
              .put("category", category)
              .put("android_sdk", Build.VERSION.SDK_INT)
              .put("restore", "Files only; Android database import is not implemented"));
      e.zip.closeEntry();
      switch (category) {
        case "contacts":
          e.contacts();
          break;
        case "sms":
          e.table("sms.jsonl", "content://sms");
          break;
        case "calls":
          e.table("calls.jsonl", "content://call_log/calls");
          break;
        case "calendar":
          for (String table :
              new String[] {"calendars", "events", "attendees", "reminders", "extendedproperties"})
            e.table(table + ".jsonl", "content://com.android.calendar/" + table);
          break;
        case "mms":
          e.mms();
          break;
        case "preferences":
          e.preferences();
          break;
        default:
          throw new IllegalArgumentException("Unknown device export");
      }
      e.zip.finish();
      e.zip.flush();
    } finally {
      e.zip.close();
    }
  }

  private void entry(String name) throws Exception {
    if (!entries.add(name)) throw new Exception("Duplicate export attachment identity");
    ZipEntry entry = new ZipEntry(name);
    entry.setTime(0);
    zip.putNextEntry(entry);
  }

  private void line(JSONObject row) throws Exception {
    String value = row.toString();
    if (value.length() > 4 * 1024 * 1024) throw new Exception("Provider record exceeds 4 MiB");
    zip.write(value.getBytes(StandardCharsets.UTF_8));
    zip.write('\n');
  }

  private Cursor query(String uri, String[] projection, String sort) throws Exception {
    Cursor cursor = source.query(Uri.parse(uri), projection, sort);
    if (cursor == null) throw new Exception("Provider returned no readable cursor");
    return cursor;
  }

  private JSONObject row(Cursor c) throws Exception {
    if (++rows > MAX_ROWS) throw new Exception("Device export exceeds 250,000 records");
    if (c.getColumnCount() > 512) throw new Exception("Provider has too many columns");
    JSONObject row = new JSONObject();
    for (int i = 0; i < c.getColumnCount(); i++) {
      String name = c.getColumnName(i);
      if (row.has(name)) throw new Exception("Provider returned duplicate column names");
      switch (c.getType(i)) {
        case Cursor.FIELD_TYPE_NULL:
          row.put(name, JSONObject.NULL);
          break;
        case Cursor.FIELD_TYPE_INTEGER:
          row.put(name, c.getLong(i));
          break;
        case Cursor.FIELD_TYPE_FLOAT:
          row.put(name, c.getDouble(i));
          break;
        case Cursor.FIELD_TYPE_BLOB:
          byte[] bytes = c.getBlob(i);
          if (bytes.length > 2 * 1024 * 1024) throw new Exception("Provider blob exceeds 2 MiB");
          row.put(
              name,
              new JSONObject()
                  .put("encoding", "base64")
                  .put("value", Base64.encodeToString(bytes, Base64.NO_WRAP)));
          break;
        default:
          String text = c.getString(i);
          if (text.length() > 2 * 1024 * 1024) throw new Exception("Provider field exceeds 2 MiB");
          row.put(name, text);
      }
    }
    return row;
  }

  private void table(String name, String uri) throws Exception {
    entry(name);
    try (Cursor cursor = query(uri, null, "_id ASC")) {
      while (cursor.moveToNext()) line(row(cursor));
    }
    zip.closeEntry();
  }

  private static long id(Cursor c, String column) throws Exception {
    long value = c.getLong(c.getColumnIndexOrThrow(column));
    if (value < 0) throw new Exception("Invalid provider record ID");
    return value;
  }

  private void attachment(String name, String uri) throws Exception {
    entry(name);
    try (InputStream input = source.open(Uri.parse(uri))) {
      if (input == null) throw new Exception("Provider returned no attachment stream");
      byte[] buffer = new byte[65536];
      int count;
      while ((count = input.read(buffer)) != -1) zip.write(buffer, 0, count);
    }
    zip.closeEntry();
  }

  private void contacts() throws Exception {
    String base = "content://com.android.contacts/";
    for (String table : new String[] {"contacts", "raw_contacts", "data", "groups"})
      table(table + ".jsonl", base + table);
    // Also provide Android's standard vCard export for ordinary manual import.
    entry("contacts.vcf");
    try (Cursor contacts = query(base + "contacts", new String[] {"_id", "lookup"}, "_id ASC")) {
      byte[] buffer = new byte[65536];
      while (contacts.moveToNext()) {
        if (++rows > MAX_ROWS) throw new Exception("Too many contact vCards");
        String lookup = contacts.getString(contacts.getColumnIndexOrThrow("lookup"));
        if (lookup == null || lookup.isEmpty() || lookup.length() > 4096)
          throw new Exception("Contact has no valid lookup key");
        Uri uri =
            Uri.withAppendedPath(
                android.provider.ContactsContract.Contacts.CONTENT_VCARD_URI, lookup);
        try (InputStream input = source.open(uri)) {
          if (input == null) throw new Exception("Contact vCard is unavailable");
          int count;
          while ((count = input.read(buffer)) != -1) zip.write(buffer, 0, count);
        }
        zip.write('\r');
        zip.write('\n');
      }
    }
    zip.closeEntry();
    try (Cursor contacts =
        query(base + "contacts", new String[] {"_id", "photo_file_id"}, "_id ASC")) {
      while (contacts.moveToNext()) {
        if (++rows > MAX_ROWS) throw new Exception("Too many contact photos");
        int photo = contacts.getColumnIndexOrThrow("photo_file_id");
        if (!contacts.isNull(photo) && contacts.getLong(photo) > 0) {
          long id = id(contacts, "_id");
          attachment("photos/" + id + ".bin", base + "contacts/" + id + "/display_photo");
        }
      }
    }
  }

  private void mms() throws Exception {
    table("messages.jsonl", "content://mms");
    entry("addresses.jsonl");
    try (Cursor messages = query("content://mms", new String[] {"_id"}, "_id ASC")) {
      while (messages.moveToNext()) {
        long message = id(messages, "_id");
        try (Cursor addresses = query("content://mms/" + message + "/addr", null, "_id ASC")) {
          while (addresses.moveToNext())
            line(new JSONObject().put("message_id", message).put("values", row(addresses)));
        }
      }
    }
    zip.closeEntry();
    entry("parts.jsonl");
    try (Cursor parts = query("content://mms/part", null, "_id ASC")) {
      while (parts.moveToNext()) {
        JSONObject row = row(parts);
        if (hasPayload(parts))
          row.put("pbsx_attachment", "attachments/" + id(parts, "_id") + ".bin");
        row.put(
            "pbsx_payload_state",
            hasPayload(parts)
                ? "provider-file"
                : row.isNull("text") ? "not-present" : "inline-text");
        row.remove("_data"); // A private filesystem path is not a portable attachment reference.
        line(row);
      }
    }
    zip.closeEntry();
    try (Cursor parts = query("content://mms/part", null, "_id ASC")) {
      while (parts.moveToNext()) {
        if (++rows > MAX_ROWS) throw new Exception("Too many MMS parts");
        if (hasPayload(parts)) {
          long id = id(parts, "_id");
          attachment("attachments/" + id + ".bin", "content://mms/part/" + id);
        }
      }
    }
  }

  private static boolean hasPayload(Cursor parts) {
    int data = parts.getColumnIndex("_data");
    return data >= 0 && !parts.isNull(data) && !parts.getString(data).isEmpty();
  }

  private void preferences() throws Exception {
    entry("preferences.jsonl");
    // Explicit allowlist: no account IDs, credentials, lock settings or vendor-wide dump.
    for (String key :
        new String[] {
          "font_scale",
          "screen_brightness",
          "screen_brightness_mode",
          "accelerometer_rotation",
          "user_rotation",
          "screen_off_timeout",
          "sound_effects_enabled",
          "haptic_feedback_enabled",
          "dtmf_tone"
        }) {
      try (Cursor values =
          query("content://settings/system/" + key, new String[] {"name", "value"}, null)) {
        boolean found = false;
        while (values.moveToNext()) {
          found = true;
          line(
              new JSONObject()
                  .put("namespace", "system")
                  .put("key", key)
                  .put("values", row(values)));
        }
        if (!found)
          line(new JSONObject().put("namespace", "system").put("key", key).put("unset", true));
      }
    }
    zip.closeEntry();
  }
}
