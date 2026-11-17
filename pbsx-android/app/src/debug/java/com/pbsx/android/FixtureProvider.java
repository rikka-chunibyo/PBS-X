package com.pbsx.android;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import java.io.File;
import java.io.FileNotFoundException;
import java.util.List;

/** A private, debug-only document-provider fixture backed by app cache. */
public final class FixtureProvider extends ContentProvider {
  static final String AUTHORITY = "com.pbsx.android.fixture";
  private static final java.util.concurrent.atomic.AtomicInteger CHILD_QUERIES =
      new java.util.concurrent.atomic.AtomicInteger();

  static int childQueries() { return CHILD_QUERIES.get(); }
  private static final String[] DEFAULT = {
    "document_id", "_display_name", "mime_type", "_size", "last_modified"
  };

  public boolean onCreate() {
    return true;
  }

  private File file(String id) throws Exception {
    if (!id.startsWith("root:")) throw new Exception("Invalid fixture ID");
    String relative = id.substring(5);
    Paths.parts(relative);
    File base = new File(getContext().getCacheDir(), "document-fixture");
    File f = new File(base, relative).getCanonicalFile();
    if (!f.getPath().startsWith(base.getCanonicalPath() + "/")
        && !f.equals(base.getCanonicalFile())) throw new Exception("Fixture path escaped cache");
    return f;
  }

  private static String id(File f, File base) throws Exception {
    return "root:"
        + base.getCanonicalFile().toPath().relativize(f.getCanonicalFile().toPath()).toString();
  }

  private void row(MatrixCursor cursor, String[] columns, File file, String id) {
    Object[] row = new Object[columns.length];
    for (int i = 0; i < columns.length; i++)
      switch (columns[i]) {
        case "document_id":
          row[i] = id;
          break;
        case "_display_name":
          row[i] = file.getName();
          break;
        case "mime_type":
          row[i] =
              file.isDirectory()
                  ? DocumentsContract.Document.MIME_TYPE_DIR
                  : "application/octet-stream";
          break;
        case "_size":
          row[i] = file.isDirectory() ? 0 : file.length();
          break;
        case "last_modified":
          row[i] = file.lastModified();
          break;
        case "flags":
          row[i] = DocumentsContract.Document.FLAG_SUPPORTS_RENAME
              | DocumentsContract.Document.FLAG_SUPPORTS_DELETE
              | DocumentsContract.Document.FLAG_SUPPORTS_WRITE;
          break;
      }
    cursor.addRow(row);
  }

  @Override
  public Cursor query(Uri uri, String[] projection, String selection, String[] args, String sort) {
    try {
      String document = DocumentsContract.getDocumentId(uri);
      File f = file(document);
      if (!f.exists()) throw new Exception("Missing fixture");
      String[] columns = projection == null ? DEFAULT : projection;
      MatrixCursor result = new MatrixCursor(columns);
      List<String> segments = uri.getPathSegments();
      if (segments.get(segments.size() - 1).equals("children")) {
        CHILD_QUERIES.incrementAndGet();
        File[] children = f.listFiles();
        if (children == null) throw new Exception("Fixture is not a directory");
        File base = new File(getContext().getCacheDir(), "document-fixture");
        for (File child : children) row(result, columns, child, id(child, base));
      } else row(result, columns, f, document);
      return result;
    } catch (Exception e) {
      throw new IllegalArgumentException(e);
    }
  }

  @Override
  public Bundle call(String method, String arg, Bundle extras) {
    try {
      Uri parent = extras.getParcelable("uri");
      File base = new File(getContext().getCacheDir(), "document-fixture");
      if ("android:deleteDocument".equals(method)) {
        if (!file(DocumentsContract.getDocumentId(parent)).delete())
          throw new Exception("Fixture deletion failed");
        return new Bundle();
      }
      String name = extras.getString("_display_name");
      if (Paths.parts(name).length != 1) throw new Exception("Invalid name");
      if ("android:renameDocument".equals(method)) {
        File original = file(DocumentsContract.getDocumentId(parent));
        File renamed = new File(original.getParentFile(), name);
        if (renamed.exists() || !original.renameTo(renamed)) throw new Exception("Fixture rename failed");
        Bundle result = new Bundle();
        result.putParcelable("uri", DocumentsContract.buildDocumentUriUsingTree(parent, id(renamed, base)));
        return result;
      }
      if (!"android:createDocument".equals(method))
        throw new IllegalArgumentException("Unsupported fixture operation");
      File f = new File(file(DocumentsContract.getDocumentId(parent)), name);
      if (f.exists()) throw new Exception("Already exists");
      boolean ok =
          DocumentsContract.Document.MIME_TYPE_DIR.equals(extras.getString("mime_type"))
              ? f.mkdir()
              : f.createNewFile();
      if (!ok) throw new Exception("Fixture creation failed");
      Bundle result = new Bundle();
      result.putParcelable("uri", DocumentsContract.buildDocumentUriUsingTree(parent, id(f, base)));
      return result;
    } catch (Exception e) {
      throw new IllegalArgumentException(e);
    }
  }

  @Override
  public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
    try {
      return ParcelFileDescriptor.open(
          file(DocumentsContract.getDocumentId(uri)), ParcelFileDescriptor.parseMode(mode));
    } catch (Exception e) {
      throw new FileNotFoundException(e.toString());
    }
  }

  @Override
  public String getType(Uri uri) {
    return DocumentsContract.Document.MIME_TYPE_DIR;
  }

  @Override
  public Uri insert(Uri uri, ContentValues values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public int update(Uri uri, ContentValues values, String where, String[] args) {
    throw new UnsupportedOperationException();
  }

  @Override
  public int delete(Uri uri, String where, String[] args) {
    throw new UnsupportedOperationException();
  }
}
