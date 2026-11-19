package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import android.os.ParcelFileDescriptor;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.LocalRestore;
import com.pbsx.core.mobilecore.Mobilecore;
import java.io.File;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class LocalRestoreTest {
  @Test
  public void stagedRestoreCommitsSafelyOnAndroidExternalStorage() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    File base =
        new File(c.getExternalFilesDir(null), "restore-fixture-" + java.util.UUID.randomUUID());
    assertTrue(base.mkdir());
    LocalRestore output = Mobilecore.newLocalRestore(base.getCanonicalPath());
    try {
      Files.write(new File(base, "keep").toPath(), "original".getBytes(StandardCharsets.UTF_8));
      try (OutputStream stream =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) output.openWrite(".pbsx-stage-test")))) {
        stream.write("incoming".getBytes(StandardCharsets.UTF_8));
      }
      try {
        output.commitFile(".pbsx-stage-test", "keep", false);
        fail("overwrote a no-replace conflict");
      } catch (Exception expected) {
      }
      assertEquals(
          "original",
          new String(Files.readAllBytes(new File(base, "keep").toPath()), StandardCharsets.UTF_8));
      output.commitFile(".pbsx-stage-test", "keep", true);
      assertEquals(
          "incoming",
          new String(Files.readAllBytes(new File(base, "keep").toPath()), StandardCharsets.UTF_8));
      try (OutputStream stream =
          new ParcelFileDescriptor.AutoCloseOutputStream(
              ParcelFileDescriptor.adoptFd((int) output.openWrite(".pbsx-stage-new")))) {
        stream.write(1);
      }
      output.commitFile(".pbsx-stage-new", "new", false);
      assertEquals(1, new File(base, "new").length());
      LocalRestore child = output.newDirectory("fresh");
      child.close();
      try {
        output.newDirectory("fresh");
        fail("reused a supposedly fresh directory");
      } catch (Exception expected) {
      }
    } finally {
      output.close();
      ExportCache.remove(base);
    }
  }
}
