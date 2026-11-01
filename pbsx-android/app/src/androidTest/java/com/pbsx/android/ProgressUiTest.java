package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.Instrumentation;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ProgressBar;
import android.widget.TextView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.lang.reflect.Field;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** UI-only synthetic state; actual Go/SAF progress is covered by SelectiveRestoreTest. */
@RunWith(AndroidJUnit4.class)
public class ProgressUiTest {
  private Field field(String name) throws Exception {
    Field f = Operations.class.getDeclaredField(name);
    f.setAccessible(true);
    return f;
  }

  private View find(View view, String label) {
    if (label.contentEquals(
            view.getContentDescription() == null ? "" : view.getContentDescription())
        || view instanceof TextView && ((TextView) view).getText().toString().contains(label))
      return view;
    if (view instanceof ViewGroup)
      for (int n = 0; n < ((ViewGroup) view).getChildCount(); n++) {
        View found = find(((ViewGroup) view).getChildAt(n), label);
        if (found != null) return found;
      }
    return null;
  }

  @Test
  public void darkTransferCardShowsFilePercentageAndUpdatesWithoutRebuildingThePage()
      throws Exception {
    Instrumentation i = InstrumentationRegistry.getInstrumentation();
    Context c = i.getTargetContext();
    SharedPreferences prefs = c.getSharedPreferences("jobs-v1", 0);
    Map<String, ?> saved = new HashMap<>(prefs.getAll());
    AtomicBoolean running = (AtomicBoolean) field("running").get(null);
    assertFalse("Run this UI fixture on an idle disposable emulator", running.get());
    MainActivity activity = null;
    try {
      prefs.edit().clear().commit();
      BackupJob job = new BackupJob();
      job.name = "Camera backup";
      JobStore.save(c, job);
      String run = JobStore.start(c, job, "backup");
      field("activeName").set(null, job.name);
      field("activeJobId").set(null, job.id);
      field("activeRunId").set(null, run);
      TransferState.begin(false);
      assertTrue(
          TransferState.accept(
              new JSONObject()
                  .put("stage", "Backing up")
                  .put("path", "Photos/holiday.mp4")
                  .put("files", 12)
                  .put("bytes", 1048576)
                  .put("file_bytes", 524288)
                  .put("file_total", 1048576)
                  .put("uploaded_bytes", 0)
                  .toString()));
      JobStore.progress(c, run, TransferState.current().summary(c));
      running.set(true);
      activity =
          (MainActivity)
              i.startActivitySync(
                  new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      MainActivity main = activity;
      AtomicReference<ProgressBar> bar = new AtomicReference<>();
      i.runOnMainSync(
          () -> {
            View root = main.findViewById(android.R.id.content);
            bar.set((ProgressBar) find(root, "Current file progress"));
            assertNotNull(bar.get());
            assertFalse(bar.get().isIndeterminate());
            assertEquals(50, bar.get().getProgress());
            assertNotNull(find(root, "Current file · 50%"));
            assertNotNull(find(root, "Photos/holiday.mp4"));
            android.util.TypedValue color = new android.util.TypedValue();
            main.getTheme().resolveAttribute(android.R.attr.colorBackground, color, true);
            assertEquals(Ui.BG, color.data);
          });
      if ("true".equals(InstrumentationRegistry.getArguments().getString("pbsx_screenshots"))) {
        i.waitForIdleSync();
        Thread.sleep(250);
        i.runOnMainSync(
            () -> {
              View decor = main.getWindow().getDecorView();
              Bitmap bitmap =
                  Bitmap.createBitmap(decor.getWidth(), decor.getHeight(), Bitmap.Config.ARGB_8888);
              decor.draw(new Canvas(bitmap));
              try (FileOutputStream output =
                  new FileOutputStream(new File(c.getCacheDir(), "progress-dark.png"))) {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, output);
              } catch (Exception e) {
                throw new AssertionError(e);
              } finally {
                bitmap.recycle();
              }
            });
      }
      TransferState.phase("Saving snapshot notes");
      JobStore.progress(c, run, TransferState.current().summary(c));
      long until = System.currentTimeMillis() + 5000;
      AtomicBoolean updated = new AtomicBoolean();
      do {
        i.runOnMainSync(() -> updated.set(bar.get().isIndeterminate()));
        if (!updated.get()) Thread.sleep(50);
      } while (!updated.get() && System.currentTimeMillis() < until);
      assertTrue("Finalization did not update the bar", updated.get());
      i.runOnMainSync(
          () ->
              assertSame(
                  "Progress must not reset the scroll/page every tick",
                  bar.get(),
                  find(main.findViewById(android.R.id.content), "Current file progress")));
    } finally {
      MainActivity main = activity;
      if (main != null) i.runOnMainSync(main::finish);
      running.set(false);
      field("activeName").set(null, "");
      field("activeJobId").set(null, "");
      field("activeRunId").set(null, "");
      SharedPreferences.Editor restore = prefs.edit().clear();
      for (Map.Entry<String, ?> entry : saved.entrySet()) {
        Object value = entry.getValue();
        if (value instanceof String) restore.putString(entry.getKey(), (String) value);
        else if (value instanceof Integer) restore.putInt(entry.getKey(), (Integer) value);
        else if (value instanceof Long) restore.putLong(entry.getKey(), (Long) value);
        else if (value instanceof Boolean) restore.putBoolean(entry.getKey(), (Boolean) value);
      }
      restore.commit();
    }
  }
}
