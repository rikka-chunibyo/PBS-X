package com.pbsx.android;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;

/** Checked private-file commits. Callers serialize access and provide an existing parent. */
final class DurableFiles {
  static void syncDirectory(File directory) throws IOException {
    try (FileChannel channel = FileChannel.open(directory.toPath(), StandardOpenOption.READ)) {
      channel.force(true);
    }
  }

  static void ensureDirectory(File directory) throws IOException {
    try {
      Files.createDirectory(directory.toPath());
    } catch (FileAlreadyExistsException e) {
      if (!Files.isDirectory(directory.toPath(), LinkOption.NOFOLLOW_LINKS)) throw e;
    }
    // Repeat both barriers on retry, including a previous mkdir followed by a
    // failed sync. Each caller creates its private hierarchy one level at a time.
    syncDirectory(directory);
    syncDirectory(directory.getParentFile());
  }

  private static void checkRegular(Path path) throws IOException {
    if (Files.exists(path, LinkOption.NOFOLLOW_LINKS)
        && !Files.isRegularFile(path, LinkOption.NOFOLLOW_LINKS))
      throw new IOException(
          "Private recovery storage is not a regular file: " + path.getFileName());
  }

  static void recoverLegacyBackup(File target) throws IOException {
    Path backup = new File(target.getPath() + ".bak").toPath();
    if (!Files.exists(backup, LinkOption.NOFOLLOW_LINKS)) return;
    checkRegular(backup);
    checkRegular(target.toPath());
    Files.move(
        backup,
        target.toPath(),
        StandardCopyOption.ATOMIC_MOVE,
        StandardCopyOption.REPLACE_EXISTING);
    syncDirectory(target.getParentFile());
  }

  static void write(File target, byte[] data) throws IOException {
    recoverLegacyBackup(target);
    checkRegular(target.toPath());
    Path temporary = Files.createTempFile(target.getParentFile().toPath(), ".pbsx-write-", ".tmp");
    Throwable failure = null;
    try {
      try (FileOutputStream output = new FileOutputStream(temporary.toFile())) {
        output.write(data);
        output.getFD().sync();
      }
      Files.move(
          temporary,
          target.toPath(),
          StandardCopyOption.ATOMIC_MOVE,
          StandardCopyOption.REPLACE_EXISTING);
      syncDirectory(target.getParentFile());
    } catch (IOException | RuntimeException | Error e) {
      failure = e;
      throw e;
    } finally {
      try {
        Files.deleteIfExists(temporary);
      } catch (IOException | RuntimeException cleanup) {
        if (failure != null) failure.addSuppressed(cleanup);
        else throw cleanup;
      }
    }
  }

  private DurableFiles() {}
}
