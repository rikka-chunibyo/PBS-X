package com.pbsx.android;

import static org.junit.Assert.*;

import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

public class DurableFilesTest {
  @Rule public TemporaryFolder temporary = new TemporaryFolder();

  private static String read(java.nio.file.Path path) throws java.io.IOException {
    return new String(Files.readAllBytes(path), StandardCharsets.UTF_8);
  }

  private static void write(java.nio.file.Path path, String value) throws java.io.IOException {
    Files.write(path, value.getBytes(StandardCharsets.UTF_8));
  }

  @Test
  public void commitsCompleteFilesAndLeavesNoStagingCopies() throws Exception {
    File root = temporary.newFolder();
    File file = new File(root, "journal");
    DurableFiles.write(file, "original".getBytes(StandardCharsets.UTF_8));
    DurableFiles.write(file, "replacement".getBytes(StandardCharsets.UTF_8));
    assertEquals("replacement", read(file.toPath()));
    assertArrayEquals(new String[] {"journal"}, root.list());
  }

  @Test
  public void failedFinalRenameIsReportedAndItsTemporaryFileIsRemoved() throws Exception {
    File root = temporary.newFolder();
    File target =
        new File(root, "journal") {
          private int paths;

          @Override
          public java.nio.file.Path toPath() {
            if (++paths == 2) {
              // Model a destination directory appearing after preflight, immediately
              // before the real filesystem rename. No test-only write hook is needed.
              try {
                Files.createDirectory(super.toPath());
                write(super.toPath().resolve("keep"), "untouched");
              } catch (java.io.IOException e) {
                throw new java.io.UncheckedIOException(e);
              }
            }
            return super.toPath();
          }
        };
    try {
      DurableFiles.write(target, "complete".getBytes(StandardCharsets.UTF_8));
      fail("Failed promotion was reported as success");
    } catch (java.io.IOException expected) {
    }
    assertEquals("untouched", read(new File(target, "keep").toPath()));
    assertArrayEquals(new String[] {"journal"}, root.list());
  }

  @Test
  public void refusesDirectoryConflictsWithoutDeletingThemOrTheirContents() throws Exception {
    File root = temporary.newFolder();
    File target = new File(root, "journal");
    assertTrue(target.mkdir());
    File sentinel = new File(target, "keep");
    write(sentinel.toPath(), "untouched");
    try {
      DurableFiles.write(target, new byte[] {1, 2, 3});
      fail("Directory conflict was reported as a successful write");
    } catch (java.io.IOException expected) {
    }
    assertEquals("untouched", read(sentinel.toPath()));
    assertArrayEquals(new String[] {"journal"}, root.list());
  }

  @Test
  public void failsWhenParentIsUnavailableAndPreservesOtherFiles() throws Exception {
    File original = temporary.newFile();
    write(original.toPath(), "keep");
    try {
      DurableFiles.write(new File(original, "journal"), new byte[] {1});
      fail("Write under a file succeeded");
    } catch (java.io.IOException expected) {
    }
    assertEquals("keep", read(original.toPath()));
  }

  @Test
  public void restoresLegacyBackupBeforeReplacingIt() throws Exception {
    File root = temporary.newFolder();
    File target = new File(root, "journal"), backup = new File(root, "journal.bak");
    write(target.toPath(), "interrupted");
    write(backup.toPath(), "last complete");
    DurableFiles.recoverLegacyBackup(target);
    assertEquals("last complete", read(target.toPath()));
    assertFalse(backup.exists());
    DurableFiles.write(target, "new complete".getBytes(StandardCharsets.UTF_8));
    assertEquals("new complete", read(target.toPath()));
  }

  @Test
  public void refusesSymbolicLinksAndInvalidLegacyBackups() throws Exception {
    File root = temporary.newFolder(), outside = temporary.newFile();
    write(outside.toPath(), "keep");
    File target = new File(root, "journal");
    Files.createSymbolicLink(target.toPath(), outside.toPath());
    try {
      DurableFiles.write(target, new byte[] {1});
      fail("Followed a recovery symlink");
    } catch (java.io.IOException expected) {
    }
    assertEquals("keep", read(outside.toPath()));
    Files.delete(target.toPath());
    assertTrue(new File(root, "journal.bak").mkdir());
    try {
      DurableFiles.write(target, new byte[] {1});
      fail("Discarded an invalid legacy backup");
    } catch (java.io.IOException expected) {
    }
    assertFalse(target.exists());
  }

  @Test
  public void directoryCreationIsRepeatableAndRefusesFiles() throws Exception {
    File root = temporary.newFolder(), directory = new File(root, "exports");
    DurableFiles.ensureDirectory(directory);
    DurableFiles.ensureDirectory(directory);
    assertTrue(directory.isDirectory());
    File file = new File(root, "keep");
    write(file.toPath(), "keep");
    try {
      DurableFiles.ensureDirectory(file);
      fail("Replaced a file with a directory");
    } catch (java.io.IOException expected) {
    }
    assertEquals("keep", read(file.toPath()));
  }
}
