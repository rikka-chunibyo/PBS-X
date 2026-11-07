package com.pbsx.android;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.res.AssetFileDescriptor;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.Binder;
import android.os.ParcelFileDescriptor;
import java.io.FileNotFoundException;

/** Synthetic records only. Debug APK only, protected by the shell's DUMP permission. */
public final class ExportFixtureProvider extends ContentProvider {
  public boolean onCreate() {
    return true;
  }

  public String getType(Uri uri) {
    return "application/octet-stream";
  }

  public Cursor query(Uri uri, String[] projection, String selection, String[] args, String sort) {
    String path = uri.getPath();
    if (path.startsWith("/settings/system/")) {
      MatrixCursor c = new MatrixCursor(new String[] {"name", "value"});
      c.addRow(new Object[] {uri.getLastPathSegment(), "1"});
      return c;
    }
    if (path.equals("/mms/part")) {
      MatrixCursor c = new MatrixCursor(new String[] {"_id", "mid", "ct", "_data", "text", "fn"});
      c.addRow(
          new Object[] {
            7L, 1L, "image/png", "/private/never-open-this-path", null, "../../unsafe.png"
          });
      c.addRow(new Object[] {8L, 1L, "text/plain", null, "MMS text\n猫", null});
      return c;
    }
    if (path.matches("/mms/[0-9]+/addr")) {
      MatrixCursor c = new MatrixCursor(new String[] {"_id", "address", "type"});
      c.addRow(new Object[] {2L, "+15555550123", 137});
      return c;
    }
    MatrixCursor c =
        new MatrixCursor(
            new String[] {
              "_id",
              "text",
              "integer",
              "decimal",
              "nothing",
              "blob",
              "reader_uid",
              "photo_file_id",
              "lookup"
            });
    c.addRow(
        new Object[] {
          1L,
          "Unicode 猫\ncommas, and = signs",
          1790000000000L,
          1.25,
          null,
          new byte[] {0, 1, (byte) 255},
          Binder.getCallingUid(),
          path.equals("/com.android.contacts/contacts") ? 3L : null,
          "fixture-lookup"
        });
    return c;
  }

  public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
    if (!mode.equals("r")
        || !(uri.getPath().equals("/mms/part/7")
            || uri.getPath().equals("/com.android.contacts/contacts/1/display_photo")
            || uri.getPath().equals("/com.android.contacts/contacts/as_vcard/fixture-lookup")))
      throw new FileNotFoundException("Unknown synthetic attachment");
    try {
      ParcelFileDescriptor[] pipe = ParcelFileDescriptor.createReliablePipe();
      new Thread(
              () -> {
                try (ParcelFileDescriptor.AutoCloseOutputStream out =
                    new ParcelFileDescriptor.AutoCloseOutputStream(pipe[1])) {
                  if (uri.getPath().contains("as_vcard"))
                    out.write(
                        "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Synthetic contact\r\nEND:VCARD\r\n"
                            .getBytes(java.nio.charset.StandardCharsets.UTF_8));
                  else out.write(new byte[] {0, 1, 127, (byte) 255, 2});
                } catch (Exception ignored) {
                }
              },
              "fixture-attachment")
          .start();
      return pipe[0];
    } catch (Exception e) {
      throw new FileNotFoundException(e.getMessage());
    }
  }

  public AssetFileDescriptor openAssetFile(Uri uri, String mode) throws FileNotFoundException {
    return new AssetFileDescriptor(openFile(uri, mode), 0, AssetFileDescriptor.UNKNOWN_LENGTH);
  }

  public Uri insert(Uri uri, ContentValues values) {
    throw new SecurityException("Read-only fixture");
  }

  public int delete(Uri uri, String selection, String[] args) {
    throw new SecurityException("Read-only fixture");
  }

  public int update(Uri uri, ContentValues values, String selection, String[] args) {
    throw new SecurityException("Read-only fixture");
  }
}
