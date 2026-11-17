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
    DocumentsContract.Document.COLUMN_LAST_MODIFIED,
    DocumentsContract.Document.COLUMN_FLAGS
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
        .put("modified", c.isNull(4) ? 0 : c.getLong(4))
        .put("flags", c.isNull(5) ? 0 : c.getInt(5));
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
    return statId(resolve(path));
  }

  private JSONObject statId(String id) throws Exception {
    try (Cursor c = resolver.query(document(id), COLUMNS, null, null, null)) {
      if (c == null || !c.moveToFirst()) throw new FileNotFoundException("Document is unavailable");
      return row(c);
    }
  }

  JSONObject inspect(String path) throws Exception {
    String id = root;
    for (String part : Paths.parts(path)) {
      id = find(id, part);
      if (id == null) return new JSONObject().put("exists", false);
    }
    try (Cursor c = resolver.query(document(id), COLUMNS, null, null, null)) {
      if (c == null || !c.moveToFirst()) throw new Exception("Destination provider is unavailable");
      return row(c).put("exists", true);
    }
  }

  void deleteFile(String path) throws Exception {
    index.clear();
    JSONObject item = inspect(path);
    if (!item.getBoolean("exists")) return;
    if (item.getBoolean("directory")) throw new Exception("Cannot remove a restore folder");
    if (!DocumentsContract.deleteDocument(resolver, document(item.getString("id"))))
      throw new Exception("Provider refused removing temporary restore file: " + path);
    index.clear();
  }

  void commitFile(String temporary, String path, boolean replace) throws Exception {
    String[] parts = Paths.parts(path), stagedParts = Paths.parts(temporary);
    if (parts.length == 0 || stagedParts.length != parts.length)
      throw new Exception("Invalid restore staging path");
    for (int i = 0; i < parts.length - 1; i++)
      if (!parts[i].equals(stagedParts[i])) throw new Exception("Restore staging must be a sibling");
    if (!stagedParts[stagedParts.length - 1].startsWith(".pbsx-stage-"))
      throw new Exception("Invalid restore staging name");
    JSONObject stage = stat(temporary), existing = inspect(path);
    int required = DocumentsContract.Document.FLAG_SUPPORTS_RENAME
        | DocumentsContract.Document.FLAG_SUPPORTS_DELETE;
    for (JSONObject item : new JSONObject[] {stage, existing}) {
      if (item == existing && !existing.getBoolean("exists")) continue;
      if (item.getBoolean("directory") || (item.getInt("flags") & required) != required)
        throw new Exception("Provider must support file rename and deletion for safe restores");
    }
    String parent = ".";
    for (int i = 0; i < parts.length - 1; i++)
      parent = parent.equals(".") ? parts[i] : parent + "/" + parts[i];
    final String parentId = resolve(parent);
    RestoreCommit.Files files = new RestoreCommit.Files() {
      public String find(String name) throws Exception {
        return DocumentTree.this.find(parentId, name);
      }

      public String rename(String id, String name) throws Exception {
        try {
          JSONObject before = statId(id);
          if (before.getBoolean("directory")) throw new Exception("Cannot rename a restore folder");
          Uri renamed = DocumentsContract.renameDocument(resolver, document(id), name);
          if (renamed == null) throw new Exception("Provider refused restore rename");
          JSONObject item = statId(DocumentsContract.getDocumentId(renamed));
          Map<String, String> names = index.get(parentId);
          if (names != null) {
            names.remove(before.getString("name"), id);
            names.put(item.getString("name"), item.getString("id"));
          }
          if (!name.equals(item.getString("name")))
            throw new Exception("Provider changed the restore name; data retained as " + item.getString("name"));
          return item.getString("id");
        } catch (Exception failure) {
          // Refresh only on failure, so rollback sees external conflicts. Normal
          // mutations maintain the cache instead of re-listing large folders.
          index.clear();
          throw failure;
        }
      }

      public void delete(String id) throws Exception {
        JSONObject before = statId(id);
        if (before.getBoolean("directory")) throw new Exception("Cannot delete a restore folder");
        if (!DocumentsContract.deleteDocument(resolver, document(id)))
          throw new Exception("Provider refused removing previous restore copy");
        Map<String, String> names = index.get(parentId);
        if (names != null) names.remove(before.getString("name"), id);
      }
    };
    RestoreCommit.commit(files, stage.getString("id"), parts[parts.length - 1],
        ".pbsx-previous-" + java.util.UUID.randomUUID(), replace);
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
    try {
      ParcelFileDescriptor fd = resolver.openFileDescriptor(uri, "w");
      if (fd == null) throw new Exception("Could not create output document");
      return fd.detachFd();
    } catch (Exception failure) {
      try {
        if (!DocumentsContract.deleteDocument(resolver, uri))
          throw new Exception("Provider refused removing failed output document");
        index.clear();
      } catch (Exception cleanup) { failure.addSuppressed(cleanup); }
      throw failure;
    }
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
