package com.pbsx.android;

import static org.junit.Assert.*;

import java.util.HashMap;
import java.util.Map;
import org.junit.Test;

public class RestoreCommitTest {
  private static final class Entry {
    final String name, bytes;
    Entry(String name, String bytes) { this.name = name; this.bytes = bytes; }
  }

  private static final class Files implements RestoreCommit.Files {
    final Map<String, Entry> entries = new HashMap<>();
    String refuseRename = "", refuseDelete = "", refuseOnce = "";
    int id;
    String add(String name, String bytes) {
      String key = "document-" + (++id);
      entries.put(key, new Entry(name, bytes));
      return key;
    }
    public String find(String name) {
      for (Map.Entry<String, Entry> e : entries.entrySet())
        if (e.getValue().name.equals(name)) return e.getKey();
      return null;
    }
    public String rename(String id, String name) throws Exception {
      if (name.equals(refuseOnce)) {
        refuseOnce = "";
        throw new Exception("rename refused once");
      }
      if (name.equals(refuseRename)) throw new Exception("rename refused");
      if (find(name) != null) throw new Exception("refusing overwrite");
      Entry old = entries.remove(id);
      if (old == null) throw new Exception("document ID changed");
      return add(name, old.bytes); // Real SAF providers may change IDs on rename.
    }
    public void delete(String id) throws Exception {
      if (entries.get(id).name.equals(refuseDelete)) throw new Exception("delete refused");
      entries.remove(id);
    }
    String bytes(String name) { return entries.get(find(name)).bytes; }
  }

  @Test public void replacementKeepsOriginalUntilCompleteAndHandlesChangingDocumentIds() throws Exception {
    Files files = new Files();
    files.add("target", "old");
    String staged = files.add("stage", "complete");
    RestoreCommit.commit(files, staged, "target", "previous", true);
    assertEquals("complete", files.bytes("target"));
    assertNull(files.find("previous"));
    assertNull(files.find("stage"));
  }

  @Test public void promotionFailureRollsBackOriginal() throws Exception {
    Files files = new Files();
    files.add("target", "old");
    String staged = files.add("stage", "complete");
    files.refuseOnce = "target";
    try {
      RestoreCommit.commit(files, staged, "target", "previous", true);
      fail("provider failure ignored");
    } catch (Exception expected) { assertTrue(expected.getMessage().contains("rename refused")); }
    assertEquals("old", files.bytes("target"));
    assertEquals("complete", files.bytes("stage"));
    assertNull(files.find("previous"));
  }

  @Test public void rollbackFailureRetainsBothCopies() throws Exception {
    Files files = new Files();
    files.add("target", "old");
    String staged = files.add("stage", "complete");
    files.refuseRename = "target";
    try {
      RestoreCommit.commit(files, staged, "target", "previous", true);
      fail("provider failure ignored");
    } catch (Exception expected) {
      // This provider refuses both promotion and rollback: retain both copies.
      assertTrue(expected.getMessage().contains("previous file retained"));
    }
    assertEquals("old", files.bytes("previous"));
    assertEquals("complete", files.bytes("stage"));
  }

  @Test public void newFileCommitCannotOverwriteAnExternalConflict() throws Exception {
    Files files = new Files();
    files.add("target", "external");
    String staged = files.add("stage", "complete");
    try {
      RestoreCommit.commit(files, staged, "target", "previous", false);
      fail("external file overwritten");
    } catch (Exception expected) { assertTrue(expected.getMessage().contains("appeared")); }
    assertEquals("external", files.bytes("target"));
  }

  @Test public void cleanupFailureRetainsPreviousCopyAndCompletedTarget() throws Exception {
    Files files = new Files();
    files.add("target", "old");
    String staged = files.add("stage", "complete");
    files.refuseDelete = "previous";
    try {
      RestoreCommit.commit(files, staged, "target", "previous", true);
      fail("cleanup failure ignored");
    } catch (Exception expected) { assertTrue(expected.getMessage().contains("File restored")); }
    assertEquals("complete", files.bytes("target"));
    assertEquals("old", files.bytes("previous"));
  }
}
