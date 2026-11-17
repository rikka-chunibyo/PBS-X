package com.pbsx.android;

/** SAF has no universal atomic replacement. Keep the old document until promotion succeeds. */
final class RestoreCommit {
  interface Files {
    String find(String name) throws Exception;
    String rename(String id, String name) throws Exception;
    void delete(String id) throws Exception;
  }

  static void commit(Files files, String staged, String target, String recovery, boolean replace)
      throws Exception {
    String old = files.find(target);
    if (old != null && !replace) throw new Exception("Destination appeared during restore: " + target);
    if (files.find(recovery) != null) throw new Exception("Restore recovery name already exists");
    String saved = old == null ? null : files.rename(old, recovery);
    try {
      if (files.find(target) != null)
        throw new Exception("Destination appeared during restore: " + target);
      files.rename(staged, target);
    } catch (Exception failure) {
      if (saved != null) {
        try {
          if (files.find(target) != null)
            throw new Exception("Destination changed; previous file retained as " + recovery);
          files.rename(saved, target);
        } catch (Exception rollback) {
          failure.addSuppressed(rollback);
          throw new Exception("Restore failed; previous file retained as " + recovery, failure);
        }
      }
      throw failure;
    }
    if (saved != null) {
      try {
        files.delete(saved);
      } catch (Exception cleanup) {
        throw new Exception("File restored; previous copy retained as " + recovery, cleanup);
      }
    }
  }
}
