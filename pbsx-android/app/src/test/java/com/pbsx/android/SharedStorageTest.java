package com.pbsx.android;

import static org.junit.Assert.*;

import java.io.File;
import java.nio.file.Files;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

public class SharedStorageTest {
  @Rule public TemporaryFolder temp = new TemporaryFolder();

  @Test
  public void readsDownloadWithNestedFilesAndListsOnlySelectableDirectories() throws Exception {
    File root = temp.newFolder("shared");
    File download = new File(root, "Download");
    assertTrue(new File(download, "Documents").mkdirs());
    assertTrue(new File(root, "Android").mkdir());
    assertTrue(new File(root, "plain-file").createNewFile());
    Files.write(new File(download, "Documents/report.txt").toPath(), new byte[] {1, 2, 3});
    assertEquals("Download", SharedStorage.directories(root, ".")[0].getName());
    assertEquals(1, SharedStorage.directories(root, ".").length);
    assertEquals(3, SharedStorage.resolve(root, "Download/Documents/report.txt", false).length());
    assertEquals("Documents", SharedStorage.directories(root, "Download")[0].getName());
    assertEquals(root.getCanonicalFile(), SharedStorage.resolve(root, ".", true));
    SharedStorage.validate("Download/日本語 and spaces", false);
  }

  @Test
  public void rejectsAbsoluteTraversalRootAndAppFolderSelections() {
    for (String path :
        new String[] {
          "",
          ".",
          "..",
          "/Download",
          "Download/",
          "Download//a",
          "Download/../DCIM",
          "Download/./a",
          "Download\\other",
          "Download/a\nfile",
          "Android",
          "Android/data",
          "Android/obb/com.example",
          "/data/user/0"
        }) {
      try {
        SharedStorage.validate(path, false);
        fail("Accepted unsafe selection: " + path);
      } catch (IllegalArgumentException expected) {
      }
    }
  }

  @Test
  public void rejectsSymlinksIncludingIntermediateLinksAndMissingSources() throws Exception {
    File root = temp.newFolder("shared"), other = temp.newFolder("outside");
    Files.write(new File(other, "secret").toPath(), new byte[] {9});
    Files.createSymbolicLink(new File(root, "link").toPath(), other.toPath());
    File real = new File(root, "Download");
    assertTrue(real.mkdir());
    Files.createSymbolicLink(new File(root, "inside-link").toPath(), real.toPath());
    for (String path : new String[] {"link", "link/secret", "inside-link", "gone"}) {
      try {
        SharedStorage.resolve(root, path, false);
        fail("Accepted symlink or missing source: " + path);
      } catch (SecurityException | IllegalArgumentException expected) {
      }
    }
    assertEquals(1, SharedStorage.directories(root, ".").length);
    try {
      SharedStorage.directories(root, "gone");
      fail("Missing folder appeared as an empty backup");
    } catch (IllegalArgumentException expected) {
    }
  }
}
