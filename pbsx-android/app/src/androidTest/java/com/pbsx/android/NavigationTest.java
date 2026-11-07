package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Exercises visible navigation against the local HTTPS archive fixture. */
@RunWith(AndroidJUnit4.class)
public class NavigationTest {
  private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();

  private View find(View root, String label, boolean description) {
    if (description
        ? label.contentEquals(
            root.getContentDescription() == null ? "" : root.getContentDescription())
        : root instanceof TextView && label.contentEquals(((TextView) root).getText())) return root;
    if (root instanceof ViewGroup)
      for (int i = 0; i < ((ViewGroup) root).getChildCount(); i++) {
        View found = find(((ViewGroup) root).getChildAt(i), label, description);
        if (found != null) return found;
      }
    return null;
  }

  private View waitFor(Activity activity, String text, boolean description) throws Exception {
    long end = System.currentTimeMillis() + 10000;
    AtomicReference<View> found = new AtomicReference<>();
    do {
      instrumentation.runOnMainSync(
          () -> found.set(find(activity.findViewById(android.R.id.content), text, description)));
      if (found.get() != null) return found.get();
      Thread.sleep(50);
    } while (System.currentTimeMillis() < end);
    throw new AssertionError("UI did not show " + text);
  }

  private void click(Activity activity, String label) throws Exception {
    View v = waitFor(activity, label, false);
    instrumentation.runOnMainSync(() -> assertTrue(v.performClick()));
  }

  private void back(Activity activity) throws Exception {
    // ATD has no focused rendered display for injected hardware Back events.
    // Exercise the app's Back button and the same navigateBack state machine.
    View button = waitFor(activity, "Back", true);
    instrumentation.runOnMainSync(() -> assertTrue(button.performClick()));
  }

  private void capture(Activity activity, String name) throws Exception {
    capture(activity.getWindow().getDecorView(), name);
  }

