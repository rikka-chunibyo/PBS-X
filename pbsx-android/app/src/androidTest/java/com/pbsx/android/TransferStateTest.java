package com.pbsx.android;

import static org.junit.Assert.*;

import android.content.Context;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class TransferStateTest {
  @Test
  public void perFileProgressIsSeparateFromUploadAndNeverInventsAnOverallPercentage()
      throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    TransferState.begin(false);
    assertFalse(TransferState.current().hasFileProgress());
    assertTrue(
        TransferState.accept(
            new JSONObject()
                .put("stage", "Backing up")
                .put("path", "Photos/large.mp4")
                .put("files", 7)
                .put("bytes", 500)
                .put("file_bytes", 50)
                .put("file_total", 200)
                .put("uploaded_bytes", 0)
                .toString()));
    TransferState.Snapshot state = TransferState.current();
    assertTrue(state.hasFileProgress());
    assertEquals(25, state.percent());
    assertEquals("Current file · 25%", state.barLabel());
    assertTrue(state.summary(c).contains("Photos/large.mp4"));
    assertTrue(state.counts(c).contains("7 files"));
    assertTrue(state.counts(c).contains("uploaded"));
    assertFalse(TransferState.accept("bad json"));
    assertSame(state, TransferState.current());
    TransferState.phase("Saving snapshot notes");
    assertFalse(TransferState.current().hasFileProgress());
    TransferState.begin(true);
    assertTrue(TransferState.current().restore);
    assertFalse(TransferState.current().counts(c).contains("uploaded"));
  }

  @Test
  public void lateProgressCannotOverwriteACompletedRun() throws Exception {
    Context base = InstrumentationRegistry.getInstrumentation().getTargetContext();
    Context c =
        new android.content.ContextWrapper(base) {
          @Override
          public android.content.SharedPreferences getSharedPreferences(String name, int mode) {
            return base.getSharedPreferences("progress-fixture-" + name, mode);
          }
        };
    c.getSharedPreferences("jobs-v1", 0).edit().clear().commit();
    BackupJob job = new BackupJob();
    String id = JobStore.start(c, job, "backup");
    JobStore.progress(c, id, "Backing up Photos/large.mp4 · 25%");
    assertTrue(JobStore.history(c).getJSONObject(0).getString("message").contains("25%"));
    JobStore.finish(c, id, true, "Backup complete");
    JobStore.progress(c, id, "late read");
    assertEquals("success", JobStore.history(c).getJSONObject(0).getString("state"));
    assertEquals("Backup complete", JobStore.history(c).getJSONObject(0).getString("message"));
  }
}
