package com.pbsx.android;

import static org.junit.Assert.*;

import java.util.Arrays;
import java.util.Collections;
import org.junit.Test;

public class BackupExclusionsTest {
  @Test
  public void excludesExactFilesAndDirectorySubtreesWithoutPrefixCollisions() throws Exception {
    BackupExclusions rules =
        new BackupExclusions(Arrays.asList("DCIM/.thumbnails", "Documents/private.txt"));
    for (String path :
        new String[] {"DCIM/.thumbnails", "DCIM/.thumbnails/a/b.jpg", "Documents/private.txt"})
      assertTrue(path, rules.excludes(path));
    for (String path :
        new String[] {
          ".",
          "DCIM",
          "DCIM/Camera/a.jpg",
          "DCIM/.thumbnails-backup/a.jpg",
          "Documents/private.txt.bak",
          "dcim/.thumbnails/a.jpg"
        }) assertFalse(path, rules.excludes(path));
  }

  @Test
  public void supportsZeroOrMoreDirectoriesSingleNameGlobsAndUnicodeCharacters() throws Exception {
    BackupExclusions rules =
        new BackupExclusions(Arrays.asList("**/*.tmp", "DCIM/*/cache", "Documents/?.txt"));
    for (String path :
        new String[] {
          "a.tmp",
          "DCIM/a.tmp",
          "DCIM/Camera/nested/a.tmp",
          "DCIM/Camera/cache/a.jpg",
          "Documents/猫.txt",
          "Documents/😀.txt",
          "Documents/*.tmp"
        }) assertTrue(path, rules.excludes(path));
    for (String path :
        new String[] {
          "DCIM/Camera/a.jpg",
          "DCIM/Camera/nested/cache/a.jpg",
          "Documents/ab.txt",
          "Documents/a.txt.bak"
        }) assertFalse(path, rules.excludes(path));
    assertTrue(
        new BackupExclusions(Collections.singletonList("DCIM/**/cache")).excludes("DCIM/cache/a"));
  }

  @Test
  public void rejectsUnsafeOrUnboundedImportedRules() throws Exception {
    for (String value :
        new String[] {
          "",
          ".",
          "../DCIM",
          "DCIM/..",
          "/DCIM",
          "DCIM//a",
          "DCIM/./a",
          "DCIM\\a",
          " DCIM",
          "DCIM\nfoo",
          "DCIM\u0000x",
          "foo**bar",
          "DCIM/**.jpg",
          String.join("", Collections.nCopies(513, "a"))
        }) {
      try {
        new BackupExclusions(Collections.singletonList(value));
        fail("Accepted exclusion: " + value);
      } catch (Exception expected) {
      }
    }
    try {
      new BackupExclusions(Collections.nCopies(201, "DCIM/cache"));
      fail("Accepted too many rules");
    } catch (Exception expected) {
    }
  }

  @Test
  public void handlesRepeatedRecursiveWildcardsWithoutRegexBacktracking() throws Exception {
    BackupExclusions rules =
        new BackupExclusions(Collections.singletonList("**/a/**/a/**/a/**/a/**/a/**/missing"));
    assertFalse(rules.excludes(String.join("/", Collections.nCopies(120, "a"))));
    assertFalse(new BackupExclusions(Collections.emptyList()).excludes("DCIM/Camera/a.jpg"));
  }
}