  private void capture(View decor, String name) throws Exception {
    if (!"true".equals(InstrumentationRegistry.getArguments().getString("pbsx_screenshots")))
      return;
    instrumentation.waitForIdleSync();
    Thread.sleep(250);
    // ATD disables SurfaceFlinger output. Draw the actual laid-out Android views
    // into a software canvas for review; this is not a hardware screenshot.
    AtomicReference<Bitmap> rendered = new AtomicReference<>();
    instrumentation.runOnMainSync(
        () -> {
          Bitmap bitmap =
              Bitmap.createBitmap(decor.getWidth(), decor.getHeight(), Bitmap.Config.ARGB_8888);
          decor.draw(new Canvas(bitmap));
          rendered.set(bitmap);
        });
    Bitmap bitmap = rendered.get();
    try (FileOutputStream output =
        new FileOutputStream(
            new File(instrumentation.getTargetContext().getCacheDir(), name + ".png"))) {
      assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output));
    } finally {
      bitmap.recycle();
    }
  }

  @Test
  public void jobsTabsEditorAndSnapshotSelectionNavigateCorrectly() throws Exception {
    Assume.assumeTrue(android.os.Build.VERSION.SDK_INT >= 29);
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    Context c = instrumentation.getTargetContext();
    SharedPreferences jobs = c.getSharedPreferences("jobs-v1", 0);
    Map<String, ?> old = new HashMap<>(jobs.getAll());
    String oldVault = Vault.load(c);
    Activity main = null, restore = null, editor = null;
    try {
      jobs.edit().clear().commit();
      Vault.save(c, config);
      BackupJob job = new BackupJob();
      job.name = "Camera test";
      job.sources.put(
          new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
      JobStore.save(c, job);
      main =
          instrumentation.startActivitySync(
              new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      waitFor(main, "Camera test", false);
      capture(main, "jobs");
      click(main, "Activity");
      waitFor(main, "Recent backup and restore runs on this device.", false);
      click(main, "Settings");
      waitFor(main, "Server & credentials", false);
      click(main, "Jobs");
      Instrumentation.ActivityMonitor browser =
          instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      click(main, "Snapshots");
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      click(restore, "Browse files");
      View photos = waitFor(restore, "Photos", false);
      instrumentation.runOnMainSync(() -> assertTrue(((View) photos.getParent()).performClick()));
      View choose = waitFor(restore, "Select a.txt", true);
      // CompoundButton toggles even when no ordinary OnClickListener consumes the click.
      instrumentation.runOnMainSync(choose::performClick);
      waitFor(restore, "Selection · 1", false);
      capture(restore, "restore-files");
      click(restore, "Restore selected");
      instrumentation.waitForIdleSync();
      AtomicReference<View> options = new AtomicReference<>();
      instrumentation.runOnMainSync(
          () -> {
            for (View window : android.view.inspector.WindowInspector.getGlobalWindowViews())
              if (find(window, "New folder (default)", false) != null) options.set(window);
          });
      assertNotNull("Restore options dialog was not shown", options.get());
      for (String label :
          new String[] {
            "New folder (default)",
            "Skip existing files",
            "Replace different files",
            "Rename conflicting files"
          }) {
        AtomicReference<View> option = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> option.set(find(options.get(), label, false)));
        assertNotNull("Missing restore mode " + label, option.get());
      }
      capture(options.get(), "restore-modes");
      instrumentation.runOnMainSync(
          () -> assertTrue(find(options.get(), "Cancel", false).performClick()));
      long closeDeadline = System.currentTimeMillis() + 5000;
      AtomicReference<Boolean> attached = new AtomicReference<>(true);
      do {
        instrumentation.runOnMainSync(() -> attached.set(options.get().isAttachedToWindow()));
        if (attached.get()) Thread.sleep(50);
      } while (attached.get() && System.currentTimeMillis() < closeDeadline);
      assertFalse("Restore options did not close", attached.get());
      back(restore);
      waitFor(restore, "Snapshot contents", false);
      waitFor(restore, "Selection · 1", false);
      back(restore);
      waitFor(restore, "Browse files", false);
      back(restore);
      restore = null;
      Instrumentation.ActivityMonitor edit =
          instrumentation.addMonitor(JobEditorActivity.class.getName(), null, false);
      click(main, "+ New job");
      editor = instrumentation.waitForMonitorWithTimeout(edit, 5000);
      instrumentation.removeMonitor(edit);
      assertNotNull(editor);
      waitFor(editor, "Schedule & connection", false);
      waitFor(editor, "Optional Shizuku sources", false);
    } finally {
      final Activity a = editor, b = restore, d = main;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null && !a.isFinishing()) a.finish();
            if (b != null && !b.isFinishing()) b.finish();
            if (d != null && !d.isFinishing()) d.finish();
          });
      SharedPreferences.Editor reset = jobs.edit().clear();
      for (Map.Entry<String, ?> item : old.entrySet()) {
        Object value = item.getValue();
        if (value instanceof String) reset.putString(item.getKey(), (String) value);
        else if (value instanceof Integer) reset.putInt(item.getKey(), (Integer) value);
        else if (value instanceof Long) reset.putLong(item.getKey(), (Long) value);
        else if (value instanceof Boolean) reset.putBoolean(item.getKey(), (Boolean) value);
      }
      reset.commit();
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
      ScheduleService.configureAll(c);
    }
  }

  private void search(Activity activity, String query, String absent) throws Exception {
    android.widget.EditText field =
        (android.widget.EditText) waitFor(activity, "Search backups", true);
    instrumentation.runOnMainSync(() -> field.setText(query));
    long deadline = System.currentTimeMillis() + 10000;
    java.util.concurrent.atomic.AtomicBoolean gone =
        new java.util.concurrent.atomic.AtomicBoolean();
    do {
      instrumentation.runOnMainSync(
          () -> gone.set(find(activity.findViewById(android.R.id.content), absent, false) == null));
      if (!gone.get()) Thread.sleep(50);
    } while (!gone.get() && System.currentTimeMillis() < deadline);
    assertTrue("Search did not filter " + absent, gone.get());
  }

  @Test
  public void restoreCatalogueFindsOtherHostsNamespacesAndBackupTypes() throws Exception {
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    Context c = instrumentation.getTargetContext();
    String oldVault = Vault.load(c);
    Activity main = null, restore = null;
    try {
      Vault.save(c, config);
      main =
          instrumentation.startActivitySync(
              new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      click(main, "Restore");
      waitFor(main, "host/family-laptop", false);
      waitFor(main, "host/grandmas-phone", false);
      waitFor(main, "vm/100", false);
      waitFor(main, "ct/101", false);
      capture(main, "restore-catalogue");
      search(main, "phones/kids", "host/family-laptop");
      waitFor(main, "host/grandmas-phone", false);
      search(main, "ct/101", "host/grandmas-phone");
      View label = waitFor(main, "ct/101", false);
      Instrumentation.ActivityMonitor browser =
          instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      instrumentation.runOnMainSync(
          () -> assertTrue(find((View) label.getParent(), "Snapshots", false).performClick()));
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      assertEquals("ct", restore.getIntent().getStringExtra("source_type"));
      assertEquals("101", restore.getIntent().getStringExtra("source"));
      assertEquals("family", restore.getIntent().getStringExtra("store"));
      click(restore, "Browse files");
      waitFor(restore, "Snapshot contents", false);
      back(restore);
      waitFor(restore, "Browse files", false);
      back(restore);
      restore = null;
      waitFor(main, "ct/101", false);
      search(main, "vm/100", "ct/101");
      View vm = waitFor(main, "vm/100", false);
      browser = instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      instrumentation.runOnMainSync(
          () -> assertTrue(find((View) vm.getParent(), "Snapshots", false).performClick()));
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      View browse = waitFor(restore, "Browse files", false);
      instrumentation.runOnMainSync(
          () ->
              assertFalse("Image-only snapshots must not offer file restore", browse.isEnabled()));
      capture(restore, "restore-vm-snapshots");
    } finally {
      Activity a = restore, b = main;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null) a.finish();
            if (b != null) b.finish();
          });
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
    }
  }
}
