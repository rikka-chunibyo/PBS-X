package com.pbsx.android;

import android.content.ContentResolver;
import android.database.Cursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import java.io.FileNotFoundException;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

/** SAF operations never translate document URIs into guessed filesystem paths. */
final class DocumentTree {
  private final ContentResolver resolver;
  private final Uri tree;
  private final String root;
  private final Map<String, Map<String, String>> index =
      new LinkedHashMap<String, Map<String, String>>(4, 0.75f, true) {
        @Override
        protected boolean removeEldestEntry(Map.Entry<String, Map<String, String>> e) {
          return size() > 3;
        }
      };
  private static final String[] COLUMNS = {
    DocumentsContract.Document.COLUMN_DOCUMENT_ID,
    DocumentsContract.Document.COLUMN_DISPLAY_NAME,
    DocumentsContract.Document.COLUMN_MIME_TYPE,
    DocumentsContract.Document.COLUMN_SIZE,
    DocumentsContract.Document.COLUMN_LAST_MODIFIED
  };

  DocumentTree(ContentResolver resolver, Uri tree) {
    this(resolver, tree, DocumentsContract.getTreeDocumentId(tree));
  }

  private DocumentTree(ContentResolver resolver, Uri tree, String root) {
    this.resolver = resolver;
    this.tree = tree;
    this.root = root;
  }

  private Uri document(String id) {
    return DocumentsContract.buildDocumentUriUsingTree(tree, id);
  }

  private JSONObject row(Cursor c) throws Exception {
    String name = c.getString(1), mime = c.getString(2);
    boolean dir = DocumentsContract.Document.MIME_TYPE_DIR.equals(mime);
    if (name == null) throw new Exception("Provider returned a nameless document");
    return new JSONObject()
        .put("id", c.getString(0))
        .put("name", name)
        .put("directory", dir)
        .put("size", dir ? 0 : c.isNull(3) ? -1 : c.getLong(3))
        .put("modified", c.isNull(4) ? 0 : c.getLong(4));
  }

  private JSONArray children(String id) throws Exception {
    JSONArray result = new JSONArray();
    Map<String, String> names = new HashMap<>();
    try (Cursor c =
        resolver.query(
            DocumentsContract.buildChildDocumentsUriUsingTree(tree, id),
            COLUMNS,
            null,
            null,
            null)) {
      if (c == null) throw new Exception("Document provider is unavailable");
      while (c.moveToNext()) {
        JSONObject item = row(c);
        String name = item.getString("name");
        if (names.put(name, item.getString("id")) != null)
          throw new Exception("Ambiguous duplicate document name: " + name);
        result.put(item);
        if (result.length() > 40000) throw new Exception("Directory exceeds 40,000 entries");
      }
    }
    index.put(id, names);
    return result;
  }

  private String find(String parent, String name) throws Exception {
    if (index.containsKey(parent)) return index.get(parent).get(name);
    String found = null;
    JSONArray rows = children(parent);
    for (int i = 0; i < rows.length(); i++) {
      JSONObject item = rows.getJSONObject(i);
      if (name.equals(item.getString("name"))) {
        if (found != null) throw new Exception("Ambiguous duplicate document name: " + name);
        found = item.getString("id");
      }
    }
    return found;
  }

  private String resolve(String path) throws Exception {
    String id = root;
    for (String part : Paths.parts(path)) {
      id = find(id, part);
      if (id == null) throw new FileNotFoundException("Source document disappeared");
    }
    return id;
  }

  JSONObject stat(String path) throws Exception {
    try (Cursor c = resolver.query(document(resolve(path)), COLUMNS, null, null, null)) {
      if (c == null || !c.moveToFirst()) throw new FileNotFoundException("Document is unavailable");
      return row(c);
    }
  }

  JSONArray list(String path) throws Exception {
    return children(resolve(path));
  }

  long read(String path) throws Exception {
    ParcelFileDescriptor fd = resolver.openFileDescriptor(document(resolve(path)), "r");
    if (fd == null) throw new FileNotFoundException("Could not open document");
    return fd.detachFd();
  }

  void mkdir(String path) throws Exception {
    create(path, DocumentsContract.Document.MIME_TYPE_DIR);
  }

  long write(String path) throws Exception {
    Uri uri = create(path, "application/octet-stream");
    ParcelFileDescriptor fd = resolver.openFileDescriptor(uri, "w");
    if (fd == null) throw new Exception("Could not create output document");
    return fd.detachFd();
  }

  private Uri create(String path, String mime) throws Exception {
    String[] parts = Paths.parts(path);
    if (parts.length == 0) throw new Exception("Cannot replace restore root");
    String parent = ".";
    for (int i = 0; i < parts.length - 1; i++)
      parent = parent.equals(".") ? parts[i] : parent + "/" + parts[i];
    String id = resolve(parent), name = parts[parts.length - 1];
    if (find(id, name) != null)
      throw new Exception("Restore refused to overwrite existing document: " + name);
    Uri uri = DocumentsContract.createDocument(resolver, document(id), mime, name);
    if (uri == null) throw new Exception("Provider refused document creation");
    // Some providers rename reserved names; fail instead of silently changing the archive layout.
    try (Cursor c =
        resolver.query(
            uri, new String[] {DocumentsContract.Document.COLUMN_DISPLAY_NAME}, null, null, null)) {
      if (c == null || !c.moveToFirst() || !name.equals(c.getString(0)))
        throw new Exception("Provider changed the requested document name: " + name);
    }
    Map<String, String> names = index.get(id);
    if (names != null) names.put(name, DocumentsContract.getDocumentId(uri));
    return uri;
  }

  DocumentTree freshRestoreDirectory(String name) throws Exception {
    Uri uri = create(name, DocumentsContract.Document.MIME_TYPE_DIR);
    String id = DocumentsContract.getDocumentId(uri);
    return new DocumentTree(resolver, tree, id);
  }
}
